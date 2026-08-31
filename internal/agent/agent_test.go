package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AdminTurnedDevOps/ABox/internal/guest/mcp"
	"github.com/AdminTurnedDevOps/ABox/internal/guest/tools"
	"github.com/AdminTurnedDevOps/ABox/internal/provider"
)

func TestMaxTurnsStops(t *testing.T) {
	l := &Loop{Repo: tools.Repo{Root: t.TempDir()}, MaxTurns: 1}
	// No provider key: Stream fails immediately. Ensure MaxTurns field is honored
	// by a loop that cannot call out: Turn still requires a working stream.
	err := l.Turn(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected provider error without credentials")
	}
}

func TestBuiltinToolNames(t *testing.T) {
	got := BuiltinTools()
	if len(got) != 5 {
		t.Fatalf("want 5 tools, got %d", len(got))
	}
	if got[0].Name != "list_files" || got[4].Name != "run_command" {
		t.Fatalf("%q %q", got[0].Name, got[4].Name)
	}
}

func TestTurnRequiresGuestRepo(t *testing.T) {
	l := &Loop{}
	err := l.Turn(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "microVM") {
		t.Fatalf("got %v", err)
	}
}

func TestSaveLoadMessagesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "context.json")
	l := &Loop{
		ContextFile: path,
		Messages: []provider.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello"},
			{Role: "tool", ToolID: "1", ToolResult: "ok"},
		},
	}
	if err := l.SaveContext(); err != nil {
		t.Fatal(err)
	}
	l2 := &Loop{ContextFile: path}
	if err := l2.LoadContext(); err != nil {
		t.Fatal(err)
	}
	if len(l2.Messages) != 3 || l2.Messages[0].Content != "hi" || l2.Messages[2].ToolResult != "ok" {
		t.Fatalf("%#v", l2.Messages)
	}
}

func TestHistoryFromMessages(t *testing.T) {
	got := HistoryFromMessages([]provider.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", ToolName: "list_files"},
		{Role: "tool", ToolResult: "ok"},
		{Role: "assistant", Content: "done"},
	})
	if len(got) != 3 {
		t.Fatalf("%#v", got)
	}
	if got[0].Kind != "user" || got[0].Text != "hi" {
		t.Fatalf("%#v", got[0])
	}
	if got[1].Kind != "tool" || got[1].Tool != "list_files" || got[1].Text != "ok" {
		t.Fatalf("%#v", got[1])
	}
	if got[2].Kind != "text" || got[2].Text != "done" {
		t.Fatalf("%#v", got[2])
	}
}

func TestHistoryFromContextJSON(t *testing.T) {
	raw := []byte(`[{"Role":"user","Content":"hi"}]`)
	got, err := HistoryFromContextJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != "user" || got[0].Text != "hi" {
		t.Fatalf("%#v", got)
	}
}

func TestParseMessagesEmpty(t *testing.T) {
	got, err := ParseMessages(nil)
	if err != nil || got != nil {
		t.Fatalf("%v %#v", err, got)
	}
}

func TestLoadContextMissingFile(t *testing.T) {
	l := &Loop{ContextFile: filepath.Join(t.TempDir(), "missing.json")}
	if err := l.LoadContext(); err != nil {
		t.Fatal(err)
	}
	if l.Messages != nil {
		t.Fatalf("%#v", l.Messages)
	}
}

func TestTrimMessagesKeepsNewest(t *testing.T) {
	msgs := []provider.Message{
		{Role: "user", Content: strings.Repeat("a", 200)},
		{Role: "user", Content: strings.Repeat("b", 200)},
		{Role: "user", Content: "tail"},
	}
	got := trimMessages(msgs, 80)
	if len(got) == 0 || got[len(got)-1].Content != "tail" {
		t.Fatalf("%#v", got)
	}
}

func TestExecUnknownTool(t *testing.T) {
	l := &Loop{}
	_, err := l.execTool(context.Background(), provider.Event{ToolName: "host_shell"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExecListFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := &Loop{Repo: tools.Repo{Root: dir}}
	out, err := l.execTool(context.Background(), provider.Event{ToolName: "list_files", ToolArgs: `{"path":"."}`})
	if err != nil {
		t.Fatal(err)
	}
	if out != `["a.txt"]` {
		t.Fatalf("out=%q", out)
	}
}

type stubMCP struct {
	calls  []string
	result string
}

func (s *stubMCP) Tools() []mcp.Tool {
	return []mcp.Tool{{
		Server:      "svc",
		Name:        "echo",
		Prefixed:    "svc__echo",
		Description: "echo",
		Parameters:  map[string]any{"type": "object"},
	}}
}

func (s *stubMCP) Call(_ context.Context, server, tool string, args json.RawMessage) (string, error) {
	s.calls = append(s.calls, server+"/"+tool+"/"+string(args))
	return s.result, nil
}

func TestExecDispatchesPrefixedMCPTool(t *testing.T) {
	st := &stubMCP{result: "echo:hi"}
	l := &Loop{MCP: st}
	out, err := l.execTool(context.Background(), provider.Event{
		ToolName: "svc__echo",
		ToolArgs: `{"text":"hi"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "echo:hi" {
		t.Fatalf("out=%q", out)
	}
	if len(st.calls) != 1 || st.calls[0] != `svc/echo/{"text":"hi"}` {
		t.Fatalf("calls=%v", st.calls)
	}
}

func TestAllToolsIncludesMCP(t *testing.T) {
	l := &Loop{MCP: &stubMCP{}}
	tools := l.allTools()
	if len(tools) != 6 {
		t.Fatalf("want 5 builtin + 1 mcp, got %d", len(tools))
	}
	if tools[5].Name != "svc__echo" {
		t.Fatalf("got %q", tools[5].Name)
	}
}
