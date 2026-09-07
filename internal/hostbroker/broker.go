// Package hostbroker routes the guest's bounded semantic network operations to
// host-owned provider and MCP clients.
package hostbroker

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/credsource"
	"github.com/AdminTurnedDevOps/ABox/internal/llmbroker"
	"github.com/AdminTurnedDevOps/ABox/internal/mcpbroker"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

type Broker struct {
	mu       sync.RWMutex
	llm      *llmbroker.Broker
	mcp      *mcpbroker.Broker
	resolver *credsource.Resolver
}

func New(cfg config.File, model config.Model, resolver *credsource.Resolver) (*Broker, error) {
	servers, err := cfg.ResolvedMCPServers()
	if err != nil {
		return nil, err
	}
	return &Broker{
		llm:      llmbroker.New(cfg, model, resolver),
		mcp:      mcpbroker.New(servers, resolver),
		resolver: resolver,
	}, nil
}

func (b *Broker) Handle(ctx context.Context, method string, params json.RawMessage, notify func(string, any) error) (any, *protocol.Error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	switch method {
	case "provider_open", "provider_send", "provider_cancel":
		return b.llm.Handle(ctx, method, params, notify)
	case "mcp_list", "mcp_call", "mcp_cancel":
		return b.mcp.Handle(ctx, method, params)
	default:
		return nil, &protocol.Error{Code: "host", Message: "unknown broker method " + method}
	}
}

func (b *Broker) UpdateModel(cfg config.File, model config.Model) {
	b.mu.RLock()
	llm := b.llm
	b.mu.RUnlock()
	llm.UpdateModel(cfg, model)
}

func (b *Broker) UpdateMCP(cfg config.File) error {
	servers, err := cfg.ResolvedMCPServers()
	if err != nil {
		return err
	}
	next := mcpbroker.New(servers, b.resolver)
	b.mu.Lock()
	old := b.mcp
	b.mcp = next
	b.mu.Unlock()
	if old != nil {
		return old.Close()
	}
	return nil
}

func (b *Broker) SetMCPTokens(secrets map[string]string) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.mcp == nil {
		return fmt.Errorf("MCP broker is not configured")
	}
	return b.mcp.SetTokens(secrets)
}

func (b *Broker) SetLogger(logf func(string, ...any)) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	b.llm.SetLogger(logf)
}

func (b *Broker) Close() error {
	b.mu.Lock()
	mcp := b.mcp
	b.mcp = nil
	b.mu.Unlock()
	if mcp != nil {
		return mcp.Close()
	}
	return nil
}
