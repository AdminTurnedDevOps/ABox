//go:build linux

package runtime

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func TestCleanupStaleHelperRequiresExactIdentity(t *testing.T) {
	sess := shortTestSession(t)
	cmd := exec.Command("sleep", "30")
	cmd.Dir = sess.Dir
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-waited
	})
	if err := recordHelper(sess, cmd.Process.Pid, cmd.Path); err != nil {
		t.Fatal(err)
	}
	sess.HelperStartID = "not-the-process-start-id"
	if err := sess.WriteMeta(); err != nil {
		t.Fatal(err)
	}
	if err := cleanupStaleHelper(sess, cmd.Path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(cmd.Process.Pid, 0); err != nil {
		t.Fatalf("identity mismatch signaled unrelated process: %v", err)
	}
}

func TestCleanupStaleHelperStopsValidatedProcess(t *testing.T) {
	sess := shortTestSession(t)
	cmd := exec.Command("sleep", "30")
	cmd.Dir = sess.Dir
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	if err := recordHelper(sess, cmd.Process.Pid, cmd.Path); err != nil {
		t.Fatal(err)
	}
	if err := cleanupStaleHelper(sess, cmd.Path); err != nil {
		t.Fatal(err)
	}
	if err := <-waited; err == nil {
		t.Fatal("stale helper exited successfully after termination signal")
	}
	if err := syscall.Kill(cmd.Process.Pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("validated stale helper remains: %v", err)
	}
	if sess.HelperPID != 0 || sess.HelperStartID != "" || sess.HelperExecutable != "" {
		t.Fatalf("helper metadata was not cleared: %#v", sess)
	}
}

func TestCleanupStaleHelperClearsMissingProcess(t *testing.T) {
	sess := shortTestSession(t)
	sess.HelperPID = 1 << 30
	sess.HelperStartID = "missing"
	sess.HelperExecutable = "/missing"
	if err := cleanupStaleHelper(sess, "/missing"); err != nil {
		t.Fatal(err)
	}
	if sess.HelperPID != 0 {
		t.Fatalf("helper pid = %d", sess.HelperPID)
	}
	if _, err := os.Stat(sess.Dir + "/session.json"); err != nil {
		t.Fatal(err)
	}
}
