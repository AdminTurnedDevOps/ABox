package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/AdminTurnedDevOps/ABox/internal/provider"
)

func TestBuiltinToolNames(t *testing.T) {
	tools := BuiltinTools()
	if len(tools) != 5 {
		t.Fatalf("want 5 tools, got %d", len(tools))
	}
}

func TestNilSandboxDoesNotRunTools(t *testing.T) {
	l := &Loop{}
	_, err := l.execTool(context.Background(), provider.Event{ToolName: "run_command", ToolArgs: `{"command":"id"}`})
	if err == nil || !strings.Contains(err.Error(), "sandbox unavailable") {
		t.Fatalf("got %v", err)
	}
}
