package vmmconfig

import (
	"encoding/json"
	"testing"

	"github.com/AdminTurnedDevOps/ABox/protocol"
)

func TestApplyDefaults(t *testing.T) {
	var cfg Config
	cfg.ApplyDefaults()
	if cfg.VCPU != DefaultVCPU || cfg.RAMMiB != DefaultRAMMiB || cfg.ExecPath != DefaultExecPath || cfg.VsockPort != protocol.RPCPort {
		t.Fatalf("%+v", cfg)
	}
}

func TestConfigJSONRoundTrip(t *testing.T) {
	orig := Config{
		VCPU:       2,
		RAMMiB:     1024,
		RootDisk:   "/tmp/root.raw",
		ConfigDisk: "/tmp/config.raw",
		RPCSocket:  "/tmp/rpc.sock",
		VsockPort:  1024,
		ExecPath:   "/usr/local/bin/abox-guest",
		ConsoleLog: "/tmp/console.log",
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]any
	if err := json.Unmarshal(data, &keys); err != nil {
		t.Fatal(err)
	}
	want := []string{"vcpu", "ram_mib", "root_disk", "config_disk", "rpc_socket", "vsock_port", "exec_path", "console_log"}
	if len(keys) != len(want) {
		t.Fatalf("keys %v", keys)
	}
	for _, k := range want {
		if _, ok := keys[k]; !ok {
			t.Fatalf("missing json key %q in %s", k, data)
		}
	}
	var got Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got != orig {
		t.Fatalf("got %+v want %+v", got, orig)
	}
}
