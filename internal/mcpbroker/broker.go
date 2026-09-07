// Package mcpbroker is the host-side semantic MCP broker. The guest can name
// configured servers and discovered tools, but cannot supply URLs or headers.
package mcpbroker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/credsource"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

const (
	connectTimeout     = 30 * time.Second
	maxConcurrentCalls = protocol.MaxGuestCalls - 1 // The RPC layer reserves one slot for cancellation.
)

type Broker struct {
	servers  []config.MCPServer
	resolver *credsource.Resolver
	client   *http.Client

	discoverMu sync.Mutex
	mu         sync.Mutex
	discovered bool
	closed     bool
	sessions   map[string]*serverSession
	tools      []protocol.MCPTool
	calls      map[string]context.CancelFunc
	overrides  map[string]string
}

type serverSession struct {
	configured map[string]struct{}
	discovered map[string]struct{}
	session    *sdkmcp.ClientSession
}

// New accepts the already policy-resolved MCP server list. An empty list is
// the offline configuration and performs no network or credential access.
func New(servers []config.MCPServer, resolver *credsource.Resolver) *Broker {
	configured := make([]config.MCPServer, len(servers))
	for i, server := range servers {
		configured[i] = server
		configured[i].Scopes = append([]string(nil), server.Scopes...)
		configured[i].ToolAllowlist = append([]string(nil), server.ToolAllowlist...)
		if server.Credential != nil {
			credential := *server.Credential
			configured[i].Credential = &credential
		}
	}
	return &Broker{
		servers:   configured,
		resolver:  resolver,
		client:    &http.Client{Timeout: 5 * time.Minute},
		sessions:  map[string]*serverSession{},
		calls:     map[string]context.CancelFunc{},
		overrides: map[string]string{},
	}
}

// WithHTTPClient installs the base client used by MCP transports. Call it
// before Handle; each server gets an origin-bound clone of this client.
func (b *Broker) WithHTTPClient(client *http.Client) *Broker {
	if client != nil {
		b.client = client
	}
	return b
}

// Handle serves the protocol v4 semantic MCP methods.
func (b *Broker) Handle(ctx context.Context, method string, raw json.RawMessage) (any, *protocol.Error) {
	switch method {
	case "mcp_list":
		return b.list(ctx, raw)
	case "mcp_call":
		return b.call(ctx, raw)
	case "mcp_cancel":
		return b.cancel(raw)
	default:
		return nil, hostError("unknown broker method " + method)
	}
}

func (b *Broker) list(ctx context.Context, raw json.RawMessage) (any, *protocol.Error) {
	if _, err := protocol.DecodeParams[protocol.MCPListParams](raw); err != nil {
		return nil, hostError(err.Error())
	}
	if err := b.discover(ctx); err != nil {
		return nil, brokerError(err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	tools := append([]protocol.MCPTool(nil), b.tools...)
	return protocol.MCPListResult{Tools: tools}, nil
}

func (b *Broker) discover(ctx context.Context) error {
	b.discoverMu.Lock()
	defer b.discoverMu.Unlock()

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errors.New("MCP broker is closed")
	}
	if b.discovered {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()

	sessions := make(map[string]*serverSession, len(b.servers))
	var tools []protocol.MCPTool
	for _, server := range b.servers {
		if server.Name == "" || server.URL == "" {
			closeSessions(sessions)
			return errors.New("configured MCP server is missing an ID or URL")
		}
		if _, exists := sessions[server.Name]; exists {
			closeSessions(sessions)
			return fmt.Errorf("duplicate configured MCP server ID %q", server.Name)
		}
		if len(tools) >= protocol.MaxMCPTools {
			break
		}
		session, serverTools, err := b.connect(ctx, server, protocol.MaxMCPTools-len(tools))
		if err != nil {
			closeSessions(sessions)
			return fmt.Errorf("MCP server %q: %w", server.Name, err)
		}
		sessions[server.Name] = session
		tools = append(tools, serverTools...)
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		closeSessions(sessions)
		return errors.New("MCP broker is closed")
	}
	b.sessions = sessions
	b.tools = tools
	b.discovered = true
	b.mu.Unlock()
	return nil
}

func (b *Broker) connect(parent context.Context, server config.MCPServer, limit int) (*serverSession, []protocol.MCPTool, error) {
	endpoint, err := url.Parse(server.URL)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" || endpoint.User != nil {
		return nil, nil, errors.New("configured endpoint is invalid")
	}
	token, err := b.resolveToken(parent, server)
	if err != nil {
		return nil, nil, err
	}
	client := originClient(b.client, endpoint, token)
	token = ""

	ctx, cancel := context.WithTimeout(parent, connectTimeout)
	defer cancel()
	cli := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "abox-host", Version: "dev"}, nil)
	session, err := cli.Connect(ctx, &sdkmcp.StreamableClientTransport{
		Endpoint:   server.URL,
		HTTPClient: client,
	}, nil)
	if err != nil {
		return nil, nil, err
	}

	configured := make(map[string]struct{}, len(server.ToolAllowlist))
	for _, name := range server.ToolAllowlist {
		configured[name] = struct{}{}
	}
	ss := &serverSession{
		configured: configured,
		discovered: map[string]struct{}{},
		session:    session,
	}
	tools, err := listTools(ctx, server.Name, ss, limit)
	if err != nil {
		_ = session.Close()
		return nil, nil, err
	}
	return ss, tools, nil
}

