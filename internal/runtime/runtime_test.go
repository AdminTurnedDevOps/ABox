package runtime

import (
	"os"
	"path/filepath"
	"testing"
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
