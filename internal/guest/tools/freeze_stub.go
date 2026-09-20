//go:build !linux || !abox_guest

package tools

import "fmt"

// Freeze is deliberately selected by untagged host builds, including Linux.
// Only an explicit abox_guest build may contain the root-filesystem ioctl.
func Freeze() error { return fmt.Errorf("FIFREEZE only available in a tagged Linux guest build") }

// Thaw is the matching GOOS link stub. See Freeze.
func Thaw() error { return fmt.Errorf("FITHAW only available in a tagged Linux guest build") }
