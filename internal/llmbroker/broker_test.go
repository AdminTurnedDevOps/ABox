package llmbroker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/credsource"
	"github.com/AdminTurnedDevOps/ABox/internal/provider"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

type rotatingSource struct {
	mu    sync.Mutex
	vals  []string
	calls int
	refs  []string
}

func (r *rotatingSource) Resolve(_ context.Context, ref credsource.Reference) (credsource.Value, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refs = append(r.refs, ref.Source+"/"+ref.Name)
	if r.calls >= len(r.vals) {
		return credsource.Value{}, credsource.ErrNotFound
	}
	v := r.vals[r.calls]
	r.calls++
	return credsource.Value{Bytes: []byte(v)}, nil
}

func (r *rotatingSource) Close() error { return nil }

func (r *rotatingSource) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

type notifyRecorder struct {
	mu      sync.Mutex
	events  []protocol.ProviderEventParams
	waiters []chan struct{}
}

func (n *notifyRecorder) notify(method string, params any) error {
	if method != "provider_event" {
		return nil
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	var p protocol.ProviderEventParams
	_ = json.Unmarshal(raw, &p)
	n.mu.Lock()
	n.events = append(n.events, p)
	waiters := n.waiters
	n.waiters = nil
	n.mu.Unlock()
	for _, w := range waiters {
		close(w)
	}
	return nil
}

func (n *notifyRecorder) waitFor(t *testing.T, timeout time.Duration, pred func(protocol.ProviderEventParams) bool) protocol.ProviderEventParams {
	t.Helper()
	deadline := time.After(timeout)
	for {
		n.mu.Lock()
		for _, ev := range n.events {
			if pred(ev) {
				n.mu.Unlock()
				return ev
			}
		}
		w := make(chan struct{})
		n.waiters = append(n.waiters, w)
		n.mu.Unlock()
		select {
		case <-w:
		case <-deadline:
			t.Fatal("timed out waiting for provider_event")
		}
	}
}

func sseProvider(t *testing.T, provider string, stream func(w http.ResponseWriter, auth, body string)) (srv *httptest.Server, authSeen func() []string) {
	t.Helper()
	var mu sync.Mutex
	var auths []string
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			auth = r.Header.Get("x-api-key")
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		auths = append(auths, auth)
		mu.Unlock()
		stream(w, auth, string(body))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), auths...) }
}

func openStream(t *testing.T, b *Broker, alias string) string {
	t.Helper()
	res, perr := b.Handle(context.Background(), "provider_open", mustJSON(t, protocol.ProviderOpenParams{Model: alias}), nil)
	if perr != nil {
		t.Fatalf("open: %v", perr)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var openRes protocol.ProviderOpenResult
	_ = json.Unmarshal(raw, &openRes)
	return openRes.StreamID
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func sendChunk(t *testing.T, b *Broker, rec *notifyRecorder, streamID string, data []byte, last bool) (any, *protocol.Error) {
	t.Helper()
	return b.Handle(context.Background(), "provider_send", mustJSON(t, protocol.ProviderSendParams{
		StreamID: streamID, Data: data, Last: last,
	}), rec.notify)
}

func defaultCfg() config.File {
	cfg := config.Defaults()
	cfg.Connectivity.Mode = "direct"
	return cfg
}

func TestBrokerStreamsOpenAITextHostAuth(t *testing.T) {
	srv, authSeen := sseProvider(t, "openai", func(w http.ResponseWriter, auth, body string) {
		if !strings.HasPrefix(auth, "Bearer k1") {
			t.Errorf("host-side auth missing: %q", auth)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`data: {"choices":[{"delta":{"content":"hi"}}]}`,
			`data: [DONE]`,
			"",
		}, "\n"))
	})
	cfg := defaultCfg()
	cfg.Models[1].BaseURL = srv.URL // openai-default
	r := credsource.NewResolver()
	r.Register("rot", &rotatingSource{vals: []string{"k1"}})
	cfg.Models[1].CredentialEnv = ""
	cfg.Models[1].Credential = &config.CredentialRef{Source: "rot", Name: "openai"}
	b := New(cfg, r).WithHTTPClient(srv.Client())

	rec := &notifyRecorder{}
	id := openStream(t, b, "openai-default")
	req := protocol.ProviderRequest{Messages: []protocol.ProviderMessage{{Role: "user", Content: "q"}}}
	raw, _ := json.Marshal(req)
	if _, perr := sendChunk(t, b, rec, id, raw, true); perr != nil {
		t.Fatalf("send: %v", perr)
	}
	ev := rec.waitFor(t, 2*time.Second, func(p protocol.ProviderEventParams) bool { return p.Text == "hi" && p.StreamID == id })
	if ev.Type != "text" {
		t.Fatalf("event %+v", ev)
	}
	rec.waitFor(t, 2*time.Second, func(p protocol.ProviderEventParams) bool { return p.Type == "done" })
	if got := authSeen(); len(got) != 1 {
		t.Fatalf("provider calls %d", len(got))
	}
}

