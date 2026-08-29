//go:build !darwin || !arm64

package main

import (
	"fmt"

	"github.com/AdminTurnedDevOps/ABox/internal/vmmconfig"
)

// startVM is a GOOS link stub: main.go always calls it, so this OS needs a
// definition. The libkrun implementation is start_darwin_arm64.go (macOS
// Apple Silicon). Not a placeholder for a Linux/Windows VMM.
func startVM(cfg vmmconfig.Config) error {
	return fmt.Errorf("abox-vmm requires macOS on Apple Silicon with libkrun")
}
