package repository

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateClean(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	run("git", "init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644)
	run("git", "add", "a.txt")
	run("git", "commit", "-m", "init")

	snap, err := ValidateClean(dir)
	if err != nil {
		t.Fatal(err)
	}
	if snap.HEAD == "" {
		t.Fatal("empty HEAD")
	}

	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("dirty"), 0o644)
	if _, err := ValidateClean(dir); err == nil {
		t.Fatal("expected dirty tree error")
	}
}

func TestValidateCleanEmptyRepo(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	_, err := ValidateClean(dir)
	if err == nil || !strings.Contains(err.Error(), "no commits") {
		t.Fatalf("got %v", err)
	}
}
