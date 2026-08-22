// Package mcp is the guest Streamable HTTP MCP client.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/AdminTurnedDevOps/ABox/internal/guest/egress"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

const (
	maxSchemaBytes = 64 << 10
	maxResultBytes = 1 << 20
)

type Tool struct {
	Server      string
	Name        string
	Prefixed    string
	Description string
	Parameters  map[string]any
}

type Manager struct {
	servers []protocol.GuestMCPServer
	secrets map[string]string
	client  *http.Client

	mu    sync.Mutex
	conns map[string]*conn
	tools []Tool
}

type conn struct {
	session *sdkmcp.ClientSession
}

func New(servers []protocol.GuestMCPServer, secrets map[string]string) *Manager {
	return &Manager{
		servers: servers,
		secrets: secrets,
		conns:   map[string]*conn{},
	}
}

func (m *Manager) WithHTTPClient(c *http.Client) *Manager {
	m.client = c
	return m
}

func (m *Manager) SetSecrets(secrets map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.secrets == nil {
		m.secrets = map[string]string{}
	}
	for k, v := range secrets {
		m.secrets[k] = v
	}
}

func (m *Manager) Connect(ctx context.Context) error {
	for _, s := range m.servers {
		if err := m.connectOne(ctx, s); err != nil {
			// Best-effort: one failed server must not block the others.
			continue
		}
	}
	return nil
}

func (m *Manager) connectOne(ctx context.Context, s protocol.GuestMCPServer) error {
	if s.Name == "" || s.URL == "" {
		return fmt.Errorf("mcp server missing name or url")
	}
	token := m.tokenFor(s)
	httpClient := m.httpClientFor(token)
	cli := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "abox-guest", Version: "dev"}, nil)
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	session, err := cli.Connect(cctx, &sdkmcp.StreamableClientTransport{
		Endpoint:   s.URL,
		HTTPClient: httpClient,
	}, nil)
	if err != nil {
		return err
	}
	listed, err := session.ListTools(cctx, nil)
	if err != nil {
		_ = session.Close()
		return err
	}
	allow := map[string]struct{}{}
	for _, n := range s.Allowlist {
		allow[n] = struct{}{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing := m.conns[s.Name]; existing != nil {
		_ = existing.session.Close()
	}
	m.conns[s.Name] = &conn{session: session}
	var kept []Tool
	for _, t := range m.tools {
		if t.Server != s.Name {
			kept = append(kept, t)
		}
	}
	m.tools = kept
	for _, t := range listed.Tools {
		if len(allow) > 0 {
			if _, ok := allow[t.Name]; !ok {
				continue
			}
		}
		params, ok := schemaMap(t.InputSchema)
		if !ok {
			continue
		}
		m.tools = append(m.tools, Tool{
			Server:      s.Name,
			Name:        t.Name,
			Prefixed:    s.Name + "__" + t.Name,
			Description: t.Description,
			Parameters:  params,
		})
	}
	return nil
}

func (m *Manager) Tools() []Tool {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Tool, len(m.tools))
	copy(out, m.tools)
	return out
}

func (m *Manager) Call(ctx context.Context, server, tool string, args json.RawMessage) (string, error) {
	m.mu.Lock()
	c := m.conns[server]
	m.mu.Unlock()
	if c == nil || c.session == nil {
		return "", fmt.Errorf("mcp server %q is not connected", server)
	}
	var arguments any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &arguments); err != nil {
			return "", fmt.Errorf("mcp args: %w", err)
		}
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
	}
	res, err := c.session.CallTool(ctx, &sdkmcp.CallToolParams{Name: tool, Arguments: arguments})
	if err != nil {
		return "", err
	}
	return resultText(res)
}

func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var last error
	for name, c := range m.conns {
		if c != nil && c.session != nil {
			if err := c.session.Close(); err != nil {
				last = err
			}
		}
		delete(m.conns, name)
	}
	m.tools = nil
	return last
}

func (m *Manager) tokenFor(s protocol.GuestMCPServer) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.TokenEnv == "" || m.secrets == nil {
		return ""
	}
	return strings.TrimSpace(m.secrets[s.TokenEnv])
}

func (m *Manager) httpClientFor(token string) *http.Client {
	base := m.client
	if base == nil {
		base = &http.Client{Timeout: 5 * time.Minute, Transport: egress.Transport()}
	}
	rt := base.Transport
	if token == "" {
		return base
	}
	return &http.Client{
		Timeout:   base.Timeout,
		Transport: bearerRT{base: rt, token: token},
	}
}

type bearerRT struct {
	base  http.RoundTripper
	token string
}

func (b bearerRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	base := b.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

func schemaMap(v any) (map[string]any, bool) {
	if v == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}, true
	}
	if m, ok := v.(map[string]any); ok {
		b, err := json.Marshal(m)
		if err != nil || len(b) > maxSchemaBytes {
			return nil, false
		}
		return m, true
	}
	b, err := json.Marshal(v)
	if err != nil || len(b) > maxSchemaBytes {
		return nil, false
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}, true
	}
	return m, true
}

func resultText(res *sdkmcp.CallToolResult) (string, error) {
	if res == nil {
		return "", fmt.Errorf("empty mcp result")
	}
	var b strings.Builder
	for _, c := range res.Content {
		switch t := c.(type) {
		case *sdkmcp.TextContent:
			b.WriteString(t.Text)
		default:
			raw, err := json.Marshal(c)
			if err != nil {
				continue
			}
			b.Write(raw)
		}
	}
	s := b.String()
	if len(s) > maxResultBytes {
		s = s[:maxResultBytes] + "…"
	}
	if res.IsError {
		return "", fmt.Errorf("%s", s)
	}
	return s, nil
}
