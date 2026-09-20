//go:build linux

package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCloneFileFallsBackAfterUnsupportedFastPaths(t *testing.T) {
	originalClone := ioctlFileClone
	originalCopy := copyFileRange
	t.Cleanup(func() {
		ioctlFileClone = originalClone
		copyFileRange = originalCopy
	})

	ioctlFileClone = func(_, _ int) error { return syscall.EOPNOTSUPP }
	copyFileRange = func(_ int, _ *int64, _ int, _ *int64, _ int, _ int) (int, error) {
		return 0, syscall.EXDEV
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	want := []byte("fallback copy")
	if err := os.WriteFile(src, want, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cloneFile(src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCloneFileResetsAfterPartialCopyFileRange(t *testing.T) {
	originalClone := ioctlFileClone
	originalCopy := copyFileRange
	t.Cleanup(func() {
		ioctlFileClone = originalClone
		copyFileRange = originalCopy
	})

	ioctlFileClone = func(_, _ int) error { return syscall.EOPNOTSUPP }
	calls := 0
	copyFileRange = func(srcFD int, _ *int64, dstFD int, _ *int64, _ int, _ int) (int, error) {
		calls++
		if calls == 1 {
			buf := make([]byte, 4)
			n, err := syscall.Read(srcFD, buf)
			if err != nil {
				return n, err
			}
			written, err := syscall.Write(dstFD, buf[:n])
			return written, err
		}
		return 0, syscall.EXDEV
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	want := []byte("complete contents after partial fast copy")
	if err := os.WriteFile(src, want, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cloneFile(src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestPlatformCloneResetsDestinationOnCopyFileRangeFailure(t *testing.T) {
	originalClone := ioctlFileClone
	originalCopy := copyFileRange
	t.Cleanup(func() {
		ioctlFileClone = originalClone
		copyFileRange = originalCopy
	})

	ioctlFileClone = func(_, _ int) error { return syscall.EOPNOTSUPP }
	copyFileRange = func(srcFD int, _ *int64, dstFD int, _ *int64, _ int, _ int) (int, error) {
		buf := []byte("partial")
		if _, err := syscall.Read(srcFD, buf); err != nil {
			return 0, err
		}
		if _, err := syscall.Write(dstFD, buf); err != nil {
			return 0, err
		}
		return 0, syscall.EXDEV
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := platformCloneFile(src, dst)
	if !errors.Is(err, syscall.EXDEV) {
		t.Fatalf("error = %v, want EXDEV", err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("destination size = %d, want 0", info.Size())
	}
}