func (b *Broker) resolveToken(ctx context.Context, server config.MCPServer) (string, error) {
	b.mu.Lock()
	override, overridden := b.overrides[config.TokenEnv(server)]
	b.mu.Unlock()
	if overridden {
		return strings.TrimSpace(override), nil
	}
	if b.resolver == nil {
		return "", nil
	}
	value, err := b.resolver.Resolve(ctx, credsource.FromConfig(server.CredentialReference()))
	if err != nil {
		value.Zero()
		if errors.Is(err, credsource.ErrNotFound) {
			return "", nil
		}
		return "", errors.New("credential resolution failed")
	}
	token := strings.TrimSpace(string(value.Bytes))
	value.Zero()
	return token, nil
}

// SetTokens updates host-memory credential overrides and forces MCP sessions
// to reconnect. Unknown destinations are rejected transactionally.
func (b *Broker) SetTokens(secrets map[string]string) error {
	allowed := make(map[string]struct{}, len(b.servers))
	for _, server := range b.servers {
		allowed[config.TokenEnv(server)] = struct{}{}
	}
	for name := range secrets {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("unknown MCP credential destination %q", name)
		}
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errors.New("MCP broker is closed")
	}
	for name, value := range secrets {
		b.overrides[name] = value
	}
	sessions := b.sessions
	calls := b.calls
	b.sessions = map[string]*serverSession{}
	b.calls = map[string]context.CancelFunc{}
	b.tools = nil
	b.discovered = false
	b.mu.Unlock()
	for _, cancel := range calls {
		cancel()
	}
	return closeSessions(sessions)
}

func listTools(ctx context.Context, serverID string, ss *serverSession, limit int) ([]protocol.MCPTool, error) {
	tools := make([]protocol.MCPTool, 0, limit)
	seenCursors := map[string]struct{}{}
	cursor := ""
	for len(tools) < limit {
		listed, err := ss.session.ListTools(ctx, &sdkmcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		for _, tool := range listed.Tools {
			if tool == nil || tool.Name == "" {
				continue
			}
			if len(ss.configured) > 0 {
				if _, ok := ss.configured[tool.Name]; !ok {
					continue
				}
			}
			if _, duplicate := ss.discovered[tool.Name]; duplicate {
				continue
			}
			parameters, ok := boundedSchema(tool.InputSchema)
			if !ok {
				continue
			}
			ss.discovered[tool.Name] = struct{}{}
			tools = append(tools, protocol.MCPTool{
				Server:      serverID,
				Name:        tool.Name,
				Prefixed:    serverID + "__" + tool.Name,
				Description: tool.Description,
				Parameters:  parameters,
			})
			if len(tools) == limit {
				break
			}
		}
		if listed.NextCursor == "" || len(tools) == limit {
			break
		}
		if _, duplicate := seenCursors[listed.NextCursor]; duplicate {
			return nil, errors.New("tool pagination repeated a cursor")
		}
		seenCursors[listed.NextCursor] = struct{}{}
		cursor = listed.NextCursor
	}
	return tools, nil
}

func boundedSchema(schema any) (map[string]any, bool) {
	if schema == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}, true
	}
	raw, err := json.Marshal(schema)
	if err != nil || len(raw) > protocol.MaxMCPSchemaBytes {
		return nil, false
	}
	var parameters map[string]any
	if err := json.Unmarshal(raw, &parameters); err != nil || parameters == nil {
		return nil, false
	}
	return parameters, true
}

