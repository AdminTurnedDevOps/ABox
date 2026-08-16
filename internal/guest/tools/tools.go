package tools

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	DefaultMaxRead   = 256 << 10
	DefaultMaxOutput = 1 << 20
	DefaultMaxDepth  = 8
	DefaultMaxList   = 2000
)

type Repo struct {
	Root string
}

func (r Repo) Resolve(rel string) (string, error) {
	if rel == "" {
		rel = "."
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute path rejected")
	}
	clean := filepath.Clean(rel)
	if strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("path traversal rejected")
	}
	full := filepath.Join(r.Root, clean)
	absRoot, err := filepath.Abs(r.Root)
	if err != nil {
		return "", err
	}
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	relToRoot, err := filepath.Rel(absRoot, absFull)
	if err != nil || strings.HasPrefix(relToRoot, "..") {
		return "", fmt.Errorf("path escapes repository")
	}
	if fi, err := os.Lstat(absFull); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		target, err := filepath.EvalSymlinks(absFull)
		if err != nil {
			return "", fmt.Errorf("symlink: %w", err)
		}
		relTarget, err := filepath.Rel(absRoot, target)
		if err != nil || strings.HasPrefix(relTarget, "..") {
			return "", fmt.Errorf("symlink escapes repository")
		}
	}
	return absFull, nil
}

func (r Repo) List(rel string, depth, limit int) ([]string, error) {
	if depth <= 0 || depth > DefaultMaxDepth {
		depth = DefaultMaxDepth
	}
	if limit <= 0 || limit > DefaultMaxList {
		limit = DefaultMaxList
	}
	start, err := r.Resolve(rel)
	if err != nil {
		return nil, err
	}
	var out []string
	err = filepath.WalkDir(start, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(r.Root, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}
		relPath = filepath.ToSlash(relPath)
		if strings.Count(relPath, "/") >= depth {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		out = append(out, relPath)
		if len(out) >= limit {
			return fs.SkipAll
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func (r Repo) Read(rel string, maxBytes int) (content string, binary, trunc bool, err error) {
	if maxBytes <= 0 || maxBytes > DefaultMaxRead {
		maxBytes = DefaultMaxRead
	}
	path, err := r.Resolve(rel)
	if err != nil {
		return "", false, false, err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", false, false, err
	}
	defer f.Close()
	buf := make([]byte, maxBytes+1)
	n, err := io.ReadFull(f, buf)
	if err == io.ErrUnexpectedEOF || err == io.EOF {
		err = nil
	}
	if err != nil {
		return "", false, false, err
	}
	trunc = n > maxBytes
	if trunc {
		n = maxBytes
	}
	data := buf[:n]
	if !utf8.Valid(data) {
		return "", true, trunc, nil
	}
	return string(data), false, trunc, nil
}

func (r Repo) Search(query, rel string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 50
	}
	start, err := r.Resolve(rel)
	if err != nil {
		return nil, err
	}
	var matches []string
	err = filepath.WalkDir(start, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		relPath, _ := filepath.Rel(r.Root, path)
		nameHit := strings.Contains(strings.ToLower(d.Name()), strings.ToLower(query))
		if nameHit {
			matches = append(matches, filepath.ToSlash(relPath))
		} else {
			data, err := os.ReadFile(path)
			if err == nil && utf8.Valid(data) && bytes.Contains(bytes.ToLower(data), bytes.ToLower([]byte(query))) {
				matches = append(matches, filepath.ToSlash(relPath))
			}
		}
		if len(matches) >= limit {
			return fs.SkipAll
		}
		return nil
	})
	return matches, err
}

func (r Repo) ApplyPatch(patch string) (string, error) {
	if len(patch) > DefaultMaxOutput {
		return "", fmt.Errorf("patch too large")
	}
	cmd := exec.Command("git", "apply", "--whitespace=nowarn", "-")
	cmd.Dir = r.Root
	cmd.Stdin = strings.NewReader(patch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		cmd = exec.Command("patch", "-p1", "--forward")
		cmd.Dir = r.Root
		cmd.Stdin = strings.NewReader(patch)
		out2, err2 := cmd.CombinedOutput()
		if err2 != nil {
			return string(out) + string(out2), fmt.Errorf("apply patch: %w", err2)
		}
		return string(out2), nil
	}
	return string(out), nil
}

func (r Repo) Run(command, workdir string, timeout time.Duration, maxOut int) (exit int, stdout, stderr string, dur time.Duration, trunc bool, err error) {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	if maxOut <= 0 {
		maxOut = DefaultMaxOutput
	}
	dir := r.Root
	if workdir != "" {
		resolved, err := r.Resolve(workdir)
		if err != nil {
			return -1, "", "", 0, false, err
		}
		dir = resolved
	}
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"LANG=C",
		"TERM=dumb",
		"TMPDIR=/tmp",
		"GIT_AUTHOR_NAME=abox-guest",
		"GIT_AUTHOR_EMAIL=abox-guest@abox.local",
		"GIT_COMMITTER_NAME=abox-guest",
		"GIT_COMMITTER_EMAIL=abox-guest@abox.local",
	}
	cmd.Stdin = bytes.NewReader(nil)
	var outBuf, errBuf limitedBuffer
	outBuf.limit = maxOut
	errBuf.limit = maxOut
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	start := time.Now()
	err = runWithTimeout(cmd, timeout)
	dur = time.Since(start)
	if cmd.ProcessState != nil {
		exit = cmd.ProcessState.ExitCode()
	} else if err != nil {
		exit = -1
	}
	return exit, outBuf.String(), errBuf.String(), dur, outBuf.trunc || errBuf.trunc, err
}

func (r Repo) InitBaseline() error {
	if err := os.MkdirAll(r.Root, 0o755); err != nil {
		return err
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = r.Root
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"GIT_AUTHOR_NAME=abox-guest",
		"GIT_AUTHOR_EMAIL=abox-guest@abox.local",
		"GIT_COMMITTER_NAME=abox-guest",
		"GIT_COMMITTER_EMAIL=abox-guest@abox.local",
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git init: %w: %s", err, out)
	}
	cfg := exec.Command("git", "config", "user.email", "abox-guest@abox.local")
	cfg.Dir = r.Root
	_ = cfg.Run()
	cfg = exec.Command("git", "config", "user.name", "abox-guest")
	cfg.Dir = r.Root
	_ = cfg.Run()
	add := exec.Command("git", "add", "-A")
	add.Dir = r.Root
	_ = add.Run()
	commit := exec.Command("git", "commit", "--allow-empty", "-m", "abox baseline")
	commit.Dir = r.Root
	commit.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"GIT_AUTHOR_NAME=abox-guest",
		"GIT_AUTHOR_EMAIL=abox-guest@abox.local",
		"GIT_COMMITTER_NAME=abox-guest",
		"GIT_COMMITTER_EMAIL=abox-guest@abox.local",
	}
	if out, err := commit.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit baseline: %w: %s", err, out)
	}
	return nil
}

