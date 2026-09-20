//go:build linux

package runtime

import (
	"os"
	"syscall"
)

func helperStopSignal() os.Signal {
	return syscall.SIGTERM
}
