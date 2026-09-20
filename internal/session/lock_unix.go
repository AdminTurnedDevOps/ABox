//go:build linux || darwin

package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func (s *Session) AcquireRuntimeLock() (bool, error) {
	if s.runtimeLock != nil {
		return false, nil
	}
	path := filepath.Join(s.Dir, "runtime.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false, fmt.Errorf("open session runtime lock: %w", err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return false, fmt.Errorf("session %s is already active", s.ID)
		}
		return false, fmt.Errorf("lock session runtime: %w", err)
	}
	s.runtimeLock = f
	return true, nil
}

func (s *Session) ReleaseRuntimeLock() error {
	if s.runtimeLock == nil {
		return nil
	}
	f := s.runtimeLock
	s.runtimeLock = nil
	err := unix.Flock(int(f.Fd()), unix.LOCK_UN)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
