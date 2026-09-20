//go:build cgo && darwin && arm64

package main

/*
#cgo CFLAGS: -I/opt/homebrew/include
#cgo LDFLAGS: -L/opt/homebrew/lib -lkrun -lkrunfw -Wl,-rpath,/opt/homebrew/lib
*/
import "C"
