//go:build linux

package runtime

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

var (
	ioctlFileClone = unix.IoctlFileClone
	copyFileRange  = unix.CopyFileRange
)

func platformCloneFile(src, dst string) (retErr error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, in.Close()) }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, out.Close()) }()

	if err := ioctlFileClone(int(out.Fd()), int(in.Fd())); err == nil {
		return nil
	}
	if err := resetCloneFiles(in, out); err != nil {
		return err
	}

	for {
		n, err := copyFileRange(int(in.Fd()), nil, int(out.Fd()), nil, 1<<30, 0)
		if err != nil {
			resetErr := resetCloneFiles(in, out)
			if resetErr != nil {
				return errors.Join(err, resetErr)
			}
			return err
		}
		if n == 0 {
			return nil
		}
	}
}

func resetCloneFiles(in, out *os.File) error {
	if err := out.Truncate(0); err != nil {
		return err
	}
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err := out.Seek(0, io.SeekStart)
	return err
}
