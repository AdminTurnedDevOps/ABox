package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/AdminTurnedDevOps/ABox/internal/vmmconfig"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "abox-vmm: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var cfg vmmconfig.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	if cfg.VCPU == 0 {
		cfg.VCPU = 1
	}
	if cfg.RAMMiB == 0 {
		cfg.RAMMiB = 768
	}
	if cfg.VsockPort == 0 {
		cfg.VsockPort = 1024
	}
	if cfg.ExecPath == "" {
		cfg.ExecPath = "/usr/local/bin/abox-guest"
	}
	if cfg.RootDisk == "" || cfg.RPCSocket == "" {
		return fmt.Errorf("root_disk and rpc_socket are required")
	}
	return startVM(cfg)
}
