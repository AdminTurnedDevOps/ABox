// Package repository snapshots a host source directory for transfer into the
// guest. It does not inspect or modify host version-control state.
package repository

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	maxArchiveEntries = 20000
	maxArchiveFile    = 32 << 20
	maxArchiveBytes   = 256 << 20
)

// ArchiveDirectory creates a bounded tar snapshot of exactly sourceDir. Git
// metadata is excluded because the guest creates its own private baseline.
func ArchiveDirectory(sourceDir string) (string, []byte, error) {
	root, err := filepath.Abs(sourceDir)
	if err != nil {
		return "", nil, fmt.Errorf("resolve source directory: %w", err)
	}
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return "", nil, fmt.Errorf("source directory: %w", err)
	}
	if !info.IsDir() {
		return "", nil, fmt.Errorf("source path is not a directory: %s", root)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	entries := 0
	totalBytes := int64(0)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if entry.Name() == ".git" {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		entries++
		if entries > maxArchiveEntries {
			return fmt.Errorf("source directory has more than %d entries", maxArchiveEntries)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported file type %q", path)
		}
		if info.Size() > maxArchiveFile {
			return fmt.Errorf("file %q exceeds %d bytes", path, maxArchiveFile)
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if info.IsDir() {
			header.Name += "/"
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		totalBytes += info.Size()
		if totalBytes > maxArchiveBytes {
			return fmt.Errorf("source directory exceeds %d bytes", maxArchiveBytes)
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		openedInfo, statErr := file.Stat()
		if statErr != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
			file.Close()
			if statErr != nil {
				return statErr
			}
			return fmt.Errorf("source file changed while snapshotting: %q", path)
		}
		_, copyErr := io.CopyN(tw, file, info.Size())
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("snapshot %q: %w", path, copyErr)
		}
		return closeErr
	})
	if err != nil {
		_ = tw.Close()
		return "", nil, err
	}
	if err := tw.Close(); err != nil {
		return "", nil, err
	}
	return root, buf.Bytes(), nil
}
