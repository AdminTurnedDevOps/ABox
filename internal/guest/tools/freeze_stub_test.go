//go:build !abox_guest

package tools

import (
	"strings"
	"testing"
)

func TestHostBuildCannotFreezeFilesystem(t *testing.T) {
	if err := Freeze(); err == nil || !strings.Contains(err.Error(), "tagged Linux guest build") {
		t.Fatalf("Freeze returned %v; untagged builds must use the non-freezing stub", err)
	}
	if err := Thaw(); err == nil || !strings.Contains(err.Error(), "tagged Linux guest build") {
		t.Fatalf("Thaw returned %v; untagged builds must use the non-freezing stub", err)
	}
}
