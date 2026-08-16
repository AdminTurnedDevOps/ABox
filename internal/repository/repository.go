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
	Root string
	HEAD string
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
	return Snapshot{Root: root, HEAD: head}, nil
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
