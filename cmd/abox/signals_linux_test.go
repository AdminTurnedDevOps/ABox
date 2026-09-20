//go:build linux

package main

import (
	"os"
	"syscall"
	"testing"
)

func TestTerminationSignalsLinux(t *testing.T) {
	got := terminationSignals()
	want := []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
	if len(got) != len(want) {
		t.Fatalf("signals = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("signals = %v, want %v", got, want)
		}
	}
}
