package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/provider"
	"github.com/AdminTurnedDevOps/ABox/internal/runtime"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

type UIEvent struct {
	Kind   string
	Text   string
	Tool   string
	Status string
	Err    string
}

type Loop struct {
	Model    config.Model
	Sandbox  *runtime.Sandbox
	Messages []provider.Message
	OnEvent  func(UIEvent)
}

func BuiltinTools() []provider.ToolSchema {
	obj := func(props map[string]any) map[string]any {
		return map[string]any{"type": "object", "properties": props}
	}
	return []provider.ToolSchema{
		{Name: "list_files", Description: "List paths in the guest repository", Parameters: obj(map[string]any{
			"path": map[string]any{"type": "string"},
		})},
		{Name: "read_file", Description: "Read a file in the guest repository", Parameters: obj(map[string]any{
			"path": map[string]any{"type": "string"},
		})},
		{Name: "search", Description: "Search the guest repository", Parameters: obj(map[string]any{
			"query": map[string]any{"type": "string"},
		})},
		{Name: "apply_patch", Description: "Apply a unified diff in the guest repository", Parameters: obj(map[string]any{
			"patch": map[string]any{"type": "string"},
		})},
		{Name: "run_command", Description: "Run a shell command in the guest", Parameters: obj(map[string]any{
			"command": map[string]any{"type": "string"},
		})},
	}
}

func (l *Loop) emit(e UIEvent) {
	if l.OnEvent != nil {
		l.OnEvent(e)
	}
}

func (l *Loop) Turn(ctx context.Context, user string) error {
	l.Messages = append(l.Messages, provider.Message{Role: "user", Content: user})
	for i := 0; i < 16; i++ {
		events, err := provider.Stream(ctx, l.Model, l.Messages, BuiltinTools())
		if err != nil {
			l.emit(UIEvent{Kind: "error", Err: err.Error()})
			return err
		}
		var text string
		var tool provider.Event
		for ev := range events {
			switch ev.Type {
			case "text":
				text += ev.Text
				l.emit(UIEvent{Kind: "text", Text: ev.Text})
			case "tool":
				tool = ev
			case "error":
				l.emit(UIEvent{Kind: "error", Err: ev.Err.Error()})
				return ev.Err
			}
		}
		if tool.ToolName == "" {
			if text != "" {
				l.Messages = append(l.Messages, provider.Message{Role: "assistant", Content: text})
			}
			l.emit(UIEvent{Kind: "done"})
			return nil
		}
		l.Messages = append(l.Messages, provider.Message{
			Role:     "assistant",
			ToolID:   tool.ToolID,
			ToolName: tool.ToolName,
			ToolArgs: tool.ToolArgs,
		})
		result, err := l.execTool(ctx, tool)
		if err != nil {
			result = "error: " + err.Error()
			l.emit(UIEvent{Kind: "tool", Tool: tool.ToolName, Status: "error", Err: err.Error()})
		} else {
			l.emit(UIEvent{Kind: "tool", Tool: tool.ToolName, Status: "ok", Text: truncate(result, 400)})
		}
		l.Messages = append(l.Messages, provider.Message{
			Role:       "tool",
			ToolID:     tool.ToolID,
			ToolResult: result,
		})
	}
	return fmt.Errorf("turn limit reached")
}

func (l *Loop) execTool(ctx context.Context, ev provider.Event) (string, error) {
	if l.Sandbox == nil {
		return "", fmt.Errorf("sandbox unavailable; model tools never run on the host")
	}
	switch ev.ToolName {
	case "list_files":
		var args struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal([]byte(ev.ToolArgs), &args)
		var res protocol.ListFilesResult
		if err := l.Sandbox.Call(ctx, "list_files", protocol.ListFilesParams{Path: args.Path, Depth: 6, Limit: 500}, &res); err != nil {
			return "", err
		}
		b, _ := json.Marshal(res.Paths)
		return string(b), nil
	case "read_file":
		var args struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal([]byte(ev.ToolArgs), &args)
		var res protocol.ReadFileResult
		if err := l.Sandbox.Call(ctx, "read_file", protocol.ReadFileParams{Path: args.Path, MaxBytes: 64 << 10}, &res); err != nil {
			return "", err
		}
		if res.Binary {
			return "[binary file]", nil
		}
		return res.Content, nil
	case "search":
		var args struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal([]byte(ev.ToolArgs), &args)
		var res protocol.SearchResult
		if err := l.Sandbox.Call(ctx, "search", protocol.SearchParams{Query: args.Query, Limit: 40}, &res); err != nil {
			return "", err
		}
		b, _ := json.Marshal(res.Matches)
		return string(b), nil
	case "apply_patch":
		var args struct {
			Patch string `json:"patch"`
		}
		_ = json.Unmarshal([]byte(ev.ToolArgs), &args)
		var res protocol.ApplyPatchResult
		if err := l.Sandbox.Call(ctx, "apply_patch", protocol.ApplyPatchParams{Patch: args.Patch}, &res); err != nil {
			return "", err
		}
		return res.Output, nil
	case "run_command":
		var args struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal([]byte(ev.ToolArgs), &args)
		ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		var res protocol.RunCommandResult
		if err := l.Sandbox.Call(ctx, "run_command", protocol.RunCommandParams{Command: args.Command, Timeout: 60}, &res); err != nil {
			return "", err
		}
		return fmt.Sprintf("exit=%d\n%s\n%s", res.ExitCode, res.Stdout, res.Stderr), nil
	default:
		return "", fmt.Errorf("unknown tool %q", ev.ToolName)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
