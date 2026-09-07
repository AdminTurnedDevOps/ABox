package mcpbroker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/credsource"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

type staticSource struct {
	value string
	err   error
}

func (s staticSource) Resolve(context.Context, credsource.Reference) (credsource.Value, error) {
	return credsource.Value{Bytes: []byte(s.value)}, s.err
}

func (staticSource) Close() error { return nil }

func raw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func testServer(t *testing.T, configure func(*sdkmcp.Server)) *httptest.Server {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "1"}, nil)
	configure(server)
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, &sdkmcp.StreamableHTTPOptions{Stateless: true})
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	return httpServer
}

func addTextTool(server *sdkmcp.Server, name string, handler sdkmcp.ToolHandler) {
	server.AddTool(&sdkmcp.Tool{
		Name:        name,
		Description: name + " description",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}},
	}, handler)
}

func echoHandler(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	var args struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return nil, err
	}
	return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "echo:" + args.Text}}}, nil
}

func list(t *testing.T, broker *Broker) protocol.MCPListResult {
	t.Helper()
	result, perr := broker.Handle(context.Background(), "mcp_list", raw(t, protocol.MCPListParams{}))
	if perr != nil {
		t.Fatalf("mcp_list: %v", perr)
	}
	return result.(protocol.MCPListResult)
}

func TestDiscoveryAndCallUseExactConfiguredID(t *testing.T) {
	server := testServer(t, func(server *sdkmcp.Server) {
		addTextTool(server, "echo", echoHandler)
	})
	broker := New([]config.MCPServer{{Name: "svc", URL: server.URL}}, nil).WithHTTPClient(server.Client())
	t.Cleanup(func() { _ = broker.Close() })

	listed := list(t, broker)
	if len(listed.Tools) != 1 || listed.Tools[0].Server != "svc" || listed.Tools[0].Prefixed != "svc__echo" {
		t.Fatalf("tools = %#v", listed.Tools)
	}
	result, perr := broker.Handle(context.Background(), "mcp_call", raw(t, protocol.MCPCallParams{
		CallID: "c1", Server: "svc", Tool: "echo", Arguments: json.RawMessage(`{"text":"hi"}`),
	}))
	if perr != nil {
		t.Fatalf("mcp_call: %v", perr)
	}
	if got := result.(protocol.MCPCallResult); got.Text != "echo:hi" || got.IsError || got.Truncated {
		t.Fatalf("result = %#v", got)
	}
	_, perr = broker.Handle(context.Background(), "mcp_call", raw(t, protocol.MCPCallParams{CallID: "c2", Server: "SVC", Tool: "echo"}))
	if perr == nil {
		t.Fatal("case-changed server ID was accepted")
	}
}

func TestCallRechecksConfiguredAndDiscoveredAllowlist(t *testing.T) {
	var hiddenCalls atomic.Int32
	server := testServer(t, func(server *sdkmcp.Server) {
		addTextTool(server, "echo", echoHandler)
		addTextTool(server, "hidden", func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			hiddenCalls.Add(1)
			return &sdkmcp.CallToolResult{}, nil
		})
	})
	broker := New([]config.MCPServer{{Name: "svc", URL: server.URL, ToolAllowlist: []string{"echo"}}}, nil).WithHTTPClient(server.Client())
	t.Cleanup(func() { _ = broker.Close() })
	if got := list(t, broker).Tools; len(got) != 1 || got[0].Name != "echo" {
		t.Fatalf("tools = %#v", got)
	}

	_, perr := broker.Handle(context.Background(), "mcp_call", raw(t, protocol.MCPCallParams{CallID: "bypass", Server: "svc", Tool: "hidden"}))
	if perr == nil || !strings.Contains(perr.Message, "not allowed") {
		t.Fatalf("allowlist bypass error = %#v", perr)
	}
	if hiddenCalls.Load() != 0 {
		t.Fatal("hidden tool reached the MCP server")
	}
}

func TestBearerResolvedOnHostAndMissingCredentialAllowed(t *testing.T) {
	var auth atomic.Value
	inner := testServer(t, func(server *sdkmcp.Server) { addTextTool(server, "echo", echoHandler) })
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		auth.Store(req.Header.Get("Authorization"))
		inner.Config.Handler.ServeHTTP(w, req)
	}))
	t.Cleanup(front.Close)

	resolver := credsource.NewResolver()
	resolver.Register("test", staticSource{value: "secret-token"})
	broker := New([]config.MCPServer{{
		Name: "svc", URL: front.URL, Credential: &config.CredentialRef{Source: "test", Name: "token"},
	}}, resolver).WithHTTPClient(front.Client())
	t.Cleanup(func() { _ = broker.Close() })
	list(t, broker)
	if got, _ := auth.Load().(string); got != "Bearer secret-token" {
		t.Fatalf("authorization = %q", got)
	}

	missing := credsource.NewResolver()
	missing.Register("test", staticSource{err: credsource.ErrNotFound})
	anonymous := New([]config.MCPServer{{
		Name: "anon", URL: inner.URL, Credential: &config.CredentialRef{Source: "test", Name: "missing"},
	}}, missing).WithHTTPClient(inner.Client())
	t.Cleanup(func() { _ = anonymous.Close() })
	if got := list(t, anonymous).Tools; len(got) != 1 {
		t.Fatalf("anonymous tools = %#v", got)
	}
}

