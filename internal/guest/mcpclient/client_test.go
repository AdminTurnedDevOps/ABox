package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AdminTurnedDevOps/ABox/protocol"
)

type rpcCall struct {
	ctx    context.Context
	method string
	params any
}

type stubRPC struct {
	protocol int

	mu    sync.Mutex
	calls []rpcCall
	call  func(context.Context, string, any) (protocol.Frame, error)
}

func (s *stubRPC) HostProtocol() int { return s.protocol }

func (s *stubRPC) Call(ctx context.Context, method string, params any) (protocol.Frame, error) {
	s.mu.Lock()
	s.calls = append(s.calls, rpcCall{ctx: ctx, method: method, params: params})
	fn := s.call
	s.mu.Unlock()
	if fn == nil {
		return protocol.Frame{}, nil
	}
	return fn(ctx, method, params)
}

func resultFrame(t *testing.T, value any) protocol.Frame {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.Frame{Result: raw}
}

func listedTool() protocol.MCPTool {
	return protocol.MCPTool{
		Server: "svc", Name: "echo", Prefixed: "svc__echo", Description: "echo text",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"text": map[string]any{"type": "string"}},
		},
	}
}

func TestRefreshCachesValidatedCopies(t *testing.T) {
	rpc := &stubRPC{protocol: 4}
	rpc.call = func(_ context.Context, method string, _ any) (protocol.Frame, error) {
		if method != "mcp_list" {
			t.Fatalf("method = %q", method)
		}
		return resultFrame(t, protocol.MCPListResult{Tools: []protocol.MCPTool{listedTool()}}), nil
	}
	c := New(rpc)
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	got := c.Tools()
	if len(got) != 1 || got[0].Prefixed != "svc__echo" {
		t.Fatalf("tools = %#v", got)
	}
	got[0].Name = "changed"
	got[0].Parameters["type"] = "changed"
	got[0].Parameters["properties"].(map[string]any)["text"] = nil
	again := c.Tools()
	if again[0].Name != "echo" || again[0].Parameters["type"] != "object" {
		t.Fatalf("cached tool mutated: %#v", again[0])
	}
	properties := again[0].Parameters["properties"].(map[string]any)
	if properties["text"] == nil {
		t.Fatal("nested cached schema mutated")
	}
}

func TestRefreshFailureKeepsPreviousCache(t *testing.T) {
	rpc := &stubRPC{protocol: 4}
	result := protocol.MCPListResult{Tools: []protocol.MCPTool{listedTool()}}
	rpc.call = func(context.Context, string, any) (protocol.Frame, error) {
		return resultFrame(t, result), nil
	}
	c := New(rpc)
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	bad := listedTool()
	bad.Prefixed = "wrong"
	result = protocol.MCPListResult{Tools: []protocol.MCPTool{bad}}
	if err := c.Refresh(context.Background()); err == nil {
		t.Fatal("expected invalid refresh error")
	}
	if got := c.Tools(); len(got) != 1 || got[0].Prefixed != "svc__echo" {
		t.Fatalf("cache replaced after failed refresh: %#v", got)
	}
}

func TestRefreshRejectsTooManyTools(t *testing.T) {
	rpc := &stubRPC{protocol: 4}
	rpc.call = func(context.Context, string, any) (protocol.Frame, error) {
		return resultFrame(t, protocol.MCPListResult{Tools: make([]protocol.MCPTool, protocol.MaxMCPTools+1)}), nil
	}
	if err := New(rpc).Refresh(context.Background()); err == nil || !strings.Contains(err.Error(), "too many tools") {
		t.Fatalf("error = %v", err)
	}
}

func TestRefreshRejectsOversizedSchema(t *testing.T) {
	rpc := &stubRPC{protocol: 4}
	rpc.call = func(context.Context, string, any) (protocol.Frame, error) {
		tool := listedTool()
		tool.Parameters = map[string]any{"description": strings.Repeat("x", protocol.MaxMCPSchemaBytes)}
		return resultFrame(t, protocol.MCPListResult{Tools: []protocol.MCPTool{tool}}), nil
	}
	if err := New(rpc).Refresh(context.Background()); err == nil || !strings.Contains(err.Error(), "invalid schema") {
		t.Fatalf("error = %v", err)
	}
}

