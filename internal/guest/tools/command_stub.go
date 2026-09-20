//go:build !linux || !abox_guest

package tools

import "os/exec"

func configureGuestCommand(*exec.Cmd)      {}
func cleanupGuestCommand(*exec.Cmd)        {}
func setGuestOwnership(string, bool) error { return nil }
