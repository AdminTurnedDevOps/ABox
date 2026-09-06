// Package brokerclient is the guest-side client for the host provider broker.
// No credential, base URL, or header exists in the guest.
package brokerclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/provider"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

// ErrHostTooOld: an old host silently drops guest-initiated frames, so a
// proto-3 guest would hang without this check.
var ErrHostTooOld = errors.New("host binary too old; run make build")

var ErrEventQueueOverflow = errors.New("provider event queue budget exceeded")

const (
	eventQueueMaxCount = 256
	eventQueueMaxBytes = 4 << 20
	canceledCallGrace  = 5 * time.Second
)

type Client struct {
	writeMu sync.Mutex
	write   func(context.Context, protocol.Frame) error

	mu        sync.Mutex
	nextID    int
	hostProto int
	closedErr error
	pending   map[string]*pendingCall
	streams   map[string]*clientStream
}

type pendingCall struct {
	once  sync.Once
	ready chan struct{}
	frame protocol.Frame
}

func newPendingCall() *pendingCall {
	return &pendingCall{ready: make(chan struct{})}
}

func (p *pendingCall) complete(frame protocol.Frame) {
	p.once.Do(func() {
		p.frame = frame
		close(p.ready)
	})
}

type clientStream struct {
	out     chan provider.Event
	notify  chan struct{}
	abort   chan struct{}
	settled chan struct{}

	mu          sync.Mutex
	queue       []queuedEvent
	queueBytes  int
	terminalErr error
	sealed      bool
	abortOnce   sync.Once
	settledOnce sync.Once
}

type queuedEvent struct {
	event provider.Event
	size  int
}

func newClientStream() *clientStream {
	s := &clientStream{
		out: make(chan provider.Event), notify: make(chan struct{}, 1),
		abort: make(chan struct{}), settled: make(chan struct{}),
	}
	go s.pump()
	return s
}

func (s *clientStream) enqueue(ev provider.Event, terminal bool, size int) error {
	s.mu.Lock()
	if s.sealed {
		s.mu.Unlock()
		return errStreamSealed
	}
	if size < 0 || size > protocol.MaxProviderEvent || len(s.queue) >= eventQueueMaxCount || s.queueBytes+size > eventQueueMaxBytes {
		s.mu.Unlock()
		return ErrEventQueueOverflow
	}
	s.queue = append(s.queue, queuedEvent{event: ev, size: size})
	s.queueBytes += size
	if terminal {
		s.sealed = true
	}
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
	return nil
}

var errStreamSealed = errors.New("provider stream sealed")

func (s *clientStream) abortNow() {
	s.abortOnce.Do(func() {
		s.mu.Lock()
		s.queue = nil
		s.queueBytes = 0
		s.terminalErr = nil
		s.sealed = true
		s.mu.Unlock()
		close(s.abort)
	})
}

func (s *clientStream) fail(err error) {
	if err == nil {
		err = errors.New("broker connection closed")
	}
	s.mu.Lock()
	if s.sealed {
		s.mu.Unlock()
		return
	}
	s.sealed = true
	s.terminalErr = err
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func (s *clientStream) pump() {
	defer func() {
		close(s.out)
		s.settledOnce.Do(func() { close(s.settled) })
	}()
	for {
		s.mu.Lock()
		if len(s.queue) > 0 {
			item := s.queue[0]
			s.queue[0] = queuedEvent{}
			s.queue = s.queue[1:]
			s.queueBytes -= item.size
			s.mu.Unlock()
			select {
			case s.out <- item.event:
			case <-s.abort:
				return
			}
			continue
		}
		if s.terminalErr != nil {
			err := s.terminalErr
			s.terminalErr = nil
			s.mu.Unlock()
			select {
			case s.out <- provider.Event{Type: "error", Err: err}:
			case <-s.abort:
				return
			}
			continue
		}
		sealed := s.sealed
		s.mu.Unlock()
		if sealed {
			return
		}
		select {
		case <-s.notify:
		case <-s.abort:
			return
		}
	}
}

func New() *Client {
	return &Client{
		pending: map[string]*pendingCall{},
		streams: map[string]*clientStream{},
	}
}

func (c *Client) Attach(write func(protocol.Frame) error) {
	if write == nil {
		c.AttachContext(nil)
		return
	}
	c.AttachContext(func(_ context.Context, frame protocol.Frame) error { return write(frame) })
}

func (c *Client) AttachContext(write func(context.Context, protocol.Frame) error) {
	c.writeMu.Lock()
	c.write = write
	c.writeMu.Unlock()
}

func (c *Client) SetHostProtocol(p int) {
	c.mu.Lock()
	c.hostProto = p
	c.mu.Unlock()
}

func (c *Client) hostProtocol() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hostProto
}

