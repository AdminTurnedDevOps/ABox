package main

import (
	"bufio"
	"os"
	"testing"
)

func TestPlatformBuildTags(t *testing.T) {
	want := map[string]string{
		"cgoflags_darwin_arm64.go": "//go:build cgo && darwin && arm64",
		"cgoflags_linux.go":        "//go:build cgo && linux && (amd64 || arm64)",
		"start_libkrun.go":         "//go:build cgo && ((darwin && arm64) || (linux && (amd64 || arm64)))",
		"start_stub.go":            "//go:build !cgo || (!darwin && !linux) || (darwin && !arm64) || (linux && !amd64 && !arm64)",
	}
	for path, tag := range want {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(file)
		if !scanner.Scan() {
			file.Close()
			t.Fatalf("read build tag from %s: %v", path, scanner.Err())
		}
		got := scanner.Text()
		file.Close()
		if got != tag {
			t.Errorf("%s build tag = %q, want %q", path, got, tag)
		}
	}
}
