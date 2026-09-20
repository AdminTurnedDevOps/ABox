//go:build cgo && linux && (amd64 || arm64)

package main

/*
#cgo pkg-config: libkrun
*/
import "C"
