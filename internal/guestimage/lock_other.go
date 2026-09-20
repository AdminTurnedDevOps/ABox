//go:build !linux && !darwin

package guestimage

import (
	"fmt"
	"os"
)

type Lock struct{}

func AcquireSharedLock(string) (*Lock, error) {
	return nil, fmt.Errorf("image generation locks are unsupported on this platform")
}

func (l *Lock) Close() error { return nil }

func openManifest(path string) (*os.File, error) { return os.Open(path) }
