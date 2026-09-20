//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	kvmGetAPIVersion = 0xae00
	kvmCreateVM      = 0xae01
	kvmAPIVersion    = 12
)

func platformPreflight() error {
	if data, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		release := strings.ToLower(string(data))
		if strings.Contains(release, "microsoft") || strings.Contains(release, "wsl") {
			return fmt.Errorf("WSL is unsupported for ABox VMM execution; use a physical Linux KVM host")
		}
	}
	if runningInContainer() {
		return fmt.Errorf("containerized ABox VMM execution is unsupported; containers are build-only environments")
	}

	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		switch {
		case errors.Is(err, os.ErrNotExist):
			return fmt.Errorf("/dev/kvm is unavailable; enable virtualization in firmware and load kvm_intel or kvm_amd")
		case errors.Is(err, os.ErrPermission):
			return fmt.Errorf("cannot open /dev/kvm: %w; add the user to the kvm group, then log out and back in", err)
		default:
			return fmt.Errorf("open /dev/kvm: %w", err)
		}
	}
	defer f.Close()

	version, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), kvmGetAPIVersion, 0)
	if errno != 0 {
		return fmt.Errorf("query KVM API version: %w; if this host is virtualized, enable nested virtualization", errno)
	}
	if int(version) != kvmAPIVersion {
		return fmt.Errorf("unsupported KVM API version %d; expected %d", version, kvmAPIVersion)
	}
	vmfd, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), kvmCreateVM, 0)
	if errno != 0 {
		message := fmt.Sprintf("create KVM VM: %v; if this host is virtualized, enable nested virtualization", errno)
		if errno == syscall.EACCES || errno == syscall.EPERM {
			message += "; on Fedora with correct file permissions, inspect SELinux AVCs with ausearch or journalctl rather than disabling SELinux"
		}
		return errors.New(message)
	}
	if err := unix.Close(int(vmfd)); err != nil {
		return fmt.Errorf("close KVM preflight VM: %w", err)
	}
	return nil
}

func runningInContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		return true
	}
	if _, err := os.Stat("/run/systemd/container"); err == nil {
		return true
	}
	if _, err := os.Stat("/usr/bin/systemd-detect-virt"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if exec.CommandContext(ctx, "/usr/bin/systemd-detect-virt", "--quiet", "--container").Run() == nil {
			return true
		}
	}
	data, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	text := strings.ToLower(string(data))
	for _, marker := range []string{"docker", "kubepods", "containerd", "libpod", "lxc"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
