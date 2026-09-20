//go:build linux && abox_guest

package tools

import (
	"os"
	"os/exec"
	"syscall"
)

const (
	guestUID = 1000
	guestGID = 1000
)

func configureGuestCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}
	if os.Geteuid() == 0 {
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: guestUID, Gid: guestGID}
	}
}

func cleanupGuestCommand(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

func setGuestOwnership(path string, symlink bool) error {
	if symlink {
		return os.Lchown(path, guestUID, guestGID)
	}
	return os.Chown(path, guestUID, guestGID)
}
