package main

import (
	"os"
	"testing"
	"time"
)

func TestWatchLivenessExitsOnEOF(t *testing.T) {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readEnd.Close()
	exited := make(chan int, 1)
	go watchLiveness(readEnd, func(code int) { exited <- code })
	if err := writeEnd.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-exited:
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
	case <-time.After(time.Second):
		t.Fatal("watchLiveness did not exit on EOF")
	}
}
