//go:build darwin

package runtime

import "golang.org/x/sys/unix"

func platformCloneFile(src, dst string) error {
	return unix.Clonefile(src, dst, 0)
}