func TestBrokerAnthropicKeyHeader(t *testing.T) {
	srv, authSeen := sseProvider(t, "anthropic", func(w http.ResponseWriter, auth, body string) {
		if auth != "k-ant" {
			t.Errorf("x-api-key %q", auth)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`data: {"type":"content_block_delta","delta":{"text":"yo"}}`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
			"",
		}, "\n"))
	})
	cfg := defaultCfg()
	cfg.Models[2].BaseURL = srv.URL // claude-default
	r := credsource.NewResolver()
	r.Register("rot", &rotatingSource{vals: []string{"k-ant"}})
	cfg.Models[2].CredentialEnv = ""
	cfg.Models[2].Credential = &config.CredentialRef{Source: "rot", Name: "anthropic"}
	b := New(cfg, r).WithHTTPClient(srv.Client())

	rec := &notifyRecorder{}
	id := openStream(t, b, "claude-default")
	req := protocol.ProviderRequest{Messages: []protocol.ProviderMessage{{Role: "user", Content: "q"}}}
	raw, _ := json.Marshal(req)
	if _, perr := sendChunk(t, b, rec, id, raw, true); perr != nil {
		t.Fatalf("send: %v", perr)
	}
	rec.waitFor(t, 2*time.Second, func(p protocol.ProviderEventParams) bool { return p.Text == "yo" })
	rec.waitFor(t, 2*time.Second, func(p protocol.ProviderEventParams) bool { return p.Type == "done" })
	if got := authSeen(); len(got) != 1 || got[0] != "k-ant" {
		t.Fatalf("auth %v", got)
	}
}

func TestBrokerUnknownAliasRejected(t *testing.T) {
	b := New(defaultCfg(), credsource.NewResolver())
	_, perr := b.Handle(context.Background(), "provider_open", mustJSON(t, providerOpenJSON("nope")), nil)
	if perr == nil || !strings.Contains(perr.Message, "unknown model profile") {
		t.Fatalf("got %+v", perr)
	}
}

func providerOpenJSON(name string) protocol.ProviderOpenParams {
	return protocol.ProviderOpenParams{Model: name}
}

func TestBrokerOfflineRejected(t *testing.T) {
	cfg := defaultCfg()
	cfg.Connectivity.Mode = "offline"
	b := New(cfg, credsource.NewResolver())
	_, perr := b.Handle(context.Background(), "provider_open", mustJSON(t, providerOpenJSON("openai-default")), nil)
	if perr == nil || !strings.Contains(perr.Message, "offline") {
		t.Fatalf("got %+v", perr)
	}
}

func TestBrokerUnknownStreamRejected(t *testing.T) {
	b := New(defaultCfg(), credsource.NewResolver())
	_, perr := b.Handle(context.Background(), "provider_send", mustJSON(t, protocol.ProviderSendParams{StreamID: "s99", Last: true}), nil)
	if perr == nil || !strings.Contains(perr.Message, "unknown provider stream") {
		t.Fatalf("got %+v", perr)
	}
}

func TestBrokerChunkBudgetRejected(t *testing.T) {
	cfg := defaultCfg()
	r := credsource.NewResolver()
	b := New(cfg, r)
	id := openStream(t, b, "openai-default")
	remaining := protocol.MaxProviderRequest - 4
	for remaining > 0 {
		n := protocol.MaxProviderChunk
		if n > remaining {
			n = remaining
		}
		if _, perr := sendChunk(t, b, &notifyRecorder{}, id, make([]byte, n), false); perr != nil {
			t.Fatalf("chunk: %v", perr)
		}
		remaining -= n
	}
	rec := &notifyRecorder{}
	_, perr := sendChunk(t, b, rec, id, []byte("overflow"), false)
	if perr == nil || !strings.Contains(perr.Message, "too large") {
		t.Fatalf("got %+v", perr)
	}
}

