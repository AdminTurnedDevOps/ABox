package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/agentapi"
	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

type Event = agentapi.Event
type Message = agentapi.Message
type ToolSchema = agentapi.ToolSchema

// Stream never reads the environment or builds its own client.
func Stream(ctx context.Context, model config.Model, key string, client *http.Client, messages []Message, tools []ToolSchema) (<-chan Event, error) {
	return stream(ctx, model, key, client, messages, tools, false)
}

func StreamWithUsage(ctx context.Context, model config.Model, key string, client *http.Client, messages []Message, tools []ToolSchema) (<-chan Event, error) {
	includeUsage := model.Provider != "xai"
	return stream(ctx, model, key, client, messages, tools, includeUsage)
}

func stream(ctx context.Context, model config.Model, key string, client *http.Client, messages []Message, tools []ToolSchema, includeUsage bool) (<-chan Event, error) {
	if strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("missing credential %s", model.CredentialEnv)
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	if model.Provider == "anthropic" {
		return streamAnthropic(ctx, model, key, client, messages, tools)
	}
	base := strings.TrimRight(model.BaseURL, "/")
	if base == "" {
		switch model.Provider {
		case "xai":
			base = "https://api.x.ai/v1"
		case "openai":
			base = "https://api.openai.com/v1"
		default:
			return nil, fmt.Errorf("unsupported provider %q", model.Provider)
		}
	}
	return streamOpenAICompat(ctx, base, key, model.Model, client, messages, tools, includeUsage)
}

func streamOpenAICompat(ctx context.Context, base, key, model string, client *http.Client, messages []Message, tools []ToolSchema, includeUsage bool) (<-chan Event, error) {
	var oaiMsgs []map[string]any
	for _, m := range messages {
		switch {
		case m.Role == "tool":
			oaiMsgs = append(oaiMsgs, map[string]any{
				"role":         "tool",
				"tool_call_id": m.ToolID,
				"content":      m.ToolResult,
			})
		case m.ToolName != "":
			oaiMsgs = append(oaiMsgs, map[string]any{
				"role": "assistant",
				"tool_calls": []map[string]any{{
					"id":   m.ToolID,
					"type": "function",
					"function": map[string]any{
						"name":      m.ToolName,
						"arguments": m.ToolArgs,
					},
				}},
			})
		default:
			oaiMsgs = append(oaiMsgs, map[string]any{"role": m.Role, "content": m.Content})
		}
	}
	var oaiTools []map[string]any
	for _, t := range tools {
		params := t.Parameters
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		oaiTools = append(oaiTools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  params,
			},
		})
	}
	body := map[string]any{
		"model":    model,
		"messages": oaiMsgs,
		"stream":   true,
	}
	if includeUsage {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	if len(oaiTools) > 0 {
		body["tools"] = oaiTools
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, fmt.Errorf("provider %s: %s", resp.Status, b)
	}
	out := make(chan Event, 32)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		toolID, toolName, toolArgs := "", "", ""
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				break
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content   string `json:"content"`
						ToolCalls []struct {
							ID       string `json:"id"`
							Function struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							} `json:"function"`
						} `json:"tool_calls"`
					} `json:"delta"`
				} `json:"choices"`
				Usage *struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				continue
			}
			if chunk.Usage != nil {
				out <- Event{Type: "usage", Usage: &protocol.UsageInfo{
					InputTokens:  chunk.Usage.PromptTokens,
					OutputTokens: chunk.Usage.CompletionTokens,
				}}
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			d := chunk.Choices[0].Delta
			if d.Content != "" {
				out <- Event{Type: "text", Text: d.Content}
			}
			for _, tc := range d.ToolCalls {
				if tc.ID != "" {
					toolID = tc.ID
				}
				if tc.Function.Name != "" {
					toolName = tc.Function.Name
				}
				toolArgs += tc.Function.Arguments
			}
		}
		if toolName != "" {
			out <- Event{Type: "tool", ToolID: toolID, ToolName: toolName, ToolArgs: toolArgs}
		}
		out <- Event{Type: "done"}
	}()
	return out, nil
}

func streamAnthropic(ctx context.Context, model config.Model, key string, client *http.Client, messages []Message, tools []ToolSchema) (<-chan Event, error) {
	base := strings.TrimRight(model.BaseURL, "/")
	if base == "" {
		base = "https://api.anthropic.com"
	}
	var msgs []map[string]any
	system := ""
	for _, m := range messages {
		if m.Role == "system" {
			system += m.Content
			continue
		}
		if m.Role == "tool" {
			msgs = append(msgs, map[string]any{
				"role": "user",
				"content": []map[string]any{{
					"type":        "tool_result",
					"tool_use_id": m.ToolID,
					"content":     m.ToolResult,
				}},
			})
			continue
		}
		if m.ToolName != "" {
			msgs = append(msgs, map[string]any{
				"role": "assistant",
				"content": []map[string]any{{
					"type":  "tool_use",
					"id":    m.ToolID,
					"name":  m.ToolName,
					"input": json.RawMessage(m.ToolArgs),
				}},
			})
			continue
		}
		msgs = append(msgs, map[string]any{"role": m.Role, "content": m.Content})
	}
	var aTools []map[string]any
	for _, t := range tools {
		params := t.Parameters
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		aTools = append(aTools, map[string]any{
			"name":         t.Name,
			"description":  t.Description,
			"input_schema": params,
		})
	}
	body := map[string]any{
		"model":      model.Model,
		"max_tokens": 4096,
		"messages":   msgs,
		"stream":     true,
	}
	if system != "" {
		body["system"] = system
	}
	if len(aTools) > 0 {
		body["tools"] = aTools
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, fmt.Errorf("provider %s: %s", resp.Status, b)
	}
	out := make(chan Event, 32)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		var toolID, toolName, toolArgs string
		var usage protocol.UsageInfo
		stopReason := ""
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var ev map[string]any
			if json.Unmarshal([]byte(payload), &ev) != nil {
				continue
			}
			switch ev["type"] {
			case "content_block_delta":
				if delta, _ := ev["delta"].(map[string]any); delta != nil {
					if t, _ := delta["text"].(string); t != "" {
						out <- Event{Type: "text", Text: t}
					}
					if p, _ := delta["partial_json"].(string); p != "" {
						toolArgs += p
					}
				}
			case "content_block_start":
				if block, _ := ev["content_block"].(map[string]any); block != nil && block["type"] == "tool_use" {
					toolID, _ = block["id"].(string)
					toolName, _ = block["name"].(string)
				}
			case "message_start":
				if msg, _ := ev["message"].(map[string]any); msg != nil {
					if u, _ := msg["usage"].(map[string]any); u != nil {
						if n, ok := u["input_tokens"].(float64); ok {
							usage.InputTokens = int(n)
						}
					}
				}
			case "message_delta":
				if u, _ := ev["usage"].(map[string]any); u != nil {
					if n, ok := u["output_tokens"].(float64); ok {
						usage.OutputTokens = int(n)
					}
				}
				if delta, _ := ev["delta"].(map[string]any); delta != nil {
					if s, _ := delta["stop_reason"].(string); s != "" {
						stopReason = s
					}
				}
			}
		}
		if usage.InputTokens > 0 || usage.OutputTokens > 0 || stopReason != "" {
			u := usage
			out <- Event{Type: "usage", Usage: &u, StopReason: stopReason}
		}
		if toolName != "" {
			out <- Event{Type: "tool", ToolID: toolID, ToolName: toolName, ToolArgs: toolArgs}
		}
		out <- Event{Type: "done"}
	}()
	return out, nil
}
