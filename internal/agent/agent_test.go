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
