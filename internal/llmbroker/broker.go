// Package llmbroker is the host provider broker. The guest sends a model
// alias; this package resolves credentials and dials the provider. The guest
// never sees a credential, base URL, or HTTP header.
package llmbroker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/credsource"
	"github.com/AdminTurnedDevOps/ABox/internal/provider"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

const idleTimeout = 5 * time.Minute

type Broker struct {
	resolver *credsource.Resolver
	client   *http.Client
	logf     func(format string, args ...any)

	mu               sync.Mutex
	connectivityMode string
	model            config.Model
	next             int
	streams          map[string]*stream
}

type streamState uint8

const (
	streamReceiving streamState = iota
	streamStarted
	streamFinished
)

type stream struct {
	id        string
	model     config.Model
	rich      bool
	mu        sync.Mutex
	state     streamState
	buf       bytes.Buffer
	ctx       context.Context
	cancel    context.CancelFunc
	idleTimer *time.Timer
	bytesIn   int
}

func New(cfg config.File, model config.Model, resolver *credsource.Resolver) *Broker {
	return &Broker{
		connectivityMode: cfg.Connectivity.Mode,
		model:            model,
		resolver:         resolver,
		client:           &http.Client{Timeout: 5 * time.Minute},
		streams:          map[string]*stream{},
	}
}

// UpdateModel changes the model available to future opens. Existing streams
// retain the model they opened with and continue to count toward the limit.
func (b *Broker) UpdateModel(cfg config.File, model config.Model) {
	b.mu.Lock()
	b.connectivityMode = cfg.Connectivity.Mode
	b.model = model
	b.mu.Unlock()
}

func (b *Broker) WithHTTPClient(c *http.Client) *Broker {
	if c != nil {
		b.client = c
	}
	return b
}

func (b *Broker) SetLogger(f func(format string, args ...any)) {
	b.logf = f
}

func (b *Broker) log(format string, args ...any) {
	if b.logf != nil {
		b.logf(format, args...)
	}
}

func (b *Broker) Handle(ctx context.Context, method string, params json.RawMessage, notify func(method string, params any) error) (any, *protocol.Error) {
	switch method {
	case "provider_open":
		return b.open(ctx, params)
	case "provider_send":
		return b.send(params, notify)
	case "provider_cancel":
		return b.cancel(params)
	default:
		return nil, &protocol.Error{Code: "host", Message: "unknown broker method " + method}
	}
}

func (b *Broker) open(parent context.Context, raw json.RawMessage) (any, *protocol.Error) {
	p, err := protocol.DecodeParams[protocol.ProviderOpenParams](raw)
	if err != nil {
		return nil, &protocol.Error{Code: "host", Message: err.Error()}
	}
	if strings.TrimSpace(p.Model) == "" {
		return nil, &protocol.Error{Code: "host", Message: "model alias required"}
	}
	b.mu.Lock()
	if b.connectivityMode == "offline" {
		b.mu.Unlock()
		return nil, &protocol.Error{Code: "host", Message: "offline mode: provider access is disabled"}
	}
	model := b.model
	if p.Model != model.Name {
		b.mu.Unlock()
		return nil, &protocol.Error{Code: "host", Message: fmt.Sprintf("model profile %q is not selected for this session", p.Model)}
	}
	if len(b.streams) >= protocol.MaxProviderStreams {
		b.mu.Unlock()
		return nil, &protocol.Error{Code: "host", Message: "too many open provider streams"}
	}
	b.next++
	id := fmt.Sprintf("s%d", b.next)
	ctx, cancel := context.WithCancel(parent)
	st := &stream{id: id, model: model, rich: p.Rich, state: streamReceiving, ctx: ctx, cancel: cancel}
	st.idleTimer = time.AfterFunc(idleTimeout, cancel)
	b.streams[id] = st
	b.mu.Unlock()
	b.log("provider stream %s opened (model=%s rich=%v)", id, p.Model, p.Rich)
	go func() {
		<-ctx.Done()
		b.finish(st)
	}()
	return protocol.ProviderOpenResult{StreamID: id}, nil
}

