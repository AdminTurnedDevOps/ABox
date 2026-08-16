//go:build !linux

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "abox-guest is the Linux guest worker; build with GOOS=linux GOARCH=arm64")
	os.Exit(2)
}
