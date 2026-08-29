//go:build !linux

package tools

import "fmt"

func Freeze() error { return fmt.Errorf("FIFREEZE only available on linux") }
func Thaw() error   { return fmt.Errorf("FITHAW only available on linux") }
