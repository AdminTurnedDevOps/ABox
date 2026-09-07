package repository

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Snapshot struct {
	Root       string
	HEAD       string
	Ephemeral  bool
	HostSource string
}

func ValidateClean(start string) (Snapshot, error) {
	root, err := gitOutput(start, "rev-parse", "--show-toplevel")
	if err != nil {
		return Snapshot{}, fmt.Errorf("not a git worktree: %w", err)
	}
	head, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		return Snapshot{}, fmt.Errorf("repository has no commits; create an initial commit so ABox can snapshot HEAD")
	}
	status, err := gitOutput(root, "status", "--porcelain")
	if err != nil {
		return Snapshot{}, fmt.Errorf("git status: %w", err)
	}
	if strings.TrimSpace(status) != "" {
		return Snapshot{}, fmt.Errorf("worktree is not clean; commit or stash before starting ABox")
	}
	if hasUnsupportedSubmodules(root) {
		return Snapshot{}, fmt.Errorf("submodules are not supported in milestone one")
	}
	return Snapshot{Root: root, HEAD: head, HostSource: root}, nil
}

// OpenForSession uses a clean committed worktree when one exists.
// Otherwise it copies the repository worktree into scratchDir, makes a
// private commit there, and returns that. The host Git repo is not changed.
func OpenForSession(start, scratchDir string) (Snapshot, error) {
	root, err := TopLevel(start)
	if err != nil {
		return Snapshot{}, fmt.Errorf("not a git worktree: %w", err)
	}
	if snap, err := ValidateClean(root); err == nil {
		return snap, nil
	}
	if hasUnsupportedSubmodules(root) {
		return Snapshot{}, fmt.Errorf("submodules are not supported in milestone one")
	}
	if err := copyWorktree(root, scratchDir); err != nil {
		return Snapshot{}, fmt.Errorf("ephemeral snapshot: %w", err)
	}
	if err := initScratchRepo(scratchDir); err != nil {
		return Snapshot{}, err
	}
	snap, err := ValidateClean(scratchDir)
	if err != nil {
		return Snapshot{}, fmt.Errorf("ephemeral snapshot: %w", err)
	}
	snap.Ephemeral = true
	snap.HostSource = root
	return snap, nil
}

func StillClean(s Snapshot) error {
	cur, err := ValidateClean(s.Root)
	if err != nil {
		return err
	}
	if cur.HEAD != s.HEAD {
		return fmt.Errorf("host HEAD moved from %s to %s", s.HEAD, cur.HEAD)
	}
	return nil
}

func ArchiveHEAD(root string) ([]byte, error) {
	cmd := exec.Command("git", "archive", "--format=tar", "HEAD")
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git archive: %w: %s", err, stderr.String())
	}
	return stdout.Bytes(), nil
}

func TopLevel(start string) (string, error) {
	return gitOutput(start, "rev-parse", "--show-toplevel")
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func hasUnsupportedSubmodules(root string) bool {
	_, err := os.Stat(filepath.Join(root, ".gitmodules"))
	return err == nil
}

func copyWorktree(src, dst string) error {
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	cmd.Dir = src
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git ls-files: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	for _, name := range bytes.Split(out, []byte{0}) {
		if len(name) == 0 {
			continue
		}
		rel := filepath.FromSlash(string(name))
		clean := filepath.Clean(rel)
		if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe repository path %q", rel)
		}
		path := src
		var info os.FileInfo
		parts := strings.Split(clean, string(filepath.Separator))
		for i, part := range parts {
			path = filepath.Join(path, part)
			info, err = os.Lstat(path)
			if err != nil {
				break
			}
			if info.Mode()&os.ModeSymlink != 0 || i < len(parts)-1 && !info.IsDir() {
				return fmt.Errorf("unsupported file type %q", rel)
			}
		}
		if err != nil {
			if os.IsNotExist(err) {
				continue // A tracked file deleted in the dirty worktree stays deleted.
			}
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported file type %q", rel)
		}
		target := filepath.Join(dst, clean)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil {
			return err
		}
		if err := os.Chmod(target, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

func initScratchRepo(dir string) error {
	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "add", "-A"},
		{"git", "commit", "--allow-empty", "-m", "abox ephemeral snapshot"},
	}
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=abox",
		"GIT_AUTHOR_EMAIL=abox@local",
		"GIT_COMMITTER_NAME=abox",
		"GIT_COMMITTER_EMAIL=abox@local",
	)
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %w: %s", strings.Join(args, " "), err, out)
		}
	}
	return nil
}