func (b *Broker) send(raw json.RawMessage, notify func(string, any) error) (any, *protocol.Error) {
	p, err := protocol.DecodeParams[protocol.ProviderSendParams](raw)
	if err != nil {
		return nil, &protocol.Error{Code: "host", Message: err.Error()}
	}
	if p.StreamID == "" {
		return nil, &protocol.Error{Code: "host", Message: "provider stream id required"}
	}
	b.mu.Lock()
	st, ok := b.streams[p.StreamID]
	b.mu.Unlock()
	if !ok {
		return nil, &protocol.Error{Code: "host", Message: "unknown provider stream"}
	}
	st.mu.Lock()
	if st.state != streamReceiving {
		st.mu.Unlock()
		return nil, &protocol.Error{Code: "host", Message: "provider stream already started"}
	}
	if len(p.Data) > protocol.MaxProviderChunk {
		st.mu.Unlock()
		b.finish(st)
		return nil, &protocol.Error{Code: "host", Message: "provider chunk too large"}
	}
	if st.buf.Len()+len(p.Data) > protocol.MaxProviderRequest {
		st.mu.Unlock()
		b.finish(st)
		return nil, &protocol.Error{Code: "host", Message: "provider request too large"}
	}
	_, _ = st.buf.Write(p.Data)
	st.bytesIn += len(p.Data)
	if st.idleTimer != nil {
		st.idleTimer.Reset(idleTimeout)
	}
	if !p.Last {
		st.mu.Unlock()
		return map[string]bool{"ok": true}, nil
	}
	st.state = streamStarted
	body := append([]byte(nil), st.buf.Bytes()...)
	st.buf = bytes.Buffer{}
	st.mu.Unlock()
	return b.start(st, body, notify)
}

func (b *Broker) start(st *stream, body []byte, notify func(string, any) error) (any, *protocol.Error) {
	var req protocol.ProviderRequest
	if err := json.Unmarshal(body, &req); err != nil {
		b.finish(st)
		return nil, &protocol.Error{Code: "host", Message: "malformed provider request"}
	}
	body = nil
	if len(req.Messages) > protocol.MaxProviderMessages {
		b.finish(st)
		return nil, &protocol.Error{Code: "host", Message: "too many provider messages"}
	}
	if len(req.Tools) > protocol.MaxProviderTools {
		b.finish(st)
		return nil, &protocol.Error{Code: "host", Message: "too many provider tools"}
	}
	for _, m := range req.Messages {
		if len(m.ToolArgs) > protocol.MaxProviderToolArgs {
			b.finish(st)
			return nil, &protocol.Error{Code: "host", Message: "tool args too large"}
		}
	}
	msgs := make([]provider.Message, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = provider.Message{
			Role: m.Role, Content: m.Content, ToolID: m.ToolID,
			ToolName: m.ToolName, ToolArgs: m.ToolArgs, ToolResult: m.ToolResult,
		}
	}
	tools := make([]provider.ToolSchema, len(req.Tools))
	for i, t := range req.Tools {
		tools[i] = provider.ToolSchema{Name: t.Name, Description: t.Description, Parameters: t.Parameters}
	}
	if b.resolver == nil {
		b.finish(st)
		return nil, &protocol.Error{Code: "host", Message: "no credential resolver configured"}
	}
	ref := st.model.CredentialReference()
	val, err := b.resolver.Resolve(st.ctx, credsource.FromConfig(ref))
	if err != nil {
		b.finish(st)
		return nil, &protocol.Error{Code: "host", Message: fmt.Sprintf("credential for model %q (%s %s): %v", st.model.Name, ref.Source, ref.Name, err)}
	}
	key := string(val.Bytes)
	val.Zero()

	st.mu.Lock()
	if st.state != streamStarted || st.ctx.Err() != nil {
		st.mu.Unlock()
		b.finish(st)
		return nil, &protocol.Error{Code: "canceled", Message: "provider stream canceled"}
	}
	if st.idleTimer == nil {
		st.idleTimer = time.AfterFunc(idleTimeout, st.cancel)
	} else {
		st.idleTimer.Reset(idleTimeout)
	}
	st.mu.Unlock()
	go b.pump(st.ctx, st, key, msgs, tools, notify)
	return map[string]bool{"ok": true}, nil
}

