//go:build !cgo || (!darwin && !linux) || (darwin && !arm64) || (linux && !amd64 && !arm64)

package main

import (
	"fmt"

	"github.com/AdminTurnedDevOps/ABox/internal/vmmconfig"
)

func startVM(cfg vmmconfig.Config) error {
	return fmt.Errorf("abox-vmm requires cgo and libkrun on macOS arm64 or Linux amd64/arm64")
}
