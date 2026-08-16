//go:build linux

package tools

import (
	"os"
	"syscall"
	"unsafe"
)

// linux/fs.h FIFREEZE / FITHAW (_IOWR('X', 119/120, int))
const (
	fiFreeze = 0xC0045877
	fiThaw   = 0xC0045878
)

func Freeze() error {
	f, err := os.Open("/")
	if err != nil {
		return err
	}
	defer f.Close()
	var dummy int
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(fiFreeze), uintptr(unsafe.Pointer(&dummy)))
	if errno != 0 {
		return errno
	}
	return nil
}

func Thaw() error {
	f, err := os.Open("/")
	if err != nil {
		return err
	}
	defer f.Close()
	var dummy int
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(fiThaw), uintptr(unsafe.Pointer(&dummy)))
	if errno != 0 {
		return errno
	}
	return nil
}
