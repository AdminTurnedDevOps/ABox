package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/guest/mcp"
	"github.com/AdminTurnedDevOps/ABox/internal/guest/tools"
	"github.com/AdminTurnedDevOps/ABox/internal/provider"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

type MCPClient interface {
	Tools() []mcp.Tool
	Call(ctx context.Context, server, tool string, args json.RawMessage) (string, error)
}

const (
	DefaultContextFile = "/var/lib/abox/context.json"
	maxContextBytes    = 2 << 20
)

type Loop struct {
	Model       config.Model
	Repo        tools.Repo
	MCP         MCPClient
	Messages    []provider.Message
	ContextFile string
	OnEvent     func(protocol.AgentEvent)
}

func BuiltinTools() []provider.ToolSchema {
	specs := tools.BuiltinSpecs()
	out := make([]provider.ToolSchema, len(specs))
	for i, s := range specs {
		out[i] = provider.ToolSchema{Name: s.Name, Description: s.Description, Parameters: s.Parameters}
	}
	return out
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
		events, err := provider.Stream(ctx, l.Model, l.Messages, l.allTools())
		if err != nil {
			l.emit(protocol.AgentEvent{Kind: "error", Err: err.Error()})
			_ = l.SaveContext()
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
				_ = l.SaveContext()
				return ev.Err
			}
		}
		if tool.ToolName == "" {
			if text != "" {
				l.Messages = append(l.Messages, provider.Message{Role: "assistant", Content: text})
			}
			l.emit(protocol.AgentEvent{Kind: "done"})
			_ = l.SaveContext()
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
	_ = l.SaveContext()
	return fmt.Errorf("turn limit reached")
}

func (l *Loop) contextPath() string {
	if l.ContextFile != "" {
		return l.ContextFile
	}
	return DefaultContextFile
}

func (l *Loop) SaveContext() error {
	return SaveMessages(l.contextPath(), l.Messages)
}

func (l *Loop) LoadContext() error {
	msgs, err := LoadMessages(l.contextPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	l.Messages = msgs
	return nil
}

func (l *Loop) History() []protocol.HistoryLine {
	return protocol.TrimHistory(HistoryFromMessages(l.Messages), protocol.MaxHistoryBytes)
}

func HistoryFromMessages(msgs []provider.Message) []protocol.HistoryLine {
	var out []protocol.HistoryLine
	for _, m := range msgs {
		switch m.Role {
		case "user":
			if m.Content != "" {
				out = append(out, protocol.HistoryLine{Kind: "user", Text: m.Content})
			}
		case "assistant":
			if m.ToolName != "" {
				out = append(out, protocol.HistoryLine{Kind: "tool", Tool: m.ToolName, Status: "ok"})
			}
			if m.Content != "" {
				out = append(out, protocol.HistoryLine{Kind: "text", Text: m.Content})
			}
		case "tool":
			if n := len(out); n > 0 && out[n-1].Kind == "tool" && out[n-1].Text == "" {
				out[n-1].Text = truncate(m.ToolResult, 400)
			}
		}
	}
	return out
}

func HistoryFromContextJSON(data []byte) ([]protocol.HistoryLine, error) {
	msgs, err := ParseMessages(data)
	if err != nil {
		return nil, err
	}
	return HistoryFromMessages(msgs), nil
}

func SaveMessages(path string, msgs []provider.Message) error {
	if path == "" {
		return fmt.Errorf("empty context path")
	}
	trimmed := trimMessages(msgs, maxContextBytes)
	data, err := json.MarshalIndent(trimmed, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func LoadMessages(path string) ([]provider.Message, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseMessages(data)
}

func ParseMessages(data []byte) ([]provider.Message, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	var msgs []provider.Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}

func trimMessages(msgs []provider.Message, max int) []provider.Message {
	if max <= 0 {
		return nil
	}
	var out []provider.Message
	var size int
	for i := len(msgs) - 1; i >= 0; i-- {
		b, err := json.Marshal(msgs[i])
		if err != nil {
			continue
		}
		if size+len(b) > max && len(out) > 0 {
			break
		}
		out = append(out, msgs[i])
		size += len(b)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func (l *Loop) allTools() []provider.ToolSchema {
	tools := BuiltinTools()
	if l.MCP == nil {
		return tools
	}
	for _, t := range l.MCP.Tools() {
		name := t.Prefixed
		if name == "" {
			name = t.Server + "__" + t.Name
		}
		desc := t.Description
		if t.Server != "" {
			desc = "[" + t.Server + "] " + desc
		}
		tools = append(tools, provider.ToolSchema{
			Name:        name,
			Description: desc,
			Parameters:  t.Parameters,
		})
	}
	return tools
}

func splitMCPName(name string) (server, tool string, ok bool) {
	server, tool, found := strings.Cut(name, "__")
	if !found || server == "" || tool == "" {
		return "", "", false
	}
	return server, tool, true
}

func (l *Loop) execTool(ctx context.Context, ev provider.Event) (string, error) {
	if server, tool, ok := splitMCPName(ev.ToolName); ok {
		if l.MCP == nil {
			return "", fmt.Errorf("mcp is not configured")
		}
		return l.MCP.Call(ctx, server, tool, json.RawMessage(ev.ToolArgs))
	}
	timeout := 60 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	}
	result, err := l.Repo.CallTool(ev.ToolName, json.RawMessage(ev.ToolArgs), timeout)
	return tools.FormatToolResult(result, err)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
