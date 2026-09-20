//go:build cgo && ((darwin && arm64) || (linux && (amd64 || arm64)))

package main

/*
#include <libkrun.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/AdminTurnedDevOps/ABox/internal/vmmconfig"
)

func startVM(cfg vmmconfig.Config) error {
	if err := platformPreflight(); err != nil {
		return err
	}
	ctx := C.krun_create_ctx()
	if ctx < 0 {
		return libkrunError("create VM context", int(ctx))
	}
	id := C.uint32_t(ctx)
	owned := true
	defer func() {
		if owned {
			C.krun_free_ctx(id)
		}
	}()

	if rc := C.krun_set_vm_config(id, C.uint8_t(cfg.VCPU), C.uint32_t(cfg.RAMMiB)); rc < 0 {
		return libkrunError("configure VM resources", int(rc))
	}

	if rc := C.krun_has_feature(C.KRUN_FEATURE_BLK); rc != 1 {
		return fmt.Errorf("installed libkrun build does not provide required block-device support")
	}

	if rc := C.krun_disable_implicit_vsock(id); rc < 0 {
		return libkrunError("disable implicit vsock forwarding", int(rc))
	}
	if rc := C.krun_add_vsock(id, 0); rc < 0 {
		return libkrunError("add guest vsock device", int(rc))
	}

	sock := C.CString(cfg.RPCSocket)
	defer C.free(unsafe.Pointer(sock))
	if rc := C.krun_add_vsock_port(id, C.uint32_t(cfg.VsockPort), sock); rc < 0 {
		return libkrunError("add guest RPC vsock port", int(rc))
	}

	root := C.CString(cfg.RootDisk)
	defer C.free(unsafe.Pointer(root))
	rootID := C.CString("root")
	defer C.free(unsafe.Pointer(rootID))
	if rc := C.krun_add_disk3(id, rootID, root, C.KRUN_DISK_FORMAT_RAW, false, false, C.KRUN_SYNC_FULL); rc < 0 {
		return libkrunError("attach root disk", int(rc))
	}

	if cfg.ConfigDisk != "" {
		cfgPath := C.CString(cfg.ConfigDisk)
		defer C.free(unsafe.Pointer(cfgPath))
		cfgID := C.CString("config")
		defer C.free(unsafe.Pointer(cfgID))
		if rc := C.krun_add_disk3(id, cfgID, cfgPath, C.KRUN_DISK_FORMAT_RAW, true, false, C.KRUN_SYNC_FULL); rc < 0 {
			return libkrunError("attach read-only config disk", int(rc))
		}
	}

	dev := C.CString("/dev/vda")
	defer C.free(unsafe.Pointer(dev))
	fstype := C.CString("ext4")
	defer C.free(unsafe.Pointer(fstype))
	if rc := C.krun_set_root_disk_remount(id, dev, fstype, nil); rc < 0 {
		return libkrunError("configure root disk", int(rc))
	}

	execPath := C.CString(cfg.ExecPath)
	defer C.free(unsafe.Pointer(execPath))
	arg0 := C.CString("abox-guest")
	defer C.free(unsafe.Pointer(arg0))
	argv := []*C.char{arg0, nil}

	envVals := []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"LANG=C",
		"TERM=dumb",
		"TMPDIR=/tmp",
		"SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt",
	}
	envp := make([]*C.char, 0, len(envVals)+1)
	for _, e := range envVals {
		cs := C.CString(e)
		defer C.free(unsafe.Pointer(cs))
		envp = append(envp, cs)
	}
	envp = append(envp, nil)

	if rc := C.krun_set_exec(id, execPath, &argv[0], &envp[0]); rc < 0 {
		return libkrunError("configure guest process", int(rc))
	}

	if cfg.ConsoleLog != "" {
		clog := C.CString(cfg.ConsoleLog)
		defer C.free(unsafe.Pointer(clog))
		if rc := C.krun_set_console_output(id, clog); rc < 0 {
			return libkrunError("configure guest console", int(rc))
		}
	}

	// krun_start_enter consumes the context even when it returns an error.
	owned = false
	rc := C.krun_start_enter(id)
	return libkrunError("start VM", int(rc))
}

func libkrunError(operation string, rc int) error {
	if rc >= 0 {
		return fmt.Errorf("%s ended unexpectedly", operation)
	}
	errno := syscall.Errno(-rc)
	switch errno {
	case syscall.ENOENT:
		return fmt.Errorf("%s: %w; verify the libkrunfw package required by the installed libkrun build and refresh the loader cache", operation, errno)
	case syscall.EACCES, syscall.EPERM:
		return fmt.Errorf("%s: %w; verify disk permissions and inspect SELinux AVCs on Fedora rather than disabling SELinux", operation, errno)
	case syscall.ENODEV, syscall.ENOSYS:
		return fmt.Errorf("%s: %w; verify KVM support and the installed libkrun/libkrunfw package pair", operation, errno)
	default:
		return fmt.Errorf("%s: %w", operation, errno)
	}
}
