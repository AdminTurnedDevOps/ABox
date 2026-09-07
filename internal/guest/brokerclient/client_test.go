package brokerclient

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/provider"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

type hostStub struct {
	t       *testing.T
	conn    net.Conn
	writeMu sync.Mutex

	mu       sync.Mutex
	requests []json.RawMessage // reassembled requests
	lastSeen []string
	openReqs []protocol.ProviderOpenParams

	cancelHook func(streamID string)
	sendErr    *protocol.Error
}

func (h *hostStub) write(f protocol.Frame) error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	return protocol.WriteFrame(h.conn, f)
}

func (h *hostStub) serve() {
	var currentStream string
	var reassembled []byte
	for {
		frame, err := protocol.ReadFrame(h.conn)
		if err != nil {
			return
		}
		switch frame.Method {
		case "provider_open":
			p, _ := protocol.DecodeParams[protocol.ProviderOpenParams](frame.Params)
			h.mu.Lock()
			h.openReqs = append(h.openReqs, p)
			h.mu.Unlock()
			currentStream = "s1"
			res, _ := protocol.EncodeParams(protocol.ProviderOpenResult{StreamID: currentStream})
			_ = h.write(protocol.Frame{ID: frame.ID, Result: res})
		case "provider_send":
			p, _ := protocol.DecodeParams[protocol.ProviderSendParams](frame.Params)
			h.mu.Lock()
			sendErr := h.sendErr
			h.mu.Unlock()
			if sendErr != nil {
				_ = h.write(protocol.Frame{ID: frame.ID, Error: sendErr})
				continue
			}
			reassembled = append(reassembled, p.Data...)
			if p.Last {
				h.mu.Lock()
				h.lastSeen = append(h.lastSeen, "last")
				h.requests = append(h.requests, append([]byte(nil), reassembled...))
				h.mu.Unlock()
				reassembled = nil
			}
			ok, _ := protocol.EncodeParams(map[string]bool{"ok": true})
			_ = h.write(protocol.Frame{ID: frame.ID, Result: ok})
		case "provider_cancel":
			p, _ := protocol.DecodeParams[protocol.ProviderCancelParams](frame.Params)
			h.mu.Lock()
			if h.cancelHook != nil {
				h.cancelHook(p.StreamID)
			}
			h.mu.Unlock()
			ok, _ := protocol.EncodeParams(map[string]bool{"ok": true})
			_ = h.write(protocol.Frame{ID: frame.ID, Result: ok})
		}
	}
}

func newClientPair(t *testing.T, hostProto int) (*Client, *hostStub) {
	t.Helper()
	host, guest := net.Pipe()
	t.Cleanup(func() { host.Close(); guest.Close() })
	stub := &hostStub{t: t, conn: host}
	go stub.serve()
	c := New()
	c.SetHostProtocol(hostProto)
	c.Attach(func(f protocol.Frame) error {
		return protocol.WriteFrame(guest, f)
	})
	go func() {
		for {
			frame, err := protocol.ReadFrame(guest)
			if err != nil {
				return
			}
			c.HandleFrame(frame)
		}
	}()
	return c, stub
}

