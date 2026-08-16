package protocol

import (
	"bytes"
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

func TestRejectOversizedFrame(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{0xff, 0xff, 0xff, 0xff})
	if _, err := ReadFrame(&buf); err == nil {
		t.Fatal("expected error")
	}
}