func (r Repo) ExportPatch() (string, string, error) {
	cmd := exec.Command("git", "diff", "HEAD")
	cmd.Dir = r.Root
	out, err := cmd.Output()
	if err != nil {
		return "", "", err
	}
	stat := exec.Command("git", "diff", "--stat", "HEAD")
	stat.Dir = r.Root
	summary, _ := stat.Output()
	untracked := exec.Command("git", "ls-files", "--others", "--exclude-standard")
	untracked.Dir = r.Root
	extra, _ := untracked.Output()
	if len(bytes.TrimSpace(extra)) > 0 {
		summary = append(summary, []byte("\nuntracked:\n")...)
		summary = append(summary, extra...)
	}
	return string(out), string(summary), nil
}

func ExtractTar(r io.Reader, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	tr := tar.NewReader(r)
	var files, bytesN int
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		files++
		if files > 20000 {
			return fmt.Errorf("too many files")
		}
		if hdr.Size > 32<<20 {
			return fmt.Errorf("file too large")
		}
		name := filepath.Clean(hdr.Name)
		if filepath.IsAbs(name) || strings.HasPrefix(name, "..") {
			return fmt.Errorf("unsafe path %q", hdr.Name)
		}
		target := filepath.Join(dest, name)
		rel, err := filepath.Rel(dest, target)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("path escapes dest")
		}
		switch hdr.Typeflag {
		case tar.TypeXHeader, tar.TypeXGlobalHeader, tar.TypeGNULongName, tar.TypeGNULongLink:
			_, _ = io.Copy(io.Discard, tr)
			continue
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, hdr.FileInfo().Mode().Perm())
			if err != nil {
				return err
			}
			n, err := io.Copy(f, io.LimitReader(tr, hdr.Size+1))
			f.Close()
			if err != nil {
				return err
			}
			if n > hdr.Size {
				return fmt.Errorf("file size mismatch")
			}
			bytesN += int(n)
			if bytesN > 256<<20 {
				return fmt.Errorf("archive too large")
			}
		case tar.TypeSymlink:
			if filepath.IsAbs(hdr.Linkname) || strings.Contains(filepath.Clean(hdr.Linkname), "..") {
				return fmt.Errorf("unsafe symlink")
			}
			_ = os.Symlink(hdr.Linkname, target)
		default:
			return fmt.Errorf("unsupported tar type %v", hdr.Typeflag)
		}
	}
}

type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
	trunc bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	remain := l.limit - l.buf.Len()
	if remain <= 0 {
		l.trunc = true
		return len(p), nil
	}
	if len(p) > remain {
		l.trunc = true
		_, _ = l.buf.Write(p[:remain])
		return len(p), nil
	}
	return l.buf.Write(p)
}

func (l *limitedBuffer) String() string { return l.buf.String() }

func runWithTimeout(cmd *exec.Cmd, d time.Duration) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(d):
		_ = cmd.Process.Kill()
		<-done
		return fmt.Errorf("timeout after %s", d)
	}
}