func TestStreamChunkingAndReassembly(t *testing.T) {
	c, stub := newClientPair(t, 3)
	big := strings.Repeat("x", protocol.MaxProviderChunk+512) // forces two chunks
	msgs := []provider.Message{{Role: "user", Content: big}}
	tools := []provider.ToolSchema{{Name: "list_files", Description: "d", Parameters: map[string]any{"type": "object"}}}

	events, err := c.Stream(context.Background(), config.Model{Name: "grok-default", Provider: "xai"}, msgs, tools)
	if err != nil {
		t.Fatal(err)
	}
	pushText(t, stub, "s1", "hello ")
	pushText(t, stub, "s1", "world")
	pushDone(t, stub, "s1")

	var text string
	for ev := range events {
		if ev.Type == "text" {
			text += ev.Text
		}
	}
	if text != "hello world" {
		t.Fatalf("text %q", text)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.requests) != 1 {
		t.Fatalf("reassembled requests %d", len(stub.requests))
	}
	var req protocol.ProviderRequest
	if err := json.Unmarshal(stub.requests[0], &req); err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 || req.Messages[0].Content != big {
		t.Fatalf("message lost in chunking")
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "list_files" {
		t.Fatalf("tools lost in chunking")
	}
	if len(stub.openReqs) != 1 || stub.openReqs[0].Model != "grok-default" {
		t.Fatalf("open params %+v", stub.openReqs)
	}
}

func TestStreamWithUsageForwardsRichOpen(t *testing.T) {
	c, stub := newClientPair(t, 3)
	events, err := c.StreamWithUsage(context.Background(), config.Model{Name: "grok-default"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	pushDone(t, stub, "s1")
	for range events {
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.openReqs) != 1 || !stub.openReqs[0].Rich {
		t.Fatalf("open params %+v", stub.openReqs)
	}
}

func TestStreamDoesNotDropBufferedEvents(t *testing.T) {
	c, stub := newClientPair(t, 3)
	events, err := c.Stream(context.Background(), config.Model{Name: "grok-default"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	const count = 200
	for i := 0; i < count; i++ {
		pushText(t, stub, "s1", "x")
	}
	pushDone(t, stub, "s1")
	got := 0
	for ev := range events {
		if ev.Type == "text" {
			got++
		}
	}
	if got != count {
		t.Fatalf("got %d events, want %d", got, count)
	}
}

func TestClientStreamQueueBudgets(t *testing.T) {
	s := &clientStream{notify: make(chan struct{}, 1)}
	for i := 0; i < eventQueueMaxCount; i++ {
		if err := s.enqueue(provider.Event{Type: "text"}, false, 1); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	if err := s.enqueue(provider.Event{Type: "text"}, false, 1); !errors.Is(err, ErrEventQueueOverflow) {
		t.Fatalf("count overflow: %v", err)
	}

	s = &clientStream{notify: make(chan struct{}, 1)}
	for i := 0; i < eventQueueMaxBytes/protocol.MaxProviderEvent; i++ {
		if err := s.enqueue(provider.Event{Type: "text"}, false, protocol.MaxProviderEvent); err != nil {
			t.Fatalf("byte enqueue %d: %v", i, err)
		}
	}
	if err := s.enqueue(provider.Event{Type: "text"}, false, 1); !errors.Is(err, ErrEventQueueOverflow) {
		t.Fatalf("byte overflow: %v", err)
	}
}

func TestStreamQueueOverflowFailsAndCancelsHost(t *testing.T) {
	c, stub := newClientPair(t, 3)
	canceled := make(chan string, 1)
	stub.mu.Lock()
	stub.cancelHook = func(id string) { canceled <- id }
	stub.mu.Unlock()
	events, err := c.Stream(context.Background(), config.Model{Name: "grok-default"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < eventQueueMaxCount+2; i++ {
		p, _ := json.Marshal(protocol.ProviderEventParams{StreamID: "s1", Type: "text", Text: "x"})
		c.HandleFrame(protocol.Frame{Method: "provider_event", Params: p})
	}

	var last provider.Event
	for ev := range events {
		last = ev
	}
	if last.Type != "error" || !errors.Is(last.Err, ErrEventQueueOverflow) {
		t.Fatalf("terminal event %+v", last)
	}
	select {
	case id := <-canceled:
		if id != "s1" {
			t.Fatalf("canceled stream %q", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("overflow did not cancel host stream")
	}
}

func TestStreamRequiresHostProtocol3(t *testing.T) {
	c, _ := newClientPair(t, 2)
	_, err := c.Stream(context.Background(), config.Model{Name: "g"}, nil, nil)
	if !errors.Is(err, ErrHostTooOld) {
		t.Fatalf("got %v", err)
	}
}

func TestStreamCancelForwardsProviderCancel(t *testing.T) {
	c, stub := newClientPair(t, 3)
	ctx, cancel := context.WithCancel(context.Background())
	canceled := make(chan struct{}, 1)
	stub.cancelHook = func(string) { canceled <- struct{}{} }

	events, err := c.Stream(ctx, config.Model{Name: "grok-default"}, []provider.Message{{Role: "user", Content: "q"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pushText(t, stub, "s1", "partial")
	cancel()
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("provider_cancel never forwarded")
	}
	closed := make(chan struct{})
	go func() {
		for range events {
		}
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("local stream did not close on cancellation")
	}
}

func TestStreamCancelClosesLocallyWhenHostWriteFails(t *testing.T) {
	c, _ := newClientPair(t, 3)
	ctx, cancel := context.WithCancel(context.Background())
	events, err := c.Stream(ctx, config.Model{Name: "grok-default"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Attach(func(protocol.Frame) error { return errors.New("host unavailable") })
	cancel()

	closed := make(chan struct{})
	go func() {
		for range events {
		}
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("local stream remained open after cancellation")
	}
}

func TestCloseIsIdempotentAndClosesStreams(t *testing.T) {
	c, _ := newClientPair(t, 3)
	events, err := c.Stream(context.Background(), config.Model{Name: "grok-default"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Close(errors.New("disconnected"))
	c.Close(errors.New("again"))
	select {
	case ev, ok := <-events:
		if !ok || ev.Type != "error" || ev.Err == nil || !strings.Contains(ev.Err.Error(), "disconnected") {
			t.Fatalf("terminal event %+v, open=%v", ev, ok)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("connection error was not delivered")
	}
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("event channel remained open after terminal error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event channel did not close")
	}
}

func TestClosePreservesQueuedEvents(t *testing.T) {
	c, _ := newClientPair(t, 3)
	events, err := c.Stream(context.Background(), config.Model{Name: "grok-default"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := json.Marshal(protocol.ProviderEventParams{StreamID: "s1", Type: "text", Text: "before disconnect"})
	c.HandleFrame(protocol.Frame{Method: "provider_event", Params: p})
	c.Close(errors.New("disconnected"))

	var got []provider.Event
	for ev := range events {
		got = append(got, ev)
	}
	if len(got) != 2 || got[0].Text != "before disconnect" || got[1].Type != "error" || got[1].Err == nil {
		t.Fatalf("events %+v", got)
	}
}

func TestStreamHostOpenErrorPropagates(t *testing.T) {
	host, guest := net.Pipe()
	t.Cleanup(func() { host.Close(); guest.Close() })
	go func() {
		frame, err := protocol.ReadFrame(host)
		if err != nil {
			return
		}
		_ = protocol.WriteFrame(host, protocol.Frame{
			ID: frame.ID, Error: &protocol.Error{Code: "host", Message: "unknown model profile \"nope\""},
		})
	}()
	c := New()
	c.SetHostProtocol(3)
	c.Attach(func(f protocol.Frame) error { return protocol.WriteFrame(guest, f) })
	go func() {
		for {
			frame, err := protocol.ReadFrame(guest)
			if err != nil {
				return
			}
			c.HandleFrame(frame)
		}
	}()
	_, err := c.Stream(context.Background(), config.Model{Name: "nope"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown model profile") {
		t.Fatalf("got %v", err)
	}
}

func TestCanceledOpenCancelsLateHostStream(t *testing.T) {
	host, guest := net.Pipe()
	t.Cleanup(func() { host.Close(); guest.Close() })

	c := New()
	c.SetHostProtocol(3)
	c.Attach(func(f protocol.Frame) error {
		return protocol.WriteFrame(guest, f)
	})
	go func() {
		for {
			frame, err := protocol.ReadFrame(guest)
			if err != nil {
				return
			}
			c.HandleFrame(frame)
		}
	}()

	openSeen := make(chan struct{})
	releaseOpen := make(chan struct{})
	canceled := make(chan string, 1)
	go func() {
		for {
			frame, err := protocol.ReadFrame(host)
			if err != nil {
				return
			}
			switch frame.Method {
			case "provider_open":
				close(openSeen)
				<-releaseOpen
				res, _ := protocol.EncodeParams(protocol.ProviderOpenResult{StreamID: "s-late"})
				_ = protocol.WriteFrame(host, protocol.Frame{ID: frame.ID, Result: res})
			case "provider_cancel":
				p, _ := protocol.DecodeParams[protocol.ProviderCancelParams](frame.Params)
				canceled <- p.StreamID
				ok, _ := protocol.EncodeParams(map[string]bool{"ok": true})
				_ = protocol.WriteFrame(host, protocol.Frame{ID: frame.ID, Result: ok})
			default:
				ok, _ := protocol.EncodeParams(map[string]bool{"ok": true})
				_ = protocol.WriteFrame(host, protocol.Frame{ID: frame.ID, Result: ok})
			}
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c.Stream(ctx, config.Model{Name: "g"}, nil, nil)
		done <- err
	}()
	select {
	case <-openSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("provider_open was not written")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled open did not return")
	}
	c.mu.Lock()
	pending := len(c.pending)
	c.mu.Unlock()
	if pending != 1 {
		t.Fatalf("canceled open tombstones = %d, want 1", pending)
	}
	close(releaseOpen)
	select {
	case id := <-canceled:
		if id != "s-late" {
			t.Fatalf("canceled stream %q", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("late provider_open was not canceled")
	}
}

func TestCanceledCallTombstoneClearedOnConnectionClose(t *testing.T) {
	c := New()
	c.AttachContext(func(context.Context, protocol.Frame) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.call(ctx, "provider_open", protocol.ProviderOpenParams{Model: "g"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	c.mu.Lock()
	pending := len(c.pending)
	c.mu.Unlock()
	if pending != 1 {
		t.Fatalf("canceled open tombstones = %d, want 1", pending)
	}

	c.Close(errors.New("disconnected"))
	c.mu.Lock()
	pending = len(c.pending)
	c.mu.Unlock()
	if pending != 0 {
		t.Fatalf("connection close retained %d tombstones", pending)
	}
}

func TestLateOpenCancelIsIssuedOutsideClientLock(t *testing.T) {
	c := New()
	lockFree := make(chan bool, 1)
	c.AttachContext(func(_ context.Context, frame protocol.Frame) error {
		if frame.Method != "provider_cancel" {
			return nil
		}
		acquired := c.mu.TryLock()
		lockFree <- acquired
		if !acquired {
			return errors.New("provider_cancel issued while client lock held")
		}
		c.mu.Unlock()
		result, _ := protocol.EncodeParams(map[string]bool{"ok": true})
		c.HandleFrame(protocol.Frame{ID: frame.ID, Result: result})
		return nil
	})
	result, _ := protocol.EncodeParams(protocol.ProviderOpenResult{StreamID: "s-late"})
	done := make(chan struct{})
	go func() {
		c.cancelOpenedFromFrame(protocol.Frame{Result: result})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("provider_cancel deadlocked on client lock")
	}
	if !<-lockFree {
		t.Fatal("provider_cancel was issued while client lock held")
	}
}

func TestStreamSendErrorCancelsOpenedHostStream(t *testing.T) {
	c, stub := newClientPair(t, 3)
	canceled := make(chan string, 1)
	stub.mu.Lock()
	stub.sendErr = &protocol.Error{Code: "host", Message: "send rejected"}
	stub.cancelHook = func(id string) { canceled <- id }
	stub.mu.Unlock()
	_, err := c.Stream(context.Background(), config.Model{Name: "grok-default"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "send rejected") {
		t.Fatalf("got %v", err)
	}
	select {
	case id := <-canceled:
		if id != "s1" {
			t.Fatalf("canceled stream %q", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("opened host stream was not canceled")
	}
}

func TestCloseWakesPendingCall(t *testing.T) {
	c := New()
	written := make(chan struct{})
	c.Attach(func(protocol.Frame) error {
		close(written)
		return nil
	})
	done := make(chan protocol.Frame, 1)
	go func() {
		frame, _ := c.call(context.Background(), "provider_open", protocol.ProviderOpenParams{Model: "g"})
		done <- frame
	}()
	<-written
	c.Close(errors.New("disconnected"))
	select {
	case frame := <-done:
		if frame.Error == nil || frame.Error.Code != "connection" {
			t.Fatalf("frame %+v", frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending call was not woken")
	}
}

func TestCallPassesContextToBlockedWriter(t *testing.T) {
	c := New()
	c.AttachContext(func(ctx context.Context, _ protocol.Frame) error {
		<-ctx.Done()
		return ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := c.call(ctx, "provider_cancel", protocol.ProviderCancelParams{StreamID: "s1"})
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked writer ignored context")
	}
}

func pushText(t *testing.T, stub *hostStub, streamID, text string) {
	t.Helper()
	p, _ := json.Marshal(protocol.ProviderEventParams{StreamID: streamID, Type: "text", Text: text})
	if err := stub.write(protocol.Frame{V: protocol.Version, ID: "h-push", Method: "provider_event", Params: p}); err != nil {
		t.Fatal(err)
	}
}

func pushDone(t *testing.T, stub *hostStub, streamID string) {
	t.Helper()
	p, _ := json.Marshal(protocol.ProviderEventParams{StreamID: streamID, Type: "done"})
	if err := stub.write(protocol.Frame{V: protocol.Version, ID: "h-push", Method: "provider_event", Params: p}); err != nil {
		t.Fatal(err)
	}
}
