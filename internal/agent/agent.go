package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/guest/tools"
	"github.com/AdminTurnedDevOps/ABox/internal/provider"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

type Loop struct {
	Model    config.Model
	Repo     tools.Repo
	Messages []provider.Message
	OnEvent  func(protocol.AgentEvent)
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

func (l *Loop) emit(e protocol.AgentEvent) {
	if l.OnEvent != nil {
		l.OnEvent(e)
	}
}

func (l *Loop) Turn(ctx context.Context, user string) error {
	if l.Repo.Root == "" {
		err := fmt.Errorf("agent runs only inside the microVM")
		l.emit(protocol.AgentEvent{Kind: "error", Err: err.Error()})
		return err
	}
	l.Messages = append(l.Messages, provider.Message{Role: "user", Content: user})
	for i := 0; i < 16; i++ {
		events, err := provider.Stream(ctx, l.Model, l.Messages, BuiltinTools())
		if err != nil {
			l.emit(protocol.AgentEvent{Kind: "error", Err: err.Error()})
			return err
		}
		var text string
		var tool provider.Event
		for ev := range events {
			switch ev.Type {
			case "text":
				text += ev.Text
				l.emit(protocol.AgentEvent{Kind: "text", Text: ev.Text})
			case "tool":
				tool = ev
			case "error":
				l.emit(protocol.AgentEvent{Kind: "error", Err: ev.Err.Error()})
				return ev.Err
			}
		}
		if tool.ToolName == "" {
			if text != "" {
				l.Messages = append(l.Messages, provider.Message{Role: "assistant", Content: text})
			}
			l.emit(protocol.AgentEvent{Kind: "done"})
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
			l.emit(protocol.AgentEvent{Kind: "tool", Tool: tool.ToolName, Status: "error", Err: err.Error()})
		} else {
			l.emit(protocol.AgentEvent{Kind: "tool", Tool: tool.ToolName, Status: "ok", Text: truncate(result, 400)})
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
	switch ev.ToolName {
	case "list_files":
		var args struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal([]byte(ev.ToolArgs), &args)
		paths, err := l.Repo.List(args.Path, 6, 500)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(paths)
		return string(b), nil
	case "read_file":
		var args struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal([]byte(ev.ToolArgs), &args)
		content, bin, _, err := l.Repo.Read(args.Path, 64<<10)
		if err != nil {
			return "", err
		}
		if bin {
			return "[binary file]", nil
		}
		return content, nil
	case "search":
		var args struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal([]byte(ev.ToolArgs), &args)
		matches, err := l.Repo.Search(args.Query, ".", 40)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(matches)
		return string(b), nil
	case "apply_patch":
		var args struct {
			Patch string `json:"patch"`
		}
		_ = json.Unmarshal([]byte(ev.ToolArgs), &args)
		return l.Repo.ApplyPatch(args.Patch)
	case "run_command":
		var args struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal([]byte(ev.ToolArgs), &args)
		timeout := 60 * time.Second
		if deadline, ok := ctx.Deadline(); ok {
			timeout = time.Until(deadline)
		}
		exit, stdout, stderr, _, _, err := l.Repo.Run(args.Command, "", timeout, tools.DefaultMaxOutput)
		if err != nil && exit == -1 {
			return "", err
		}
		return fmt.Sprintf("exit=%d\n%s\n%s", exit, stdout, stderr), nil
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

func ModelFromGuest(m protocol.GuestModel) config.Model {
	return config.Model{
		Name:          m.Name,
		Provider:      m.Provider,
		Model:         m.Model,
		CredentialEnv: m.CredentialEnv,
		BaseURL:       m.BaseURL,
	}
}
