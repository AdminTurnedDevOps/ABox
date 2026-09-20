//go:build !linux

package runtime

import "os"

func helperStopSignal() os.Signal {
	return os.Interrupt
}
