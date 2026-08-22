package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/AdminTurnedDevOps/ABox/protocol"
)

type echoArgs struct {
	Text string `json:"text"`
}

func echoTool(_ context.Context, _ *sdkmcp.CallToolRequest, args echoArgs) (*sdkmcp.CallToolResult, any, error) {
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "echo:" + args.Text}},
	}, nil, nil
}

func serveMCP(t *testing.T, stateless bool) *httptest.Server {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "1"}, nil)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "echo", Description: "echo text"}, echoTool)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "hidden", Description: "should be filtered"}, echoTool)
	h := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, &sdkmcp.StreamableHTTPOptions{
		Stateless: stateless,
	})
	return httptest.NewServer(h)
}

func TestConnectStatefulListsPrefixedTools(t *testing.T) {
	ts := serveMCP(t, false)
	defer ts.Close()
	m := New([]protocol.GuestMCPServer{{
		Name:      "svc",
		URL:       ts.URL,
		Allowlist: []string{"echo"},
	}}, nil).WithHTTPClient(ts.Client())
	if err := m.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	tools := m.Tools()
	if len(tools) != 1 || tools[0].Prefixed != "svc__echo" {
		t.Fatalf("tools=%#v", tools)
	}
}

func TestConnectStatelessCall(t *testing.T) {
	ts := serveMCP(t, true)
	defer ts.Close()
	m := New([]protocol.GuestMCPServer{{Name: "svc", URL: ts.URL}}, nil).WithHTTPClient(ts.Client())
	if err := m.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	out, err := m.Call(context.Background(), "svc", "echo", json.RawMessage(`{"text":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "echo:hi") {
		t.Fatalf("out=%q", out)
	}
}

func TestBearerHeaderSent(t *testing.T) {
	inner := serveMCP(t, true)
	defer inner.Close()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			http.Error(w, "missing bearer", http.StatusUnauthorized)
			return
		}
		inner.Config.Handler.ServeHTTP(w, r)
	}))
	defer ts.Close()
	m := New([]protocol.GuestMCPServer{{
		Name:     "svc",
		URL:      ts.URL,
		TokenEnv: "SVC_TOKEN",
	}}, map[string]string{"SVC_TOKEN": "test-token"}).WithHTTPClient(ts.Client())
	if err := m.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer m.Close()
}

func TestConnect401DoesNotAbortOthers(t *testing.T) {
	ok := serveMCP(t, true)
	defer ok.Close()
	deny := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
		_, _ = io.Copy(io.Discard, r.Body)
	}))
	defer deny.Close()
	m := New([]protocol.GuestMCPServer{
		{Name: "bad", URL: deny.URL},
		{Name: "ok", URL: ok.URL},
	}, nil).WithHTTPClient(http.DefaultClient)
	if err := m.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if len(m.Tools()) == 0 {
		t.Fatal("expected ok server tools")
	}
}
