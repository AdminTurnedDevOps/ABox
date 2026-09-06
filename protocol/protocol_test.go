package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

type shortWriter struct {
	buf bytes.Buffer
	n   int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > w.n {
		p = p[:w.n]
	}
	return w.buf.Write(p)
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }

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

func TestWriteFrameUsesFullWrites(t *testing.T) {
	w := &shortWriter{n: 3}
	orig := Frame{V: Version, ID: "short", Method: "list_files"}
	if err := WriteFrame(w, orig); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(&w.buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != orig.ID || got.Method != orig.Method {
		t.Fatalf("got %+v", got)
	}
}

func TestWriteFrameRejectsNoProgressWriter(t *testing.T) {
	if err := WriteFrame(zeroWriter{}, Frame{ID: "1"}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("got %v", err)
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

func TestTrimHistoryRejectsSingleOversizedItem(t *testing.T) {
	const limit = 64
	h := []HistoryLine{{Kind: "text", Text: strings.Repeat("x", 256)}}
	got := TrimHistory(h, limit)
	if len(got) != 0 {
		t.Fatalf("oversized history retained: %#v", got)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > limit {
		t.Fatalf("encoded history is %d bytes, limit %d", len(raw), limit)
	}
}

func TestHistoryLineAliasesAgentEvent(t *testing.T) {
	ev := AgentEvent{Kind: "text", Text: "hi", Tool: "search", Status: "ok", Err: "e"}
	var line HistoryLine = ev
	if line != ev {
		t.Fatalf("got %+v want %+v", line, ev)
	}
}

func TestRejectOversizedFrame(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{0xff, 0xff, 0xff, 0xff})
	if _, err := ReadFrame(&buf); err == nil {
		t.Fatal("expected error")
	}
}

func TestProviderBrokerTypesRoundTrip(t *testing.T) {
	req := ProviderRequest{
		Messages: []ProviderMessage{{
			Role: "assistant", ToolID: "1", ToolName: "list_files", ToolArgs: `{"path":"."}`,
		}},
		Tools: []ProviderToolSchema{{
			Name: "list_files", Description: "list", Parameters: map[string]any{"type": "object"},
		}},
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var got ProviderRequest
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 1 || got.Messages[0].ToolName != "list_files" || got.Messages[0].ToolArgs == "" {
		t.Fatalf("%+v", got.Messages)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "list_files" {
		t.Fatalf("%+v", got.Tools)
	}

	ev := ProviderEventParams{
		StreamID: "s1", Type: "text", Text: "hi",
		Usage: &UsageInfo{InputTokens: 1, OutputTokens: 2}, StopReason: "end_turn",
	}
	rawEv, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var gotEv ProviderEventParams
	if err := json.Unmarshal(rawEv, &gotEv); err != nil {
		t.Fatal(err)
	}
	if gotEv.StreamID != "s1" || gotEv.Usage == nil || gotEv.Usage.OutputTokens != 2 {
		t.Fatalf("%+v", gotEv)
	}

	open := ProviderOpenParams{Model: "grok-default", Rich: true}
	rawOpen, _ := json.Marshal(open)
	var gotOpen ProviderOpenParams
	_ = json.Unmarshal(rawOpen, &gotOpen)
	if gotOpen.Model != "grok-default" || !gotOpen.Rich {
		t.Fatalf("%+v", gotOpen)
	}
}

func TestHelloResultProtocolField(t *testing.T) {
	raw, err := json.Marshal(HelloResult{Accepted: true, Protocol: 3})
	if err != nil {
		t.Fatal(err)
	}
	var got HelloResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Accepted || got.Protocol != 3 {
		t.Fatalf("%+v", got)
	}
	// Old hosts send no protocol field: decodes as 0.
	var legacy HelloResult
	if err := json.Unmarshal([]byte(`{"accepted":true}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Protocol != 0 {
		t.Fatalf("legacy ack: %+v", legacy)
	}
}
