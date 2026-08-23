package protocol

import (
	"bytes"
	"strings"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	orig := Frame{V: Version, ID: "1", Method: "list_files"}
	if err := WriteFrame(&buf, orig); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "1" || got.Method != "list_files" {
		t.Fatalf("got %+v", got)
	}
}

func TestTrimHistoryKeepsNewest(t *testing.T) {
	h := []HistoryLine{
		{Kind: "user", Text: strings.Repeat("a", 200)},
		{Kind: "text", Text: "tail"},
	}
	got := TrimHistory(h, 40)
	if len(got) == 0 || got[len(got)-1].Text != "tail" {
		t.Fatalf("%#v", got)
	}
}

func TestRejectOversizedFrame(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{0xff, 0xff, 0xff, 0xff})
	if _, err := ReadFrame(&buf); err == nil {
		t.Fatal("expected error")
	}
}