func (b *Broker) call(parent context.Context, raw json.RawMessage) (any, *protocol.Error) {
	p, err := protocol.DecodeParams[protocol.MCPCallParams](raw)
	if err != nil {
		return nil, hostError(err.Error())
	}
	if p.CallID == "" {
		return nil, hostError("MCP call ID required")
	}
	if len(p.Arguments) > protocol.MaxMCPArgsBytes {
		return nil, hostError("MCP arguments too large")
	}
	var arguments any = map[string]any{}
	if len(p.Arguments) > 0 {
		if err := json.Unmarshal(p.Arguments, &arguments); err != nil {
			return nil, hostError("malformed MCP arguments")
		}
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, hostError("MCP broker is closed")
	}
	ss := b.sessions[p.Server]
	if ss == nil {
		b.mu.Unlock()
		return nil, hostError(fmt.Sprintf("MCP server %q is not configured and discovered", p.Server))
	}
	if len(ss.configured) > 0 {
		if _, ok := ss.configured[p.Tool]; !ok {
			b.mu.Unlock()
			return nil, hostError("MCP tool is not allowed")
		}
	}
	if _, ok := ss.discovered[p.Tool]; !ok {
		b.mu.Unlock()
		return nil, hostError("MCP tool was not discovered")
	}
	if _, duplicate := b.calls[p.CallID]; duplicate {
		b.mu.Unlock()
		return nil, hostError("MCP call ID is already active")
	}
	if len(b.calls) >= maxConcurrentCalls {
		b.mu.Unlock()
		return nil, hostError("too many concurrent MCP calls")
	}
	ctx, cancel := context.WithCancel(parent)
	b.calls[p.CallID] = cancel
	b.mu.Unlock()
	defer func() {
		cancel()
		b.mu.Lock()
		delete(b.calls, p.CallID)
		b.mu.Unlock()
	}()

	if _, ok := ctx.Deadline(); !ok {
		var timeoutCancel context.CancelFunc
		ctx, timeoutCancel = context.WithTimeout(ctx, protocol.DefaultRPCTimeout)
		defer timeoutCancel()
	}
	result, err := ss.session.CallTool(ctx, &sdkmcp.CallToolParams{Name: p.Tool, Arguments: arguments})
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, &protocol.Error{Code: "canceled", Message: "MCP call canceled"}
		}
		return nil, hostError(err.Error())
	}
	return boundedResult(result)
}

func boundedResult(result *sdkmcp.CallToolResult) (any, *protocol.Error) {
	if result == nil {
		return nil, hostError("empty MCP result")
	}
	var text strings.Builder
	truncated := false
	for _, content := range result.Content {
		var part string
		switch value := content.(type) {
		case *sdkmcp.TextContent:
			part = value.Text
		default:
			raw, err := json.Marshal(content)
			if err != nil {
				continue
			}
			part = string(raw)
		}
		remaining := protocol.MaxMCPResultBytes - text.Len()
		if len(part) > remaining {
			text.WriteString(validPrefix(part, remaining))
			truncated = true
			break
		}
		text.WriteString(part)
	}
	return protocol.MCPCallResult{Text: text.String(), IsError: result.IsError, Truncated: truncated}, nil
}

func validPrefix(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func (b *Broker) cancel(raw json.RawMessage) (any, *protocol.Error) {
	p, err := protocol.DecodeParams[protocol.MCPCancelParams](raw)
	if err != nil {
		return nil, hostError(err.Error())
	}
	if p.CallID == "" {
		return nil, hostError("MCP call ID required")
	}
	b.mu.Lock()
	cancel := b.calls[p.CallID]
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return map[string]bool{"ok": true}, nil
}

// Close cancels active calls and closes all MCP sessions. It is idempotent.
func (b *Broker) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	calls := b.calls
	b.calls = map[string]context.CancelFunc{}
	sessions := b.sessions
	b.sessions = map[string]*serverSession{}
	b.tools = nil
	b.mu.Unlock()

	for _, cancel := range calls {
		cancel()
	}
	return closeSessions(sessions)
}

func closeSessions(sessions map[string]*serverSession) error {
	var first error
	for _, ss := range sessions {
		if ss != nil && ss.session != nil {
			if err := ss.session.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

func originClient(base *http.Client, endpoint *url.URL, token string) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	client := *base
	origin := normalizedOrigin(endpoint)
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.Transport = originRoundTripper{base: transport, origin: origin, token: token}
	previous := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if normalizedOrigin(req.URL) != origin {
			return errors.New("MCP redirect crossed the configured origin")
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return &client
}

type originRoundTripper struct {
	base   http.RoundTripper
	origin string
	token  string
}

func (rt originRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if normalizedOrigin(req.URL) != rt.origin {
		return nil, errors.New("MCP request left the configured origin")
	}
	clone := req.Clone(req.Context())
	if rt.token != "" {
		clone.Header.Set("Authorization", "Bearer "+rt.token)
	}
	return rt.base.RoundTrip(clone)
}

func normalizedOrigin(u *url.URL) string {
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" {
		switch scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	return scheme + "\x00" + host + "\x00" + port
}

func hostError(message string) *protocol.Error {
	return &protocol.Error{Code: "host", Message: message}
}

func brokerError(err error) *protocol.Error {
	if errors.Is(err, context.Canceled) {
		return &protocol.Error{Code: "canceled", Message: "MCP operation canceled"}
	}
	return hostError(err.Error())
}
