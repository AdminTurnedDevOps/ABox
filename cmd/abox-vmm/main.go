package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type VMMConfig struct {
	VCPU       uint8  `json:"vcpu"`
	RAMMiB     uint32 `json:"ram_mib"`
	RootDisk   string `json:"root_disk"`
	ConfigDisk string `json:"config_disk"`
	RPCSocket  string `json:"rpc_socket"`
	VsockPort  uint32 `json:"vsock_port"`
	ExecPath   string `json:"exec_path"`
	ConsoleLog string `json:"console_log"`
}

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
	var cfg VMMConfig
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
