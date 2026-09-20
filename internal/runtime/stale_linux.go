//go:build linux

package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/AdminTurnedDevOps/ABox/internal/session"
	"golang.org/x/sys/unix"
)

type helperIdentity struct {
	executable string
	startID    string
	cwd        string
	uid        int
}

func recordHelper(sess *session.Session, pid int, executable string) error {
	_ = executable
	identity, err := readHelperIdentity(pid)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read helper identity: %w", err)
	}
	if identity.uid != os.Getuid() || filepath.Clean(identity.cwd) != filepath.Clean(sess.Dir) {
		return fmt.Errorf("started helper identity does not match the session")
	}
	sess.HelperPID = pid
	sess.HelperStartID = identity.startID
	sess.HelperExecutable = identity.executable
	return sess.WriteMeta()
}

func clearHelper(sess *session.Session) error {
	if sess == nil || (sess.HelperPID == 0 && sess.HelperStartID == "" && sess.HelperExecutable == "") {
		return nil
	}
	sess.HelperPID = 0
	sess.HelperStartID = ""
	sess.HelperExecutable = ""
	return sess.WriteMeta()
}

func cleanupStaleHelper(sess *session.Session, executable string) error {
	_ = executable
	if sess.HelperPID <= 0 {
		return clearHelper(sess)
	}
	identity, err := readHelperIdentity(sess.HelperPID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return clearHelper(sess)
		}
		return fmt.Errorf("inspect stale VMM helper: %w", err)
	}
	valid := identity.uid == os.Getuid() &&
		identity.executable == sess.HelperExecutable &&
		identity.startID == sess.HelperStartID && filepath.Clean(identity.cwd) == filepath.Clean(sess.Dir)
	if !valid {
		return clearHelper(sess)
	}
	pidfd, err := unix.PidfdOpen(sess.HelperPID, 0)
	if err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return clearHelper(sess)
		}
		return fmt.Errorf("open stale VMM pidfd: %w", err)
	}
	defer unix.Close(pidfd)
	identity, err = readHelperIdentity(sess.HelperPID)
	if err != nil || identity.uid != os.Getuid() || identity.executable != sess.HelperExecutable ||
		identity.startID != sess.HelperStartID || filepath.Clean(identity.cwd) != filepath.Clean(sess.Dir) {
		return clearHelper(sess)
	}
	if err := unix.PidfdSendSignal(pidfd, unix.SIGTERM, nil, 0); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("stop stale VMM helper: %w", err)
	}
	poll := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
	if n, err := unix.Poll(poll, 2000); err != nil {
		return fmt.Errorf("wait for stale VMM helper: %w", err)
	} else if n > 0 {
		return clearHelper(sess)
	}
	if err := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill stale VMM helper: %w", err)
	}
	if n, err := unix.Poll(poll, 2000); err != nil {
		return fmt.Errorf("wait for killed stale VMM helper: %w", err)
	} else if n == 0 {
		return fmt.Errorf("stale VMM helper did not exit after SIGKILL")
	}
	return clearHelper(sess)
}

func readHelperIdentity(pid int) (helperIdentity, error) {
	base := filepath.Join("/proc", strconv.Itoa(pid))
	executable, err := os.Readlink(filepath.Join(base, "exe"))
	if err != nil {
		return helperIdentity{}, err
	}
	cwd, err := os.Readlink(filepath.Join(base, "cwd"))
	if err != nil {
		return helperIdentity{}, err
	}
	status, err := os.ReadFile(filepath.Join(base, "status"))
	if err != nil {
		return helperIdentity{}, err
	}
	uid := -1
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "Uid:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				uid, err = strconv.Atoi(fields[1])
			}
			break
		}
	}
	if err != nil || uid < 0 {
		return helperIdentity{}, fmt.Errorf("parse process uid")
	}
	stat, err := os.ReadFile(filepath.Join(base, "stat"))
	if err != nil {
		return helperIdentity{}, err
	}
	closeParen := strings.LastIndexByte(string(stat), ')')
	if closeParen < 0 {
		return helperIdentity{}, fmt.Errorf("parse process stat")
	}
	fields := strings.Fields(string(stat)[closeParen+1:])
	if len(fields) <= 19 {
		return helperIdentity{}, fmt.Errorf("parse process start identity")
	}
	executable = strings.TrimSuffix(executable, " (deleted)")
	return helperIdentity{executable: executable, startID: fields[19], cwd: cwd, uid: uid}, nil
}
