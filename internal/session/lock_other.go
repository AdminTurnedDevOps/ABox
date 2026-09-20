//go:build !linux && !darwin

package session

import "fmt"

func (s *Session) AcquireRuntimeLock() (bool, error) {
	return false, fmt.Errorf("session runtime locks are unsupported on this platform")
}

func (s *Session) ReleaseRuntimeLock() error { return nil }
