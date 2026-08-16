package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRejectTraversal(t *testing.T) {
	dir := t.TempDir()
	r := Repo{Root: dir}
	if _, err := r.Resolve("../etc/passwd"); err == nil {
		t.Fatal("expected rejection")
	}
	if _, err := r.Resolve("/etc/passwd"); err == nil {
		t.Fatal("expected rejection")
	}
}

func TestListAndRead(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644)
	r := Repo{Root: dir}
	paths, err := r.List(".", 4, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "a.txt" {
		t.Fatalf("paths=%v", paths)
	}
	content, bin, _, err := r.Read("a.txt", 100)
	if err != nil || bin || content != "hello" {
		t.Fatalf("read %q bin=%v err=%v", content, bin, err)
	}
}