func (b *Broker) pump(ctx context.Context, st *stream, key string, msgs []provider.Message, tools []provider.ToolSchema, notify func(string, any) error) {
	var events <-chan provider.Event
	var err error
	if st.rich {
		events, err = provider.StreamWithUsage(ctx, st.model, key, b.client, msgs, tools)
	} else {
		events, err = provider.Stream(ctx, st.model, key, b.client, msgs, tools)
	}
	if err != nil {
		b.notify(notify, protocol.ProviderEventParams{StreamID: st.id, Type: "error", Err: err.Error()})
		b.finish(st)
		return
	}
	b.forwardEvents(st, events, notify)
}

func (b *Broker) forwardEvents(st *stream, events <-chan provider.Event, notify func(string, any) error) {
	terminalSent := false
	eventCount := 0
	draining := false
	for ev := range events {
		if draining {
			continue
		}
		eventCount++
		if eventCount > protocol.MaxProviderEvents {
			st.cancel()
			b.notify(notify, protocol.ProviderEventParams{StreamID: st.id, Type: "error", Err: "too many provider events"})
			terminalSent = true
			draining = true
			continue
		}
		st.mu.Lock()
		if st.state == streamStarted && st.idleTimer != nil {
			st.idleTimer.Reset(idleTimeout)
		}
		st.mu.Unlock()
		p := protocol.ProviderEventParams{
			StreamID: st.id, Type: ev.Type, Text: ev.Text,
			ToolID: ev.ToolID, ToolName: ev.ToolName, ToolArgs: ev.ToolArgs,
			Usage: ev.Usage, StopReason: ev.StopReason,
		}
		if ev.Err != nil {
			p.Err = ev.Err.Error()
		}
		terminal := ev.Type == "done" || ev.Type == "error"
		raw, marshalErr := json.Marshal(p)
		if marshalErr != nil || len(raw) > protocol.MaxProviderEvent || len(p.ToolArgs) > protocol.MaxProviderToolArgs {
			st.cancel()
			b.notify(notify, protocol.ProviderEventParams{StreamID: st.id, Type: "error", Err: "provider event too large"})
			terminalSent = true
			draining = true
			continue
		}
		if err := b.notify(notify, p); err != nil {
			terminalSent = true
			draining = true
			st.cancel()
			continue
		}
		if terminal {
			terminalSent = true
			draining = true
			st.cancel()
		}
	}
	if !terminalSent {
		b.notify(notify, protocol.ProviderEventParams{StreamID: st.id, Type: "done"})
	}
	b.finish(st)
}

func (b *Broker) notify(notify func(string, any) error, p protocol.ProviderEventParams) error {
	if notify == nil {
		return fmt.Errorf("provider event notifier unavailable")
	}
	return notify("provider_event", p)
}

func (b *Broker) cancel(raw json.RawMessage) (any, *protocol.Error) {
	p, err := protocol.DecodeParams[protocol.ProviderCancelParams](raw)
	if err != nil {
		return nil, &protocol.Error{Code: "host", Message: err.Error()}
	}
	b.mu.Lock()
	st := b.streams[p.StreamID]
	b.mu.Unlock()
	if st != nil {
		b.log("provider stream %s canceled", st.id)
		st.mu.Lock()
		receiving := st.state == streamReceiving
		st.mu.Unlock()
		st.cancel()
		if receiving {
			b.finish(st)
		}
	}
	return map[string]bool{"ok": true}, nil
}

func (b *Broker) finish(st *stream) {
	st.mu.Lock()
	if st.state == streamFinished {
		st.mu.Unlock()
		return
	}
	st.state = streamFinished
	st.buf = bytes.Buffer{}
	if st.idleTimer != nil {
		st.idleTimer.Stop()
		st.idleTimer = nil
	}
	bytesIn := st.bytesIn
	st.mu.Unlock()
	st.cancel()
	b.mu.Lock()
	if b.streams[st.id] == st {
		delete(b.streams, st.id)
	}
	b.mu.Unlock()
	b.log("provider stream %s closed (bytes_in=%d)", st.id, bytesIn)
}
