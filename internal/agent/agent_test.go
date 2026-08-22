package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/AdminTurnedDevOps/ABox/internal/guest/mcp"
	"github.com/AdminTurnedDevOps/ABox/internal/provider"
)

func TestBuiltinToolNames(t *testing.T) {
	tools := BuiltinTools()
	if len(tools) != 5 {
		t.Fatalf("want 5 tools, got %d", len(tools))
	}
}

func TestTurnRequiresGuestRepo(t *testing.T) {
	l := &Loop{}
	err := l.Turn(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "microVM") {
		t.Fatalf("got %v", err)
	}
}

func TestExecUnknownTool(t *testing.T) {
	l := &Loop{}
	_, err := l.execTool(context.Background(), provider.Event{ToolName: "host_shell"})
	if err == nil {
		t.Fatal("expected error")
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
