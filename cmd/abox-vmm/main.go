// Reads configuration and calls libkrun to create/start the VM

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
	liveness := os.NewFile(3, "supervisor-liveness")
	if liveness == nil {
		return fmt.Errorf("liveness fd 3 is required")
	}
	if _, err := liveness.Stat(); err != nil {
		return fmt.Errorf("liveness fd 3: %w", err)
	}
	defer liveness.Close()
	go watchLiveness(liveness, os.Exit)

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var cfg vmmconfig.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	cfg.ApplyDefaults()
	if cfg.RootDisk == "" || cfg.RPCSocket == "" {
		return fmt.Errorf("root_disk and rpc_socket are required")
	}
	return startVM(cfg)
}

func watchLiveness(r io.Reader, exit func(int)) {
	_, err := io.Copy(io.Discard, r)
	if err != nil {
		exit(1)
		return
	}
	exit(0)
}
