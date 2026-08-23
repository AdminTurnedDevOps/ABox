package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/session"
)

func TestCloneFileCopiesContents(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("hello-abox"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cloneFile(src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello-abox" {
		t.Fatalf("got %q", got)
	}
}

func TestPrepareResumeDoesNotClobberRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := session.Create("/repo", "head")
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("session-disk-bytes")
	if err := os.WriteFile(s.RootDisk(), original, 0o600); err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join(t.TempDir(), "golden.raw")
	if err := os.WriteFile(golden, []byte("GOLDEN"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = Prepare(s, golden, config.Model{Name: "grok", Provider: "xai", Model: "grok-4"}, nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(s.RootDisk())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("root.raw clobbered: %q", got)
	}
	if _, err := os.Stat(s.ConfigDisk()); err != nil {
		t.Fatal(err)
	}
}
