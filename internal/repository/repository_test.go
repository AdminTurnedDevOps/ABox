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

type archiveEntry struct {
	body string
	mode int64
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=abox-test",
		"GIT_AUTHOR_EMAIL=abox-test@example.invalid",
		"GIT_COMMITTER_NAME=abox-test",
		"GIT_COMMITTER_EMAIL=abox-test@example.invalid",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func sameFile(t *testing.T, left, right string) bool {
	t.Helper()
	leftInfo, err := os.Stat(left)
	if err != nil {
		t.Fatal(err)
	}
	rightInfo, err := os.Stat(right)
	if err != nil {
		t.Fatal(err)
	}
	return os.SameFile(leftInfo, rightInfo)
}

func TestOpenForSessionDiscoversCleanGitRoot(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	if err := os.MkdirAll(filepath.Join(root, "nested", "project"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("tracked"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "initial")

	snap, err := OpenForSession(filepath.Join(root, "nested", "project"), filepath.Join(t.TempDir(), "host-tree"))
	if err != nil {
		t.Fatal(err)
	}
	if snap.Ephemeral || !sameFile(t, snap.Root, root) || !sameFile(t, snap.HostSource, root) || snap.HEAD == "" {
		t.Fatalf("snapshot=%+v", snap)
	}
	archive, err := ArchiveHEAD(snap.Root)
	if err != nil {
		t.Fatal(err)
	}
	if got := readArchive(t, archive)["tracked.txt"].body; got != "tracked" {
		t.Fatalf("tracked.txt=%q", got)
	}
}

func TestOpenForSessionSnapshotsDirtyWorktree(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "script.sh"), []byte("#!/bin/sh\necho clean\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.log"), []byte("clean"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "deleted.txt"), []byte("delete"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "script.sh", "tracked.log", "deleted.txt")
	runGit(t, root, "commit", "-m", "initial")

	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.env\n*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "script.sh"), []byte("#!/bin/sh\necho dirty\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.log"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ignored.env"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	oddName := "untracked\nfile.txt"
	if err := os.WriteFile(filepath.Join(root, "nested", oddName), []byte("included"), 0o644); err != nil {
		t.Fatal(err)
	}

	snap, err := OpenForSession(filepath.Join(root, "nested"), filepath.Join(t.TempDir(), "host-tree"))
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Ephemeral || !sameFile(t, snap.HostSource, root) || sameFile(t, snap.Root, root) {
		t.Fatalf("snapshot=%+v", snap)
	}
	archive, err := ArchiveHEAD(snap.Root)
	if err != nil {
		t.Fatal(err)
	}
	entries := readArchive(t, archive)
	for name, want := range map[string]string{
		"script.sh":   "#!/bin/sh\necho dirty\n",
		"tracked.log": "dirty",
		".gitignore":  "*.env\n*.log\n",
		filepath.ToSlash(filepath.Join("nested", oddName)): "included",
	} {
		if got := entries[name].body; got != want {
			t.Fatalf("%q=%q want %q", name, got, want)
		}
	}
	for _, name := range []string{"deleted.txt", "ignored.env"} {
		if _, ok := entries[name]; ok {
			t.Fatalf("excluded file %q entered archive", name)
		}
	}
	if entries["script.sh"].mode&0o111 == 0 {
		t.Fatalf("script mode=%o", entries["script.sh"].mode)
	}
}

func TestOpenForSessionExcludesHostState(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "project.txt"), []byte("project"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "project.txt")
	runGit(t, root, "commit", "-m", "initial")
	state := filepath.Join(root, ".abox")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "credentials.env"), []byte("SECRET=value"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "repo-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	stateAlias := filepath.Join(alias, ".abox")

	snap, err := OpenForSessionExcluding(alias, filepath.Join(state, "sessions", "test", "host-tree"), stateAlias)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := ArchiveHEAD(snap.Root)
	if err != nil {
		t.Fatal(err)
	}
	entries := readArchive(t, archive)
	if _, ok := entries["project.txt"]; !ok {
		t.Fatal("project file missing")
	}
	for name := range entries {
		if name == ".abox" || strings.HasPrefix(name, ".abox/") {
			t.Fatalf("host state included in archive: %q", name)
		}
	}
}

func TestOpenForSessionRejectsSelectedSymlink(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "target"), []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := OpenForSession(root, filepath.Join(t.TempDir(), "host-tree")); err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("error=%v", err)
	}
}

func TestOpenForSessionRequiresGitWorktree(t *testing.T) {
	_, err := OpenForSession(t.TempDir(), filepath.Join(t.TempDir(), "host-tree"))
	if err == nil || !strings.Contains(err.Error(), "not a git worktree") {
		t.Fatalf("error=%v", err)
	}
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
			body: string(body), mode: header.Mode,
		}
	}
}
