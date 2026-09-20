package runtime

import (
	"errors"
	"fmt"
	"io"
	"os"
)

func cloneFile(src, dst string) (retErr error) {
	_ = os.Remove(dst)
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("clone source is not a regular file: %s", src)
	}
	if err := platformCloneFile(src, dst); err == nil {
		if err := os.Chmod(dst, 0o600); err != nil {
			_ = os.Remove(dst)
			return err
		}
		return nil
	}

	// A failed fast path may have created or partially populated dst.
	_ = os.Remove(dst)
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, in.Close())
		if retErr != nil {
			_ = os.Remove(dst)
		}
	}()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, out.Close())
		if retErr != nil {
			_ = os.Remove(dst)
		}
	}()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return os.Chmod(dst, 0o600)
}