func TestBrokerRejectsOversizedChunkAndCleansStream(t *testing.T) {
	b := New(defaultCfg(), credsource.NewResolver())
	id := openStream(t, b, "openai-default")
	_, perr := sendChunk(t, b, &notifyRecorder{}, id, make([]byte, protocol.MaxProviderChunk+1), false)
	if perr == nil || !strings.Contains(perr.Message, "chunk too large") {
		t.Fatalf("got %+v", perr)
	}
	_, perr = sendChunk(t, b, &notifyRecorder{}, id, []byte(`{}`), true)
	if perr == nil || !strings.Contains(perr.Message, "unknown provider stream") {
		t.Fatalf("stream was not removed: %+v", perr)
	}
}

func TestBrokerLastStartsProviderExactlyOnce(t *testing.T) {
	srv, authSeen := sseProvider(t, "openai", func(w http.ResponseWriter, auth, body string) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	})
	cfg := defaultCfg()
	cfg.Models[1].BaseURL = srv.URL
	r := credsource.NewResolver()
	r.Register("rot", &rotatingSource{vals: []string{"key"}})
	cfg.Models[1].CredentialEnv = ""
	cfg.Models[1].Credential = &config.CredentialRef{Source: "rot", Name: "openai"}
	b := New(cfg, r).WithHTTPClient(srv.Client())
	rec := &notifyRecorder{}
	id := openStream(t, b, "openai-default")
	body, _ := json.Marshal(protocol.ProviderRequest{Messages: []protocol.ProviderMessage{{Role: "user", Content: "q"}}})
	if _, perr := sendChunk(t, b, rec, id, body, true); perr != nil {
		t.Fatalf("first Last: %v", perr)
	}
	if _, perr := sendChunk(t, b, rec, id, nil, true); perr == nil {
		t.Fatal("repeated Last was accepted")
	}
	rec.waitFor(t, 2*time.Second, func(p protocol.ProviderEventParams) bool { return p.Type == "done" })
	if got := authSeen(); len(got) != 1 {
		t.Fatalf("provider calls %d", len(got))
	}
}

func TestBrokerRejectsDataAfterStart(t *testing.T) {
	srv, _ := sseProvider(t, "openai", func(w http.ResponseWriter, auth, body string) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	})
	cfg := defaultCfg()
	cfg.Models[1].BaseURL = srv.URL
	r := credsource.NewResolver()
	r.Register("rot", &rotatingSource{vals: []string{"key"}})
	cfg.Models[1].CredentialEnv = ""
	cfg.Models[1].Credential = &config.CredentialRef{Source: "rot", Name: "openai"}
	b := New(cfg, r).WithHTTPClient(srv.Client())
	id := openStream(t, b, "openai-default")
	body, _ := json.Marshal(protocol.ProviderRequest{})
	if _, perr := sendChunk(t, b, &notifyRecorder{}, id, body, true); perr != nil {
		t.Fatal(perr)
	}
	if _, perr := sendChunk(t, b, &notifyRecorder{}, id, []byte("more"), false); perr == nil {
		t.Fatal("data after start was accepted")
	}
}

func TestBrokerRequestCountBounds(t *testing.T) {
	b := New(defaultCfg(), credsource.NewResolver())
	id := openStream(t, b, "openai-default")
	req := protocol.ProviderRequest{Messages: make([]protocol.ProviderMessage, protocol.MaxProviderMessages+1)}
	body, _ := json.Marshal(req)
	_, perr := sendChunk(t, b, &notifyRecorder{}, id, body, true)
	if perr == nil || !strings.Contains(perr.Message, "too many provider messages") {
		t.Fatalf("got %+v", perr)
	}
}

func TestBrokerToolCountBounds(t *testing.T) {
	b := New(defaultCfg(), credsource.NewResolver())
	id := openStream(t, b, "openai-default")
	req := protocol.ProviderRequest{Tools: make([]protocol.ProviderToolSchema, protocol.MaxProviderTools+1)}
	body, _ := json.Marshal(req)
	_, perr := sendChunk(t, b, &notifyRecorder{}, id, body, true)
	if perr == nil || !strings.Contains(perr.Message, "too many provider tools") {
		t.Fatalf("got %+v", perr)
	}
}

