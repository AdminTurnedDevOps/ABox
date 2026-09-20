//go:build linux

package runtime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/session"
)

func TestStopSignalsTERMReapsAndRemovesSocket(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	terminated := filepath.Join(dir, "terminated")
	script := filepath.Join(dir, "helper.sh")
	body := "#!/bin/sh\ntrap 'touch \"" + terminated + "\"; exit 0' TERM\ntouch \"" + ready + "\"\nwhile :; do sleep 1; done\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(script)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waiter := newProcessWaiter(cmd)
	waitForFile(t, ready)

	sock := filepath.Join(dir, "rpc.sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	sb := &Sandbox{Sess: &session.Session{Dir: dir}, cmd: cmd, process: waiter}
	if err := sb.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := sb.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(terminated); err != nil {
		t.Fatalf("helper did not observe SIGTERM: %v", err)
	}
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Fatalf("socket remains after Stop: %v", err)
	}
	if err := syscall.Kill(cmd.Process.Pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("helper still exists after Wait: %v", err)
	}
}

func TestStopHelperKillsAndReapsAfterTimeout(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := exec.Command("/bin/sh", "-c", "trap '' TERM; : > \""+ready+"\"; exec sleep 30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waiter := newProcessWaiter(cmd)
	waitForFile(t, ready)
	sb := &Sandbox{cmd: cmd, process: waiter}
	sb.stopHelper(25 * time.Millisecond)
	if err := syscall.Kill(cmd.Process.Pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("helper still exists after forced kill and Wait: %v", err)
	}
}

func TestStartReportsEarlyHelperExitAndReaps(t *testing.T) {
	sess := shortTestSession(t)
	script := filepath.Join(t.TempDir(), "vmm")
	body := "#!/bin/sh\nprintf '%s\\n' $$ > helper.pid\nprintf 'bounded helper diagnostic\\n' >&2\nexit 7\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Start(ctx, sess, script, 1, 128)
	if err == nil || !strings.Contains(err.Error(), "bounded helper diagnostic") {
		t.Fatalf("Start error = %v", err)
	}
	pidData, readErr := os.ReadFile(filepath.Join(sess.Dir, "helper.pid"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if convErr != nil {
		t.Fatal(convErr)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("helper still exists after failed Start: %v", err)
	}
	if _, err := os.Stat(sess.RPCSocket()); !os.IsNotExist(err) {
		t.Fatalf("socket remains after failed Start: %v", err)
	}
}

func TestStartPassesOnlyLivenessExtraFile(t *testing.T) {
	sess := shortTestSession(t)
	var err error
	sentinelPath := filepath.Join(t.TempDir(), "sentinel")
	if err := os.WriteFile(sentinelPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	sentinel, err := os.Open(sentinelPath)
	if err != nil {
		t.Fatal(err)
	}
	defer sentinel.Close()
	sentinelFD := int(sentinel.Fd())
	if sentinelFD == 3 {
		t.Fatal("test sentinel unexpectedly occupies fd 3")
	}

	script := filepath.Join(t.TempDir(), "vmm")
	body := "#!/bin/sh\nprintf '%s\\n' $$ > helper.pid\nif [ -e /proc/self/fd/3 ]; then : > fd3-present; fi\nif [ \"$(readlink /proc/self/fd/" + strconv.Itoa(sentinelFD) + ")\" = \"" + sentinelPath + "\" ]; then : > sentinel-present; fi\nIFS= read -r _ <&3\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := Start(ctx, sess, script, 1, 128); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Start error = %v, want deadline exceeded", err)
	}
	if _, err := os.Stat(filepath.Join(sess.Dir, "fd3-present")); err != nil {
		t.Fatalf("liveness fd 3 was not inherited: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sess.Dir, "sentinel-present")); !os.IsNotExist(err) {
		t.Fatalf("unrelated parent fd %d was inherited: %v", sentinelFD, err)
	}
	pidData, err := os.ReadFile(filepath.Join(sess.Dir, "helper.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("helper still exists after canceled boot: %v", err)
	}
	if _, err := os.Stat(sess.RPCSocket()); !os.IsNotExist(err) {
		t.Fatalf("socket remains after canceled boot: %v", err)
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func shortTestSession(t *testing.T) *session.Session {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "abox-runtime-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return &session.Session{ID: "test-session", Capability: "test-capability", Dir: dir}
}
