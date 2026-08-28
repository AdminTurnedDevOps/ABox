// Package vmmconfig is the JSON stdin contract from the supervisor to abox-vmm.
package vmmconfig

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
