package repository

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type archiveEntry struct {
	body string
	mode int64
	dir  bool
}

func readArchive(t *testing.T, data []byte) map[string]archiveEntry {
	t.Helper()
	out := map[string]archiveEntry{}
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		out[strings.TrimSuffix(header.Name, "/")] = archiveEntry{
			body: string(body), mode: header.Mode, dir: header.FileInfo().IsDir(),
		}
	}
}

func TestArchiveDirectorySnapshotsPlainDirectory(t *testing.T) {
	t.Setenv("PATH", "")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("local=value"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ignored.txt"), []byte("included"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "script.sh"), []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	source, data, err := ArchiveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	wantSource, _ := filepath.Abs(root)
	if source != wantSource {
		t.Fatalf("source=%q want %q", source, wantSource)
	}
	entries := readArchive(t, data)
	if got := entries[".env"].body; got != "local=value" {
		t.Fatalf(".env=%q", got)
	}
	if got := entries["ignored.txt"].body; got != "included" {
		t.Fatalf("ignored.txt=%q", got)
	}
	if got := entries["nested/script.sh"]; got.body != "#!/bin/sh\necho ok\n" || got.mode&0o111 == 0 {
		t.Fatalf("script=%+v", got)
	}
	if got := entries["nested/empty"]; !got.dir {
		t.Fatalf("empty directory=%+v", got)
	}
}

func TestArchiveDirectoryUsesExactDirectoryAndExcludesGitMetadata(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "chosen")
	if err := os.MkdirAll(filepath.Join(source, ".git", "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "nested", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "inside.txt"), []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".git", "HEAD"), []byte("secret metadata"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, data, err := ArchiveDirectory(source)
	if err != nil {
		t.Fatal(err)
	}
	entries := readArchive(t, data)
	if _, ok := entries["inside.txt"]; !ok {
		t.Fatal("selected directory file missing")
	}
	for name := range entries {
		if name == "outside.txt" || name == ".git" || strings.Contains(name, "/.git") {
			t.Fatalf("unexpected archive entry %q", name)
		}
	}
}

func TestArchiveDirectoryRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "target"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, _, err := ArchiveDirectory(root)
	if err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("got %v", err)
	}
}

func TestArchiveDirectoryRejectsInvalidSource(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ArchiveDirectory(file); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("file error=%v", err)
	}
	if _, _, err := ArchiveDirectory(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected missing-directory error")
	}
}

func TestArchiveDirectorySupportsEmptyDirectory(t *testing.T) {
	_, data, err := ArchiveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if entries := readArchive(t, data); len(entries) != 0 {
		t.Fatalf("entries=%v", entries)
	}
}