func TestCrossOriginRedirectRejectedWithoutLeakingBearer(t *testing.T) {
	var targetRequests atomic.Int32
	var targetAuth atomic.Value
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		targetRequests.Add(1)
		targetAuth.Store(req.Header.Get("Authorization"))
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	t.Cleanup(target.Close)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)

	resolver := credsource.NewResolver()
	resolver.Register("test", staticSource{value: "do-not-leak"})
	broker := New([]config.MCPServer{{
		Name: "svc", URL: redirect.URL, Credential: &config.CredentialRef{Source: "test", Name: "token"},
	}}, resolver).WithHTTPClient(redirect.Client())
	t.Cleanup(func() { _ = broker.Close() })
	_, perr := broker.Handle(context.Background(), "mcp_list", raw(t, protocol.MCPListParams{}))
	if perr == nil || !strings.Contains(perr.Message, "crossed the configured origin") {
		t.Fatalf("redirect error = %#v", perr)
	}
	if targetRequests.Load() != 0 {
		got, _ := targetAuth.Load().(string)
		t.Fatalf("redirect target received %d requests, auth %q", targetRequests.Load(), got)
	}
}

func TestProtocolBounds(t *testing.T) {
	server := testServer(t, func(server *sdkmcp.Server) {
		addTextTool(server, "large-result", func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: strings.Repeat("x", protocol.MaxMCPResultBytes+100)}}}, nil
		})
		server.AddTool(&sdkmcp.Tool{
			Name: "large-schema",
			InputSchema: map[string]any{
				"type": "object", "description": strings.Repeat("s", protocol.MaxMCPSchemaBytes),
			},
		}, echoHandler)
		for i := 0; i < protocol.MaxMCPTools+5; i++ {
			addTextTool(server, fmt.Sprintf("tool-%03d", i), echoHandler)
		}
	})
	broker := New([]config.MCPServer{{Name: "svc", URL: server.URL}}, nil).WithHTTPClient(server.Client())
	t.Cleanup(func() { _ = broker.Close() })
	listed := list(t, broker)
	if len(listed.Tools) != protocol.MaxMCPTools {
		t.Fatalf("tool count = %d, want %d", len(listed.Tools), protocol.MaxMCPTools)
	}
	for _, tool := range listed.Tools {
		if tool.Name == "large-schema" {
			t.Fatal("oversized schema was exposed")
		}
	}

	_, perr := broker.Handle(context.Background(), "mcp_call", raw(t, protocol.MCPCallParams{
		CallID: "large-args", Server: "svc", Tool: "large-result", Arguments: json.RawMessage(`{"x":"` + strings.Repeat("a", protocol.MaxMCPArgsBytes) + `"}`),
	}))
	if perr == nil || !strings.Contains(perr.Message, "arguments too large") {
		t.Fatalf("argument bound error = %#v", perr)
	}
	result, perr := broker.Handle(context.Background(), "mcp_call", raw(t, protocol.MCPCallParams{CallID: "large-result", Server: "svc", Tool: "large-result"}))
	if perr != nil {
		t.Fatalf("large result: %v", perr)
	}
	got := result.(protocol.MCPCallResult)
	if len(got.Text) != protocol.MaxMCPResultBytes || !got.Truncated {
		t.Fatalf("result bytes = %d, truncated = %v", len(got.Text), got.Truncated)
	}
}

func TestCancellationByCallID(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := testServer(t, func(server *sdkmcp.Server) {
		addTextTool(server, "wait", func(ctx context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			close(started)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-release:
				return nil, errors.New("released")
			}
		})
	})
	broker := New([]config.MCPServer{{Name: "svc", URL: server.URL}}, nil).WithHTTPClient(server.Client())
	t.Cleanup(func() { _ = broker.Close() })
	list(t, broker)

	done := make(chan *protocol.Error, 1)
	go func() {
		_, perr := broker.Handle(context.Background(), "mcp_call", raw(t, protocol.MCPCallParams{CallID: "cancel-me", Server: "svc", Tool: "wait"}))
		done <- perr
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("tool call did not start")
	}
	if _, perr := broker.Handle(context.Background(), "mcp_cancel", raw(t, protocol.MCPCancelParams{CallID: "cancel-me"})); perr != nil {
		t.Fatalf("cancel: %v", perr)
	}
	select {
	case perr := <-done:
		if perr == nil || perr.Code != "canceled" {
			t.Fatalf("call error = %#v", perr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled call did not return")
	}
	close(release)
}

func TestConcurrentCallBound(t *testing.T) {
	server := testServer(t, func(server *sdkmcp.Server) { addTextTool(server, "echo", echoHandler) })
	broker := New([]config.MCPServer{{Name: "svc", URL: server.URL}}, nil).WithHTTPClient(server.Client())
	t.Cleanup(func() { _ = broker.Close() })
	list(t, broker)

	broker.mu.Lock()
	for i := 0; i < maxConcurrentCalls; i++ {
		broker.calls[fmt.Sprintf("active-%d", i)] = func() {}
	}
	broker.mu.Unlock()
	_, perr := broker.Handle(context.Background(), "mcp_call", raw(t, protocol.MCPCallParams{CallID: "overflow", Server: "svc", Tool: "echo"}))
	if perr == nil || !strings.Contains(perr.Message, "too many concurrent") {
		t.Fatalf("concurrency bound error = %#v", perr)
	}
}

func TestOfflineEmptyListDoesNotResolveCredentials(t *testing.T) {
	resolver := credsource.NewResolver()
	resolver.Register("test", staticSource{err: errors.New("must not resolve")})
	broker := New(nil, resolver)
	t.Cleanup(func() { _ = broker.Close() })
	if got := list(t, broker).Tools; len(got) != 0 {
		t.Fatalf("offline tools = %#v", got)
	}
	_, perr := broker.Handle(context.Background(), "mcp_call", raw(t, protocol.MCPCallParams{CallID: "offline", Server: "svc", Tool: "echo"}))
	if perr == nil {
		t.Fatal("offline call was accepted")
	}
}