func TestRequiresProtocolFour(t *testing.T) {
	rpc := &stubRPC{protocol: 3}
	c := New(rpc)
	if err := c.Refresh(context.Background()); !errors.Is(err, ErrHostTooOld) {
		t.Fatalf("refresh error = %v", err)
	}
	if _, err := c.Call(context.Background(), "svc", "echo", nil); !errors.Is(err, ErrHostTooOld) {
		t.Fatalf("call error = %v", err)
	}
	if len(rpc.calls) != 0 {
		t.Fatalf("unexpected RPCs: %#v", rpc.calls)
	}
}

func TestCallValidatesCacheAndArguments(t *testing.T) {
	rpc := &stubRPC{protocol: 4}
	rpc.call = func(_ context.Context, method string, _ any) (protocol.Frame, error) {
		if method == "mcp_list" {
			return resultFrame(t, protocol.MCPListResult{Tools: []protocol.MCPTool{listedTool()}}), nil
		}
		return resultFrame(t, protocol.MCPCallResult{Text: "ok"}), nil
	}
	c := New(rpc)
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(context.Background(), "svc", "missing", nil); err == nil {
		t.Fatal("expected uncached tool error")
	}
	if _, err := c.Call(context.Background(), "svc", "echo", json.RawMessage(`{`)); err == nil {
		t.Fatal("expected invalid JSON error")
	}
	if _, err := c.Call(context.Background(), "svc", "echo", make(json.RawMessage, protocol.MaxMCPArgsBytes+1)); err == nil {
		t.Fatal("expected oversized arguments error")
	}
	if len(rpc.calls) != 1 {
		t.Fatalf("invalid calls reached host: %d RPCs", len(rpc.calls))
	}
}

func TestCallUsesUniqueIDsAndReturnsResult(t *testing.T) {
	rpc := &stubRPC{protocol: 4}
	var ids []string
	rpc.call = func(_ context.Context, method string, params any) (protocol.Frame, error) {
		if method == "mcp_list" {
			return resultFrame(t, protocol.MCPListResult{Tools: []protocol.MCPTool{listedTool()}}), nil
		}
		p, ok := params.(protocol.MCPCallParams)
		if !ok {
			t.Fatalf("params type = %T", params)
		}
		ids = append(ids, p.CallID)
		if p.Server != "svc" || p.Tool != "echo" || string(p.Arguments) != `{"text":"hi"}` {
			t.Fatalf("params = %#v", p)
		}
		return resultFrame(t, protocol.MCPCallResult{Text: "echo:hi"}), nil
	}
	c := New(rpc)
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got, err := c.Call(context.Background(), "svc", "echo", json.RawMessage(`{"text":"hi"}`))
		if err != nil || got != "echo:hi" {
			t.Fatalf("got %q, error %v", got, err)
		}
	}
	if len(ids) != 2 || ids[0] == "" || ids[0] == ids[1] {
		t.Fatalf("call IDs = %v", ids)
	}
}

func TestCallTurnsToolErrorIntoError(t *testing.T) {
	rpc := &stubRPC{protocol: 4}
	rpc.call = func(_ context.Context, method string, _ any) (protocol.Frame, error) {
		if method == "mcp_list" {
			return resultFrame(t, protocol.MCPListResult{Tools: []protocol.MCPTool{listedTool()}}), nil
		}
		return resultFrame(t, protocol.MCPCallResult{Text: "tool failed", IsError: true}), nil
	}
	c := New(rpc)
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(context.Background(), "svc", "echo", nil); err == nil || err.Error() != "tool failed" {
		t.Fatalf("error = %v", err)
	}
}

func TestCanceledCallSendsCancelWithIndependentContext(t *testing.T) {
	canceled := make(chan protocol.MCPCancelParams, 1)
	rpc := &stubRPC{protocol: 4}
	rpc.call = func(ctx context.Context, method string, params any) (protocol.Frame, error) {
		switch method {
		case "mcp_list":
			return resultFrame(t, protocol.MCPListResult{Tools: []protocol.MCPTool{listedTool()}}), nil
		case "mcp_call":
			<-ctx.Done()
			return protocol.Frame{}, ctx.Err()
		case "mcp_cancel":
			if ctx.Err() != nil {
				t.Fatal("cancel RPC reused canceled context")
			}
			p := params.(protocol.MCPCancelParams)
			canceled <- p
			return resultFrame(t, map[string]bool{"ok": true}), nil
		default:
			t.Fatalf("method = %q", method)
			return protocol.Frame{}, nil
		}
	}
	c := New(rpc)
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Call(ctx, "svc", "echo", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	select {
	case p := <-canceled:
		if p.CallID == "" {
			t.Fatal("empty canceled call ID")
		}
	case <-time.After(time.Second):
		t.Fatal("mcp_cancel was not sent")
	}
}
