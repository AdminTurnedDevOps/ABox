//go:build !darwin || !arm64

package main

import (
	"fmt"

	"github.com/AdminTurnedDevOps/ABox/internal/vmmconfig"
)

func startVM(cfg vmmconfig.Config) error {
	return fmt.Errorf("abox-vmm requires macOS on Apple Silicon with libkrun")
}
