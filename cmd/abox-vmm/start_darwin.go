//go:build darwin && arm64

package main

/*
#cgo CFLAGS: -I/opt/homebrew/include
#cgo LDFLAGS: -L/opt/homebrew/lib -lkrun -lkrunfw -Wl,-rpath,/opt/homebrew/lib
#include <libkrun.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func startVM(cfg VMMConfig) error {
	ctx := C.krun_create_ctx()
	if ctx < 0 {
		return fmt.Errorf("krun_create_ctx: %d", int(ctx))
	}
	id := C.uint32_t(ctx)

	if rc := C.krun_set_vm_config(id, C.uint8_t(cfg.VCPU), C.uint32_t(cfg.RAMMiB)); rc < 0 {
		return fmt.Errorf("krun_set_vm_config: %d", int(rc))
	}

	if rc := C.krun_has_feature(C.KRUN_FEATURE_BLK); rc != 1 {
		return fmt.Errorf("libkrun build lacks block devices (krun_has_feature=%d)", int(rc))
	}

	if rc := C.krun_disable_implicit_vsock(id); rc < 0 {
		return fmt.Errorf("krun_disable_implicit_vsock: %d", int(rc))
	}
	if rc := C.krun_add_vsock(id, C.KRUN_TSI_HIJACK_INET); rc < 0 {
		return fmt.Errorf("krun_add_vsock(TSI_INET): %d", int(rc))
	}

	sock := C.CString(cfg.RPCSocket)
	defer C.free(unsafe.Pointer(sock))
	if rc := C.krun_add_vsock_port(id, C.uint32_t(cfg.VsockPort), sock); rc < 0 {
		return fmt.Errorf("krun_add_vsock_port: %d", int(rc))
	}

	root := C.CString(cfg.RootDisk)
	defer C.free(unsafe.Pointer(root))
	rootID := C.CString("root")
	defer C.free(unsafe.Pointer(rootID))
	if rc := C.krun_add_disk3(id, rootID, root, C.KRUN_DISK_FORMAT_RAW, false, false, C.KRUN_SYNC_FULL); rc < 0 {
		return fmt.Errorf("krun_add_disk3 root: %d", int(rc))
	}

	if cfg.ConfigDisk != "" {
		cfgPath := C.CString(cfg.ConfigDisk)
		defer C.free(unsafe.Pointer(cfgPath))
		cfgID := C.CString("config")
		defer C.free(unsafe.Pointer(cfgID))
		if rc := C.krun_add_disk3(id, cfgID, cfgPath, C.KRUN_DISK_FORMAT_RAW, true, false, C.KRUN_SYNC_FULL); rc < 0 {
			return fmt.Errorf("krun_add_disk3 config: %d", int(rc))
		}
	}

	dev := C.CString("/dev/vda")
	defer C.free(unsafe.Pointer(dev))
	fstype := C.CString("ext4")
	defer C.free(unsafe.Pointer(fstype))
	if rc := C.krun_set_root_disk_remount(id, dev, fstype, nil); rc < 0 {
		return fmt.Errorf("krun_set_root_disk_remount: %d", int(rc))
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
		return fmt.Errorf("krun_set_exec: %d", int(rc))
	}

	if cfg.ConsoleLog != "" {
		clog := C.CString(cfg.ConsoleLog)
		defer C.free(unsafe.Pointer(clog))
		_ = C.krun_set_console_output(id, clog)
	}

	rc := C.krun_start_enter(id)
	return fmt.Errorf("krun_start_enter returned %d", int(rc))
}
