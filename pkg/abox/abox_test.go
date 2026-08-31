package abox

import (
	"errors"
	"testing"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/runtime"
)

func TestErrGuestTooOldIsRuntime(t *testing.T) {
	if !errors.Is(ErrGuestTooOld, runtime.ErrGuestTooOld) {
		t.Fatal("SDK error must wrap runtime.ErrGuestTooOld")
	}
}

func TestOptionsDefaults(t *testing.T) {
	o := Options{}.withDefaults()
	if o.BootTimeout != 45*time.Second {
		t.Fatalf("boot timeout %s", o.BootTimeout)
	}
}
