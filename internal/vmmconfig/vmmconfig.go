// Package vmmconfig is the JSON stdin contract from the supervisor to abox-vmm.
package vmmconfig

import "github.com/AdminTurnedDevOps/ABox/protocol"

const (
	DefaultVCPU     = 1
	DefaultRAMMiB   = 768
	DefaultExecPath = "/usr/local/bin/abox-guest"
)

// Config is the validated blob the host supervisor sends to abox-vmm on stdin.
type Config struct {
	VCPU       uint8  `json:"vcpu"`
	RAMMiB     uint32 `json:"ram_mib"`
	RootDisk   string `json:"root_disk"`
	ConfigDisk string `json:"config_disk"`
	RPCSocket  string `json:"rpc_socket"`
	VsockPort  uint32 `json:"vsock_port"`
	ExecPath   string `json:"exec_path"`
	ConsoleLog string `json:"console_log"`
}

func (c *Config) ApplyDefaults() {
	if c.VCPU == 0 {
		c.VCPU = DefaultVCPU
	}
	if c.RAMMiB == 0 {
		c.RAMMiB = DefaultRAMMiB
	}
	if c.VsockPort == 0 {
		c.VsockPort = protocol.RPCPort
	}
	if c.ExecPath == "" {
		c.ExecPath = DefaultExecPath
	}
}
