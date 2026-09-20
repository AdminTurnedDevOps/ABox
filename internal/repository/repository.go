// Package repository snapshots a host source directory for transfer into the
// guest. It does not inspect or modify host version-control state.
package repository

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/AdminTurnedDevOps/ABox/protocol"
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

// OpenForSession uses a clean committed worktree when one exists. Otherwise it
// copies tracked and non-ignored untracked files into a private repository.
func OpenForSession(start, scratchDir string) (Snapshot, error) {
	return OpenForSessionExcluding(start, scratchDir)
}

// OpenForSessionExcluding also omits host-only paths such as ~/.abox.
func OpenForSessionExcluding(start, scratchDir string, excludedPaths ...string) (Snapshot, error) {
	root, err := TopLevel(start)
	if err != nil {
		return Snapshot{}, fmt.Errorf("not a git worktree: %w", err)
	}
	excluded, err := exclusionsWithin(root, excludedPaths)
	if err != nil {
		return Snapshot{}, err
	}
	if len(excluded) == 0 {
		if snap, err := ValidateClean(root); err == nil {
			return snap, nil
		}
	}
	if hasUnsupportedSubmodules(root) {
		return Snapshot{}, fmt.Errorf("submodules are not supported in milestone one")
	}
	if err := copyWorktreeExcluding(root, scratchDir, excluded); err != nil {
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
		return nil, fmt.Errorf("git archive: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	archive := stdout.Bytes()
	if err := validateArchive(archive); err != nil {
		return nil, err
	}
	return archive, nil
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

func exclusionsWithin(root string, paths []string) ([]string, error) {
	var excluded []string
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve excluded path: %w", err)
		}
		absolute = filepath.Clean(absolute)
		if absolute == filepath.Clean(root) {
			return nil, fmt.Errorf("source directory %q is host-only ABox state; run abox from a Git worktree", root)
		}
		if pathWithin(root, absolute) {
			excluded = append(excluded, absolute)
		}
	}
	return excluded, nil
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func copyWorktreeExcluding(src, dst string, excluded []string) error {
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
		path := filepath.Join(src, clean)
		skip := false
		for _, excludedPath := range excluded {
			if pathWithin(excludedPath, path) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		var info os.FileInfo
		current := src
		parts := strings.Split(clean, string(filepath.Separator))
		for i, part := range parts {
			current = filepath.Join(current, part)
			info, err = os.Lstat(current)
			if err != nil {
				break
			}
			if info.Mode()&os.ModeSymlink != 0 || i < len(parts)-1 && !info.IsDir() {
				return fmt.Errorf("unsupported file type %q", rel)
			}
		}
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported file type %q", rel)
		}
		if info.Size() > protocol.MaxArchiveFile {
			return fmt.Errorf("file %q exceeds %d bytes", path, protocol.MaxArchiveFile)
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
		{"git", "add", "-f", "-A"},
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

func validateArchive(data []byte) error {
	tr := tar.NewReader(bytes.NewReader(data))
	entries := 0
	totalBytes := int64(0)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("validate repository archive: %w", err)
		}
		switch header.Typeflag {
		case tar.TypeXHeader, tar.TypeXGlobalHeader, tar.TypeGNULongName, tar.TypeGNULongLink:
			continue
		case tar.TypeDir, tar.TypeReg, tar.TypeRegA:
		default:
			return fmt.Errorf("unsupported archive file type %q", header.Name)
		}
		clean := filepath.Clean(filepath.FromSlash(header.Name))
		if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe repository archive path %q", header.Name)
		}
		entries++
		if entries > protocol.MaxArchiveFiles {
			return fmt.Errorf("source directory has more than %d entries", protocol.MaxArchiveFiles)
		}
		if header.Size > protocol.MaxArchiveFile {
			return fmt.Errorf("file %q exceeds %d bytes", header.Name, protocol.MaxArchiveFile)
		}
		totalBytes += header.Size
		if totalBytes > protocol.MaxArchiveBytes {
			return fmt.Errorf("source directory exceeds %d bytes", protocol.MaxArchiveBytes)
		}
	}
}
