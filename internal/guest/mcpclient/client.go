// Package mcpclient proxies semantic MCP operations through the host broker.
package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AdminTurnedDevOps/ABox/protocol"
)

const cancelTimeout = 2 * time.Second

var (
	ErrHostTooOld = errors.New("host does not support MCP RPC; run make build")
	nextCallID    atomic.Uint64
)

// RPC is the guest broker functionality needed by the MCP proxy.
type RPC interface {
	HostProtocol() int
	Call(context.Context, string, any) (protocol.Frame, error)
}

// Tool is an MCP tool advertised by the host.
type Tool = protocol.MCPTool

type Client struct {
	rpc RPC

	mu    sync.RWMutex
	tools []Tool
	known map[toolRef]struct{}
}

func New(rpc RPC) *Client {
	return &Client{rpc: rpc, known: make(map[toolRef]struct{})}
}

// Refresh fetches and atomically replaces the cached host MCP tool list.
func (c *Client) Refresh(ctx context.Context) error {
	if err := c.requireV4(); err != nil {
		return err
	}
	frame, err := c.rpc.Call(ctx, "mcp_list", protocol.MCPListParams{})
	if err != nil {
		return fmt.Errorf("mcp_list: %w", err)
	}
	if frame.Error != nil {
		return fmt.Errorf("mcp_list: %w", frame.Error)
	}

	var result *protocol.MCPListResult
	if len(frame.Result) == 0 || json.Unmarshal(frame.Result, &result) != nil || result == nil {
		return errors.New("mcp_list: malformed result")
	}
	if len(result.Tools) > protocol.MaxMCPTools {
		return fmt.Errorf("mcp_list: too many tools (maximum %d)", protocol.MaxMCPTools)
	}

	tools := make([]Tool, len(result.Tools))
	known := make(map[toolRef]struct{}, len(result.Tools))
	prefixed := make(map[string]struct{}, len(result.Tools))
	for i, tool := range result.Tools {
		if tool.Server == "" || tool.Name == "" || tool.Prefixed != tool.Server+"__"+tool.Name || tool.Parameters == nil {
			return fmt.Errorf("mcp_list: invalid tool at index %d", i)
		}
		key := toolRef{server: tool.Server, tool: tool.Name}
		if _, exists := known[key]; exists {
			return fmt.Errorf("mcp_list: duplicate tool %q", tool.Prefixed)
		}
		if _, exists := prefixed[tool.Prefixed]; exists {
			return fmt.Errorf("mcp_list: duplicate prefixed tool %q", tool.Prefixed)
		}
		schema, err := json.Marshal(tool.Parameters)
		if err != nil || len(schema) > protocol.MaxMCPSchemaBytes {
			return fmt.Errorf("mcp_list: invalid schema for tool %q", tool.Prefixed)
		}
		var parameters map[string]any
		if json.Unmarshal(schema, &parameters) != nil || parameters == nil {
			return fmt.Errorf("mcp_list: invalid schema for tool %q", tool.Prefixed)
		}
		tool.Parameters = parameters
		tools[i] = tool
		known[key] = struct{}{}
		prefixed[tool.Prefixed] = struct{}{}
	}

	c.mu.Lock()
	c.tools = tools
	c.known = known
	c.mu.Unlock()
	return nil
}

// Tools returns a deep copy of the currently cached tools.
func (c *Client) Tools() []Tool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Tool, len(c.tools))
	for i, tool := range c.tools {
		out[i] = tool
		out[i].Parameters = cloneMap(tool.Parameters)
	}
	return out
}

// Call invokes a tool from the most recently refreshed cache.
func (c *Client) Call(ctx context.Context, server, tool string, args json.RawMessage) (string, error) {
	if err := c.requireV4(); err != nil {
		return "", err
	}
	c.mu.RLock()
	_, ok := c.known[toolRef{server: server, tool: tool}]
	c.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("mcp tool %q on server %q is not available", tool, server)
	}
	if len(args) > protocol.MaxMCPArgsBytes {
		return "", fmt.Errorf("mcp arguments too large (maximum %d bytes)", protocol.MaxMCPArgsBytes)
	}
	if len(args) > 0 && !json.Valid(args) {
		return "", errors.New("mcp arguments are not valid JSON")
	}

	callID := fmt.Sprintf("mcp-%d", nextCallID.Add(1))
	frame, err := c.rpc.Call(ctx, "mcp_call", protocol.MCPCallParams{
		CallID: callID, Server: server, Tool: tool, Arguments: args,
	})
	if err != nil && ctx.Err() != nil {
		go c.cancel(callID)
	}
	if err != nil {
		return "", fmt.Errorf("mcp_call: %w", err)
	}
	if frame.Error != nil {
		return "", fmt.Errorf("mcp_call: %w", frame.Error)
	}

	var result *protocol.MCPCallResult
	if len(frame.Result) == 0 || json.Unmarshal(frame.Result, &result) != nil || result == nil {
		return "", errors.New("mcp_call: malformed result")
	}
	if len(result.Text) > protocol.MaxMCPResultBytes {
		return "", fmt.Errorf("mcp_call: result too large (maximum %d bytes)", protocol.MaxMCPResultBytes)
	}
	if result.IsError {
		if result.Text == "" {
			return "", errors.New("mcp tool returned an error")
		}
		return "", errors.New(result.Text)
	}
	return result.Text, nil
}

func (c *Client) requireV4() error {
	if c.rpc == nil {
		return errors.New("mcp broker is not configured")
	}
	if got := c.rpc.HostProtocol(); got < 4 {
		return fmt.Errorf("%w (host speaks protocol %d)", ErrHostTooOld, got)
	}
	return nil
}

func (c *Client) cancel(callID string) {
	ctx, cancel := context.WithTimeout(context.Background(), cancelTimeout)
	defer cancel()
	_, _ = c.rpc.Call(ctx, "mcp_cancel", protocol.MCPCancelParams{CallID: callID})
}

type toolRef struct {
	server string
	tool   string
}

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = cloneValue(value)
	}
	return out
}

func cloneValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneMap(value)
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = cloneValue(item)
		}
		return out
	default:
		return value
	}
}