func (c *Client) HandleFrame(f protocol.Frame) bool {
	if f.Method == "provider_event" {
		c.dispatchEvent(f)
		return true
	}
	if f.Method == "" && strings.HasPrefix(f.ID, "g-") {
		c.mu.Lock()
		call := c.pending[f.ID]
		if call != nil {
			call.complete(f)
		}
		c.mu.Unlock()
		return true
	}
	return false
}

func (c *Client) call(ctx context.Context, method string, params any) (protocol.Frame, error) {
	c.writeMu.Lock()
	write := c.write
	c.writeMu.Unlock()
	if write == nil {
		return protocol.Frame{}, fmt.Errorf("broker client not attached")
	}
	raw, err := protocol.EncodeParams(params)
	if err != nil {
		return protocol.Frame{}, err
	}
	c.mu.Lock()
	if c.closedErr != nil {
		err := c.closedErr
		c.mu.Unlock()
		return protocol.Frame{}, err
	}
	c.nextID++
	id := fmt.Sprintf("g-%d", c.nextID)
	call := newPendingCall()
	c.pending[id] = call
	c.mu.Unlock()
	forget := func() {
		c.mu.Lock()
		if c.pending[id] == call {
			delete(c.pending, id)
		}
		c.mu.Unlock()
	}
	if err := write(ctx, protocol.Frame{V: protocol.Version, ID: id, Method: method, Params: raw}); err != nil {
		forget()
		c.Close(err)
		return protocol.Frame{}, err
	}
	select {
	case <-ctx.Done():
		select {
		case <-call.ready:
			forget()
			return call.frame, nil
		default:
			go c.reapCanceledCall(id, call, method)
			return protocol.Frame{}, ctx.Err()
		}
	case <-call.ready:
		forget()
		return call.frame, nil
	}
}

// A canceled provider_open can still succeed on the host; cancel that stream
// so it does not occupy a slot until idle timeout.
func (c *Client) reapCanceledCall(id string, call *pendingCall, method string) {
	timer := time.NewTimer(canceledCallGrace)
	defer timer.Stop()
	select {
	case <-call.ready:
	case <-timer.C:
		c.forgetPending(id, call)
		return
	}
	c.forgetPending(id, call)
	if method == "provider_open" {
		c.cancelOpenedFromFrame(call.frame)
	}
}

func (c *Client) forgetPending(id string, call *pendingCall) {
	c.mu.Lock()
	if c.pending[id] == call {
		delete(c.pending, id)
	}
	c.mu.Unlock()
}

func (c *Client) cancelOpenedFromFrame(frame protocol.Frame) {
	if frame.Error != nil {
		return
	}
	var openRes protocol.ProviderOpenResult
	if json.Unmarshal(frame.Result, &openRes) != nil || openRes.StreamID == "" {
		return
	}
	c.mu.Lock()
	closed := c.closedErr != nil
	c.mu.Unlock()
	if closed {
		return
	}
	c.cancelHost(openRes.StreamID)
}

func (c *Client) Stream(ctx context.Context, model config.Model, messages []provider.Message, tools []provider.ToolSchema) (<-chan provider.Event, error) {
	return c.stream(ctx, model, messages, tools, false)
}

func (c *Client) StreamWithUsage(ctx context.Context, model config.Model, messages []provider.Message, tools []provider.ToolSchema) (<-chan provider.Event, error) {
	return c.stream(ctx, model, messages, tools, true)
}

