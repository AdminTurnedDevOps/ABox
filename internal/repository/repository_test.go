package repository

import (
	"archive/tar"
	"bytes"
	"io"
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

func TestOpenForSessionEphemeral(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(dir, "script.sh"), []byte("#!/bin/sh\necho clean\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tracked.log"), []byte("clean"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "deleted.txt"), []byte("remove me"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", "script.sh", "tracked.log", "deleted.txt")
	run("git", "commit", "-m", "init")

	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.env\n*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "info", "exclude"), []byte("info-secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "script.sh"), []byte("#!/bin/sh\necho dirty\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tracked.log"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.env"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "info-secret"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	oddName := "untracked\nfile.txt"
	if err := os.WriteFile(filepath.Join(subdir, oddName), []byte("included"), 0o644); err != nil {
		t.Fatal(err)
	}

	scratch := t.TempDir()
	snap, err := OpenForSession(subdir, scratch)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Ephemeral {
		t.Fatal("expected ephemeral snapshot")
	}
	hostInfo, err := os.Stat(snap.HostSource)
	if err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(hostInfo, dirInfo) {
		t.Fatalf("HostSource=%q is not repository root %q", snap.HostSource, dir)
	}
	for name, want := range map[string]string{
		"script.sh":                      "#!/bin/sh\necho dirty\n",
		"tracked.log":                    "dirty",
		".gitignore":                     "*.env\n*.log\n",
		filepath.Join("subdir", oddName): "included",
	} {
		got, err := os.ReadFile(filepath.Join(snap.Root, name))
		if err != nil {
			t.Fatalf("read %q: %v", name, err)
		}
		if string(got) != want {
			t.Fatalf("%q=%q want %q", name, got, want)
		}
	}
	for _, name := range []string{"deleted.txt", "ignored.env", "info-secret"} {
		if _, err := os.Stat(filepath.Join(snap.Root, name)); !os.IsNotExist(err) {
			t.Fatalf("excluded file %q entered host-tree: %v", name, err)
		}
	}
	info, err := os.Stat(filepath.Join(snap.Root, "script.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("script mode=%o, executable bit lost", info.Mode().Perm())
	}

	archive, err := ArchiveHEAD(snap.Root)
	if err != nil {
		t.Fatal(err)
	}
	archived := make(map[string]int64)
	tr := tar.NewReader(bytes.NewReader(archive))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		archived[hdr.Name] = hdr.Mode
	}
	for _, name := range []string{"deleted.txt", "ignored.env", "info-secret"} {
		if _, ok := archived[name]; ok {
			t.Fatalf("excluded file %q entered archive", name)
		}
	}
	if archived["script.sh"]&0o111 == 0 {
		t.Fatalf("archived script mode=%o, executable bit lost", archived["script.sh"])
	}
	if _, ok := archived[filepath.ToSlash(filepath.Join("subdir", oddName))]; !ok {
		t.Fatalf("NUL-delimited untracked name missing from archive: %#v", archived)
	}
}

func TestOpenForSessionRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "target"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := OpenForSession(dir, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("got %v", err)
	}
}

func TestOpenForSessionRejectsSymlinkedDirectory(t *testing.T) {
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
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "file.txt"), []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", "nested/file.txt")
	run("git", "commit", "-m", "init")

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "file.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(nested); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, nested); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := OpenForSession(dir, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("got %v", err)
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
