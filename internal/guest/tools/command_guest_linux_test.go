//go:build linux && abox_guest

package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestGuestCommandDropsPrivilegesAndCreatesProcessGroup(t *testing.T) {
	cmd := guestCommand("/bin/true")
	if cmd.SysProcAttr == nil {
		t.Fatal("guest command has no process attributes")
	}
	if os.Geteuid() == 0 {
		if got := cmd.SysProcAttr.Credential; got == nil || got.Uid != guestUID || got.Gid != guestGID {
			t.Fatalf("credential = %#v", got)
		}
	} else if cmd.SysProcAttr.Credential != nil {
		t.Fatalf("unprivileged test process cannot apply credential: %#v", cmd.SysProcAttr.Credential)
	}
	if !cmd.SysProcAttr.Setpgid || cmd.SysProcAttr.Pdeathsig != syscall.SIGKILL {
		t.Fatalf("sysprocattr = %#v", cmd.SysProcAttr)
	}
}

func TestGuestCommandBackgroundChildCannotHangWait(t *testing.T) {
	repo := Repo{Root: t.TempDir()}
	start := time.Now()
	_, _, _, _, _, err := repo.RunContext(context.Background(), "sleep 30 &", filepath.Clean("."), 5*time.Second, 1024)
	if err == nil {
		t.Fatal("background child unexpectedly outlived command without an error")
	}
	if elapsed := time.Since(start); elapsed < 500*time.Millisecond {
		t.Fatalf("command failed before exercising bounded wait: %v", err)
	} else if elapsed > 4*time.Second {
		t.Fatalf("background child delayed command cleanup for %s", elapsed)
	}
}

func TestConfigureGuestCommandOverridesNoExecutable(t *testing.T) {
	cmd := exec.Command("/bin/true")
	configureGuestCommand(cmd)
	if cmd.Path != "/bin/true" {
		t.Fatalf("path = %q", cmd.Path)
	}
}
