//go:build !darwin || !arm64

package main

import "fmt"

func startVM(cfg VMMConfig) error {
	return fmt.Errorf("abox-vmm requires macOS on Apple Silicon with libkrun")
}