func (c *Client) stream(ctx context.Context, model config.Model, messages []provider.Message, tools []provider.ToolSchema, rich bool) (<-chan provider.Event, error) {
	if c.hostProtocol() < 3 {
		return nil, fmt.Errorf("%w (host speaks protocol %d)", ErrHostTooOld, c.hostProtocol())
	}
	openCtx, openCancel := context.WithTimeout(ctx, time.Minute)
	defer openCancel()

	open, err := c.call(openCtx, "provider_open", protocol.ProviderOpenParams{Model: model.Name, Rich: rich})
	if err != nil {
		return nil, fmt.Errorf("provider_open: %w", err)
	}
	if open.Error != nil {
		return nil, open.Error
	}
	var openRes protocol.ProviderOpenResult
	decodeErr := json.Unmarshal(open.Result, &openRes)
	if decodeErr != nil || openRes.StreamID == "" {
		if openRes.StreamID != "" {
			go c.cancelHost(openRes.StreamID)
		}
		return nil, fmt.Errorf("provider_open: malformed result")
	}
	streamID := openRes.StreamID
	cancelOpened := func() {
		go c.cancelHost(streamID)
	}

	req := protocol.ProviderRequest{
		Messages: make([]protocol.ProviderMessage, len(messages)),
	}
	for i, m := range messages {
		req.Messages[i] = protocol.ProviderMessage{
			Role: m.Role, Content: m.Content, ToolID: m.ToolID,
			ToolName: m.ToolName, ToolArgs: m.ToolArgs, ToolResult: m.ToolResult,
		}
	}
	for _, t := range tools {
		req.Tools = append(req.Tools, protocol.ProviderToolSchema{
			Name: t.Name, Description: t.Description, Parameters: t.Parameters,
		})
	}
	body, err := json.Marshal(req)
	if err != nil {
		cancelOpened()
		return nil, err
	}
	if len(body) > protocol.MaxProviderRequest {
		cancelOpened()
		return nil, fmt.Errorf("provider request too large")
	}

	// Host may start streaming as soon as it processes the Last chunk.
	stream := newClientStream()
	c.mu.Lock()
	if c.closedErr != nil {
		err := c.closedErr
		c.mu.Unlock()
		stream.abortNow()
		cancelOpened()
		return nil, err
	}
	c.streams[streamID] = stream
	c.mu.Unlock()

	const chunk = protocol.MaxProviderChunk
	for off := 0; off < len(body); off += chunk {
		end := off + chunk
		if end > len(body) {
			end = len(body)
		}
		sendCtx, cancelSend := context.WithTimeout(ctx, time.Minute)
		resp, err := c.call(sendCtx, "provider_send", protocol.ProviderSendParams{
			StreamID: streamID, Data: body[off:end], Last: end == len(body),
		})
		cancelSend()
		if err != nil {
			c.abortStream(streamID, stream)
			cancelOpened()
			return nil, fmt.Errorf("provider_send: %w", err)
		}
		if resp.Error != nil {
			c.abortStream(streamID, stream)
			cancelOpened()
			return nil, resp.Error
		}
	}

	// Also wait on settled so this goroutine does not leak when ctx never cancels.
	go func() {
		select {
		case <-ctx.Done():
			c.abortStream(streamID, stream)
			c.cancelHost(streamID)
		case <-stream.settled:
		}
	}()
	return stream.out, nil
}

func (c *Client) abortStream(id string, stream *clientStream) {
	c.mu.Lock()
	if c.streams[id] == stream {
		delete(c.streams, id)
	}
	c.mu.Unlock()
	stream.abortNow()
}

func (c *Client) cancelHost(streamID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = c.call(ctx, "provider_cancel", protocol.ProviderCancelParams{StreamID: streamID})
}

func (c *Client) dispatchEvent(f protocol.Frame) {
	var p protocol.ProviderEventParams
	if err := json.Unmarshal(f.Params, &p); err != nil {
		c.Close(fmt.Errorf("malformed provider event: %w", err))
		return
	}
	ev := provider.Event{
		Type: p.Type, Text: p.Text,
		ToolID: p.ToolID, ToolName: p.ToolName, ToolArgs: p.ToolArgs,
		Usage: p.Usage, StopReason: p.StopReason,
	}
	if p.Err != "" {
		ev.Err = errors.New(p.Err)
	} else if p.Type == "error" {
		ev.Err = errors.New("provider error")
	}
	terminal := p.Type == "done" || p.Type == "error"
	c.mu.Lock()
	stream := c.streams[p.StreamID]
	if stream == nil {
		c.mu.Unlock()
		return
	}
	if err := stream.enqueue(ev, terminal, len(f.Params)); err != nil {
		if errors.Is(err, ErrEventQueueOverflow) && c.streams[p.StreamID] == stream {
			delete(c.streams, p.StreamID)
			c.mu.Unlock()
			stream.fail(ErrEventQueueOverflow)
			go c.cancelHost(p.StreamID)
			return
		}
		c.mu.Unlock()
		return
	}
	if terminal {
		if c.streams[p.StreamID] == stream {
			delete(c.streams, p.StreamID)
		}
	}
	c.mu.Unlock()
}

func (c *Client) Close(err error) {
	if err == nil {
		err = errors.New("broker connection closed")
	}
	c.mu.Lock()
	if c.closedErr != nil {
		c.mu.Unlock()
		return
	}
	c.closedErr = err
	pending := c.pending
	streams := c.streams
	c.pending = map[string]*pendingCall{}
	c.streams = map[string]*clientStream{}
	c.mu.Unlock()
	for _, call := range pending {
		call.complete(protocol.Frame{Error: &protocol.Error{Code: "connection", Message: err.Error()}})
	}
	for _, stream := range streams {
		stream.fail(err)
	}
}