func TestBrokerRejectsOversizedProviderEvent(t *testing.T) {
	srv, _ := sseProvider(t, "openai", func(w http.ResponseWriter, auth, body string) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"`+strings.Repeat("x", protocol.MaxProviderEvent)+`"}}]}`+"\n\n")
	})
	cfg := defaultCfg()
	cfg.Models[1].BaseURL = srv.URL
	r := credsource.NewResolver()
	r.Register("rot", &rotatingSource{vals: []string{"key"}})
	cfg.Models[1].CredentialEnv = ""
	cfg.Models[1].Credential = &config.CredentialRef{Source: "rot", Name: "openai"}
	b := New(cfg, r).WithHTTPClient(srv.Client())
	rec := &notifyRecorder{}
	id := openStream(t, b, "openai-default")
	body, _ := json.Marshal(protocol.ProviderRequest{})
	if _, perr := sendChunk(t, b, rec, id, body, true); perr != nil {
		t.Fatal(perr)
	}
	ev := rec.waitFor(t, 2*time.Second, func(p protocol.ProviderEventParams) bool { return p.Type == "error" })
	if !strings.Contains(ev.Err, "event too large") {
		t.Fatalf("event %+v", ev)
	}
}

func TestBrokerDrainsProviderAfterNotifyFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	st := &stream{id: "s1", state: streamStarted, ctx: ctx, cancel: cancel}
	b := &Broker{streams: map[string]*stream{"s1": st}}
	events := make(chan provider.Event)
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		defer close(events)
		for i := 0; i < 100; i++ {
			events <- provider.Event{Type: "text", Text: "x"}
		}
	}()
	forwardDone := make(chan struct{})
	go func() {
		b.forwardEvents(st, events, func(string, any) error { return errors.New("connection closed") })
		close(forwardDone)
	}()
	select {
	case <-producerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("provider producer remained blocked after notify failure")
	}
	select {
	case <-forwardDone:
	case <-time.After(2 * time.Second):
		t.Fatal("broker did not finish draining provider events")
	}
}

func TestBrokerOpenContextClosesReceivingStream(t *testing.T) {
	b := New(defaultCfg(), credsource.NewResolver())
	ctx, cancel := context.WithCancel(context.Background())
	res, perr := b.Handle(ctx, "provider_open", mustJSON(t, protocol.ProviderOpenParams{Model: "openai-default"}), nil)
	if perr != nil {
		t.Fatal(perr)
	}
	openRaw, _ := json.Marshal(res)
	var open protocol.ProviderOpenResult
	_ = json.Unmarshal(openRaw, &open)
	cancel()
	deadline := time.Now().Add(time.Second)
	for {
		b.mu.Lock()
		_, exists := b.streams[open.StreamID]
		b.mu.Unlock()
		if !exists {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stream survived its connection context")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestBrokerCredentialResolvedPerCall(t *testing.T) {
	srv, authSeen := sseProvider(t, "openai", func(w http.ResponseWriter, auth, body string) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	})
	cfg := defaultCfg()
	cfg.Models[1].BaseURL = srv.URL
	rot := &rotatingSource{vals: []string{"key-one", "key-two"}}
	r := credsource.NewResolver()
	r.Register("rot", rot)
	cfg.Models[1].CredentialEnv = ""
	cfg.Models[1].Credential = &config.CredentialRef{Source: "rot", Name: "openai"}
	b := New(cfg, r).WithHTTPClient(srv.Client())

	for i := 0; i < 2; i++ {
		rec := &notifyRecorder{}
		id := openStream(t, b, "openai-default")
		raw, _ := json.Marshal(protocol.ProviderRequest{Messages: []protocol.ProviderMessage{{Role: "user", Content: "q"}}})
		if _, perr := sendChunk(t, b, rec, id, raw, true); perr != nil {
			t.Fatalf("send %d: %v", i, perr)
		}
		rec.waitFor(t, 2*time.Second, func(p protocol.ProviderEventParams) bool { return p.Type == "done" })
	}
	if rot.count() != 2 {
		t.Fatalf("resolver calls %d", rot.count())
	}
	got := authSeen()
	if len(got) != 2 {
		t.Fatalf("provider calls %d", len(got))
	}
	if !strings.HasPrefix(got[0], "Bearer key-one") || !strings.HasPrefix(got[1], "Bearer key-two") {
		t.Fatalf("rotated credentials not used per call: %v", got)
	}
}

func TestBrokerMissingCredentialTypedError(t *testing.T) {
	cfg := defaultCfg()
	r := credsource.NewResolver()
	r.Register("rot", &rotatingSource{vals: nil})
	cfg.Models[1].CredentialEnv = ""
	cfg.Models[1].Credential = &config.CredentialRef{Source: "rot", Name: "openai"}
	b := New(cfg, r)
	id := openStream(t, b, "openai-default")
	raw, _ := json.Marshal(protocol.ProviderRequest{})
	_, perr := sendChunk(t, b, &notifyRecorder{}, id, raw, true)
	if perr == nil || !strings.Contains(perr.Message, "credential for model") {
		t.Fatalf("got %+v", perr)
	}
}

func TestBrokerCancelAbortsHTTP(t *testing.T) {
	requestCtx := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"slow\"}}]}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done() // hold the stream open until cancel
		close(requestCtx)
	}))
	t.Cleanup(srv.Close)

	cfg := defaultCfg()
	cfg.Models[1].BaseURL = srv.URL
	r := credsource.NewResolver()
	r.Register("rot", &rotatingSource{vals: []string{"k"}})
	cfg.Models[1].CredentialEnv = ""
	cfg.Models[1].Credential = &config.CredentialRef{Source: "rot", Name: "openai"}
	b := New(cfg, r).WithHTTPClient(srv.Client())

	rec := &notifyRecorder{}
	id := openStream(t, b, "openai-default")
	raw, _ := json.Marshal(protocol.ProviderRequest{Messages: []protocol.ProviderMessage{{Role: "user", Content: "q"}}})
	if _, perr := sendChunk(t, b, rec, id, raw, true); perr != nil {
		t.Fatalf("send: %v", perr)
	}
	rec.waitFor(t, 2*time.Second, func(p protocol.ProviderEventParams) bool { return p.Text == "slow" })
	if _, perr := b.Handle(context.Background(), "provider_cancel", mustJSON(t, protocol.ProviderCancelParams{StreamID: id}), nil); perr != nil {
		t.Fatalf("cancel: %v", perr)
	}
	select {
	case <-requestCtx:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not abort the provider HTTP request")
	}
	if _, perr := b.Handle(context.Background(), "provider_cancel", mustJSON(t, protocol.ProviderCancelParams{StreamID: id}), nil); perr != nil {
		t.Fatalf("second cancel: %v", perr)
	}
}

func TestBrokerToolArgsBound(t *testing.T) {
	cfg := defaultCfg()
	r := credsource.NewResolver()
	b := New(cfg, r)
	id := openStream(t, b, "openai-default")
	req := protocol.ProviderRequest{Messages: []protocol.ProviderMessage{{
		Role: "assistant", ToolName: "x", ToolArgs: strings.Repeat("a", protocol.MaxProviderToolArgs+1),
	}}}
	raw, _ := json.Marshal(req)
	var perr *protocol.Error
	for off := 0; off < len(raw); off += protocol.MaxProviderChunk {
		end := min(off+protocol.MaxProviderChunk, len(raw))
		_, perr = sendChunk(t, b, &notifyRecorder{}, id, raw[off:end], end == len(raw))
		if perr != nil {
			break
		}
	}
	if perr == nil || !strings.Contains(perr.Message, "tool args too large") {
		t.Fatalf("got %+v", perr)
	}
}

func TestBrokerUnknownMethodTypedError(t *testing.T) {
	b := New(defaultCfg(), credsource.NewResolver())
	_, perr := b.Handle(context.Background(), "fetch_url", []byte(`{}`), nil)
	if perr == nil || !strings.Contains(perr.Message, "unknown broker method") {
		t.Fatalf("got %+v", perr)
	}
}

func TestBrokerMaxConcurrentStreams(t *testing.T) {
	cfg := defaultCfg()
	b := New(cfg, credsource.NewResolver())
	first := openStream(t, b, "openai-default")
	second := openStream(t, b, "grok-default")
	_, perr := b.Handle(context.Background(), "provider_open", mustJSON(t, providerOpenJSON("claude-default")), nil)
	if perr == nil || !strings.Contains(perr.Message, "too many open provider streams") {
		t.Fatalf("got %+v", perr)
	}
	_ = first
	_ = second
}

var _ = errors.New
var _ = fmt.Sprintf
