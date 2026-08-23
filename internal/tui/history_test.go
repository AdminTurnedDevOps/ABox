package tui

import (
	"testing"

	"github.com/AdminTurnedDevOps/ABox/protocol"
)

func TestLogFromHistory(t *testing.T) {
	got := LogFromHistory([]protocol.HistoryLine{
		{Kind: "user", Text: "hi"},
		{Kind: "tool", Tool: "search", Status: "ok", Text: "no matches"},
		{Kind: "text", Text: "done"},
	})
	want := []string{
		"you: hi",
		"",
		"  ▸ searched the guest repo (no files matched)",
		"done",
	}
	if len(got) != len(want) {
		t.Fatalf("%#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("i=%d got %q want %q", i, got[i], want[i])
		}
	}
}

func TestLogFromHistoryEmpty(t *testing.T) {
	if got := LogFromHistory(nil); got != nil {
		t.Fatalf("%#v", got)
	}
}
