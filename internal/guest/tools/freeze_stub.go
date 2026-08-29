//go:build !linux

package tools

import "fmt"

// Freeze is a GOOS link stub so package tools builds on the host. The ioctl
// is freeze_linux.go (guest). Host code does not call this. Not a placeholder
// for host-side filesystem freeze.
func Freeze() error { return fmt.Errorf("FIFREEZE only available on linux") }

// Thaw is the matching GOOS link stub. See Freeze.
func Thaw() error { return fmt.Errorf("FITHAW only available on linux") }
