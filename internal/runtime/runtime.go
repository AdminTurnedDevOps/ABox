package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/session"
	"github.com/AdminTurnedDevOps/ABox/internal/vmmconfig"
	"github.com/AdminTurnedDevOps/ABox/protocol"
	"golang.org/x/sys/unix"
)

// ErrGuestTooOld is returned when a v2-only operation is used against a v1 guest.
var ErrGuestTooOld = errors.New("guest protocol too old")

const cancelResponseTimeout = 5 * time.Second

const (
	callQueueFrames = 1
	callQueueBytes  = protocol.MaxFrameBytes
	turnQueueFrames = 256
	turnQueueBytes  = 8 << 20
	writeTimeout    = 30 * time.Second
	shutdownTimeout = 3 * time.Second
)

type TurnOptions struct {
	MaxTurns   int
	TimeoutSec int
	RichEvents bool
}

func (o TurnOptions) needsV2() bool {
	return o.MaxTurns > 0 || o.TimeoutSec > 0 || o.RichEvents
}

type TurnOutcome struct {
	Canceled   bool
	Usage      *protocol.UsageInfo
	StopReason string
}

// Handle must not block the connection read loop; stream via notify instead.
type GuestCallHandler interface {
	Handle(ctx context.Context, method string, params json.RawMessage,
		notify func(method string, params any) error) (any, *protocol.Error)
}

type Sandbox struct {
	Sess          *session.Session
	History       []protocol.HistoryLine
	GuestProtocol int
	cmd           *exec.Cmd
	conn          net.Conn
	OnGuestCall   GuestCallHandler

	writeOnce sync.Once
	writeGate chan struct{}
	turnMu    sync.Mutex

	mu         sync.Mutex
	nextID     int
	calls      map[string]*frameQueue
	activeTurn string
	turnQ      *frameQueue
	lifeCtx    context.Context
	lifeCancel context.CancelFunc
	guestSlots chan struct{}
	cancelSlot chan struct{}
	busyQueue  chan protocol.Frame

	readOnce sync.Once
	failOnce sync.Once
	readDone chan struct{}
}

var (
	errQueueClosed = errors.New("frame queue closed")
	errQueueFull   = errors.New("frame queue budget exceeded")
)

// frameQueue gives the connection read loop bounded, nonblocking delivery.
// Exceeding either budget fails the owning call/turn instead of silently
// dropping a response or blocking cancellation traffic.
type frameQueue struct {
	mu     sync.Mutex
	frames []queuedFrame
	bytes  int
	maxN   int
	maxB   int
	wake   chan struct{}
	closed bool
	err    error
}

type queuedFrame struct {
	frame protocol.Frame
	size  int
}

func newFrameQueue(maxFrames, maxBytes int) *frameQueue {
	return &frameQueue{maxN: maxFrames, maxB: maxBytes, wake: make(chan struct{}, 1)}
}

func (q *frameQueue) push(frame protocol.Frame) error {
	raw, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return errQueueClosed
	}
	if len(q.frames) >= q.maxN || q.bytes+len(raw) > q.maxB {
		q.mu.Unlock()
		return errQueueFull
	}
	q.frames = append(q.frames, queuedFrame{frame: frame, size: len(raw)})
	q.bytes += len(raw)
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return nil
}

func (q *frameQueue) abort(err error) {
	q.mu.Lock()
	if !q.closed {
		q.frames = nil
		q.bytes = 0
		q.closed = true
		q.err = err
	}
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *frameQueue) close(err error) {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		q.err = err
	}
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *frameQueue) pop(ctx context.Context) (protocol.Frame, bool, error) {
	for {
		q.mu.Lock()
		if len(q.frames) > 0 {
			item := q.frames[0]
			q.frames[0] = queuedFrame{}
			q.frames = q.frames[1:]
			q.bytes -= item.size
			q.mu.Unlock()
			return item.frame, true, nil
		}
		if q.closed {
			err := q.err
			q.mu.Unlock()
			return protocol.Frame{}, false, err
		}
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			// Prefer a frame that became ready with the cancellation signal.
			// This avoids racing a real terminal response at the deadline.
			q.mu.Lock()
			if len(q.frames) > 0 {
				item := q.frames[0]
				q.frames[0] = queuedFrame{}
				q.frames = q.frames[1:]
				q.bytes -= item.size
				q.mu.Unlock()
				return item.frame, true, nil
			}
			if q.closed {
				err := q.err
				q.mu.Unlock()
				return protocol.Frame{}, false, err
			}
			q.mu.Unlock()
			return protocol.Frame{}, false, ctx.Err()
		case <-q.wake:
		}
	}
}

func Prepare(sess *session.Session, imagePath string, model config.Model, mcpServers []config.MCPServer, resume bool) error {
	if imagePath == "" {
		imagePath = config.GuestImagePath()
	}
	if resume {
		if _, err := os.Stat(sess.RootDisk()); err != nil {
			return fmt.Errorf("resume: session disk missing at %s", sess.RootDisk())
		}
	} else {
		if _, err := os.Stat(imagePath); err != nil {
			return fmt.Errorf("guest image missing at %s (run: make image)", imagePath)
		}
		if err := cloneFile(imagePath, sess.RootDisk()); err != nil {
			return fmt.Errorf("clone session disk: %w", err)
		}
	}
	if err := sess.WriteGuestConfig(model, mcpServers); err != nil {
		return err
	}
	data, err := os.ReadFile(sess.GuestConfigJSON())
	if err != nil {
		return err
	}
	return session.WritePaddedConfig(sess.ConfigDisk(), data)
}

func Start(ctx context.Context, sess *session.Session, vmmPath string, vcpu int, ram int) (*Sandbox, error) {
	if vmmPath == "" {
		vmmPath = lookPath("abox-vmm")
	}
	if vmmPath == "" {
		return nil, fmt.Errorf("abox-vmm not found; build with make build")
	}
	_ = os.Remove(sess.RPCSocket())
	ln, err := net.Listen("unix", sess.RPCSocket())
	if err != nil {
		return nil, fmt.Errorf("listen rpc: %w", err)
	}
	if err := os.Chmod(sess.RPCSocket(), 0o600); err != nil {
		ln.Close()
		return nil, err
	}

	cfg := vmmconfig.Config{
		VCPU:       uint8(vcpu),
		RAMMiB:     uint32(ram),
		RootDisk:   sess.RootDisk(),
		ConfigDisk: sess.ConfigDisk(),
		RPCSocket:  sess.RPCSocket(),
		VsockPort:  protocol.RPCPort,
		ExecPath:   vmmconfig.DefaultExecPath,
		ConsoleLog: sess.ConsoleLog(),
	}
	payload, err := json.Marshal(cfg)
	if err != nil {
		ln.Close()
		return nil, err
	}

	cmd := exec.Command(vmmPath)
	cmd.Dir = sess.Dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"DYLD_LIBRARY_PATH=/opt/homebrew/lib",
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		ln.Close()
		return nil, err
	}
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		ln.Close()
		return nil, fmt.Errorf("start abox-vmm: %w", err)
	}
	if _, err := stdin.Write(payload); err != nil {
		cmd.Process.Kill()
		ln.Close()
		return nil, err
	}
	stdin.Close()

	type acc struct {
		c   net.Conn
		err error
	}
	ch := make(chan acc, 1)
	go func() {
		_ = ln.(*net.UnixListener).SetDeadline(time.Now().Add(30 * time.Second))
		c, err := ln.Accept()
		ch <- acc{c, err}
	}()

	var conn net.Conn
	select {
	case <-ctx.Done():
		cmd.Process.Kill()
		ln.Close()
		return nil, ctx.Err()
	case a := <-ch:
		if a.err != nil {
			cmd.Process.Kill()
			ln.Close()
			return nil, fmt.Errorf("guest rpc accept: %w", a.err)
		}
		conn = a.c
	}

	sb := &Sandbox{Sess: sess, cmd: cmd, conn: conn, calls: map[string]*frameQueue{}}
	if err := sb.waitHello(ctx); err != nil {
		sb.Stop()
		return nil, err
	}
	return sb, nil
}

func (s *Sandbox) waitHello(ctx context.Context) error {
	_ = s.conn.SetDeadline(time.Now().Add(15 * time.Second))
	frame, err := protocol.ReadFrame(s.conn)
	if err != nil {
		return fmt.Errorf("guest hello: %w", err)
	}
	if frame.Method != "hello" {
		return fmt.Errorf("expected hello, got %q", frame.Method)
	}
	hello, err := protocol.DecodeParams[protocol.HelloParams](frame.Params)
	if err != nil {
		return err
	}
	if hello.SessionID != s.Sess.ID || hello.Capability != s.Sess.Capability {
		return fmt.Errorf("guest capability mismatch")
	}
	ok, _ := protocol.EncodeParams(protocol.HelloResult{Accepted: true, Protocol: protocol.Version})
	if err := protocol.WriteFrame(s.conn, protocol.Frame{ID: frame.ID, Result: ok}); err != nil {
		return err
	}
	if hello.Protocol == 0 {
		s.GuestProtocol = 1
	} else {
		s.GuestProtocol = hello.Protocol
	}
	s.History = hello.History
	_ = s.conn.SetDeadline(time.Time{})
	return nil
}

func (s *Sandbox) startReading() {
	s.readOnce.Do(func() {
		s.readDone = make(chan struct{})
		s.lifeCtx, s.lifeCancel = context.WithCancel(context.Background())
		s.guestSlots = make(chan struct{}, protocol.MaxGuestCalls-1) // reserve one slot for cancel
		s.cancelSlot = make(chan struct{}, 1)
		s.busyQueue = make(chan protocol.Frame, protocol.MaxGuestCalls)
		go s.writeBusyReplies()
		go s.readLoop()
	})
}

func (s *Sandbox) readLoop() {
	for {
		frame, err := protocol.ReadFrame(s.conn)
		if err != nil {
			s.failAll(err)
			return
		}
		if frame.Method != "" {
			switch {
			case strings.HasPrefix(frame.ID, "g-"):
				slots := s.guestSlots
				if frame.Method == "provider_cancel" {
					slots = s.cancelSlot
				}
				select {
				case slots <- struct{}{}:
					go func(frame protocol.Frame, slots chan struct{}) {
						defer func() { <-slots }()
						s.dispatchGuestCall(frame)
					}(frame, slots)
				default:
					select {
					case s.busyQueue <- frame:
					default:
						s.failConnection(fmt.Errorf("guest busy reply backpressure exceeded"))
						return
					}
				}
			case frame.Method == "agent_event":
				s.mu.Lock()
				q := s.turnQ
				active := s.activeTurn
				if q != nil && frame.ID == active {
					if err := q.push(frame); err != nil && !errors.Is(err, errQueueClosed) {
						queueErr := fmt.Errorf("turn frame queue overflow: %w", err)
						q.abort(queueErr)
						s.mu.Unlock()
						s.failConnection(queueErr)
						return
					}
				}
				s.mu.Unlock()
			default:
			}
			continue
		}
		s.mu.Lock()
		q, known := s.calls[frame.ID]
		turnFrame := false
		if !known && frame.ID == s.activeTurn {
			q = s.turnQ
			turnFrame = true
		}
		if q == nil {
			s.mu.Unlock()
			continue
		}
		if err := q.push(frame); err != nil && !errors.Is(err, errQueueClosed) {
			queueName := "call"
			if turnFrame {
				queueName = "turn"
			}
			queueErr := fmt.Errorf("%s frame queue overflow: %w", queueName, err)
			q.abort(queueErr)
			s.mu.Unlock()
			s.failConnection(queueErr)
			return
		}
		s.mu.Unlock()
	}
}

func (s *Sandbox) failConnection(err error) {
	_ = s.conn.Close()
	s.failAll(err)
}

func (s *Sandbox) failAll(err error) {
	s.failOnce.Do(func() {
		if err == nil {
			err = errors.New("guest connection closed")
		}
		if s.lifeCancel != nil {
			s.lifeCancel()
		}
		s.mu.Lock()
		pending := s.calls
		s.calls = map[string]*frameQueue{}
		turnQ := s.turnQ
		s.turnQ = nil
		s.activeTurn = ""
		s.mu.Unlock()
		for _, q := range pending {
			q.close(err)
		}
		if turnQ != nil {
			turnQ.close(err)
		}
		close(s.readDone)
	})
}

func (s *Sandbox) dispatchGuestCall(frame protocol.Frame) {
	out := protocol.Frame{V: protocol.Version, ID: frame.ID}
	if s.GuestProtocol < 3 {
		out.Error = &protocol.Error{Code: "host", Message: fmt.Sprintf("provider broker requires protocol 3, guest speaks %d", s.GuestProtocol)}
		if err := s.writeFrame(out); err != nil {
			s.failConnection(err)
		}
		return
	}
	if s.OnGuestCall == nil {
		out.Error = &protocol.Error{Code: "host", Message: "guest calls not supported by this host"}
		if err := s.writeFrame(out); err != nil {
			s.failConnection(err)
		}
		return
	}
	notify := func(method string, params any) error {
		raw, err := protocol.EncodeParams(params)
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.nextID++
		n := s.nextID
		s.mu.Unlock()
		if err := s.writeFrame(protocol.Frame{V: protocol.Version, ID: fmt.Sprintf("h-%d", n), Method: method, Params: raw}); err != nil {
			s.failConnection(err)
			return err
		}
		return nil
	}
	res, perr := s.OnGuestCall.Handle(s.lifeCtx, frame.Method, frame.Params, notify)
	switch {
	case perr != nil:
		out.Error = perr
	case res != nil:
		raw, err := protocol.EncodeParams(res)
		if err != nil {
			out.Error = &protocol.Error{Code: "host", Message: err.Error()}
		} else {
			out.Result = raw
		}
	default:
		out.Result = []byte(`{"ok":true}`)
	}
	if err := s.writeFrame(out); err != nil {
		s.failConnection(err)
	}
}

func (s *Sandbox) writeBusyReplies() {
	for {
		select {
		case <-s.lifeCtx.Done():
			return
		case frame := <-s.busyQueue:
			if err := s.writeFrame(protocol.Frame{V: protocol.Version, ID: frame.ID, Error: &protocol.Error{
				Code: "busy", Message: "too many concurrent guest calls",
			}}); err != nil {
				s.failConnection(err)
				return
			}
		}
	}
}

func (s *Sandbox) writeFrame(f protocol.Frame) error {
	return s.writeFrameContext(context.Background(), f)
}

func (s *Sandbox) writeFrameContext(parent context.Context, f protocol.Frame) error {
	ctx, cancel := context.WithTimeout(parent, writeTimeout)
	defer cancel()
	s.writeOnce.Do(func() {
		s.writeGate = make(chan struct{}, 1)
		s.writeGate <- struct{}{}
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.writeGate:
	}
	defer func() { s.writeGate <- struct{}{} }()
	done := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		select {
		case <-done:
		case <-ctx.Done():
			select {
			case <-done:
			default:
				_ = s.conn.Close()
			}
		}
		close(watchDone)
	}()
	err := protocol.WriteFrame(s.conn, f)
	close(done)
	<-watchDone
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil {
		return err
	}
	return nil
}

func (s *Sandbox) dropCall(id string) {
	s.mu.Lock()
	q := s.calls[id]
	delete(s.calls, id)
	s.mu.Unlock()
	if q != nil {
		q.close(context.Canceled)
	}
}

func (s *Sandbox) Call(ctx context.Context, method string, params any, result any) error {
	s.startReading()
	raw, err := protocol.EncodeParams(params)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.lifeCtx.Err() != nil {
		s.mu.Unlock()
		return errors.New("guest connection closed")
	}
	if s.calls == nil {
		s.calls = map[string]*frameQueue{}
	}
	s.nextID++
	id := strconv.Itoa(s.nextID)
	q := newFrameQueue(callQueueFrames, callQueueBytes)
	s.calls[id] = q
	s.mu.Unlock()
	if err := s.writeFrameContext(ctx, protocol.Frame{V: protocol.Version, ID: id, Method: method, Params: raw}); err != nil {
		s.failConnection(err)
		return err
	}
	frame, ok, err := q.pop(ctx)
	s.dropCall(id)
	if !ok {
		if err != nil {
			return err
		}
		return errors.New("guest connection closed")
	}
	if frame.Error != nil {
		return frame.Error
	}
	if result == nil || len(frame.Result) == 0 {
		return nil
	}
	return json.Unmarshal(frame.Result, result)
}

func (s *Sandbox) UserTurn(ctx context.Context, text string, onEvent func(protocol.AgentEvent)) error {
	_, err := s.userTurnLocked(ctx, text, TurnOptions{}, onEvent, false)
	return err
}

func (s *Sandbox) UserTurnCtx(ctx context.Context, text string, opts TurnOptions, onEvent func(protocol.AgentEvent)) (*TurnOutcome, error) {
	return s.userTurnLocked(ctx, text, opts, onEvent, true)
}

func (s *Sandbox) userTurnLocked(ctx context.Context, text string, opts TurnOptions, onEvent func(protocol.AgentEvent), v2API bool) (*TurnOutcome, error) {
	if v2API && opts.needsV2() && s.GuestProtocol < 2 {
		return nil, fmt.Errorf("%w: need protocol 2, guest speaks %d", ErrGuestTooOld, s.GuestProtocol)
	}
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	s.startReading()

	s.mu.Lock()
	if s.lifeCtx.Err() != nil {
		s.mu.Unlock()
		return nil, errors.New("guest connection closed")
	}
	s.nextID++
	id := strconv.Itoa(s.nextID)
	params := protocol.UserTurnParams{Text: text}
	if s.GuestProtocol >= 2 {
		params.MaxTurns = opts.MaxTurns
		params.TimeoutSec = opts.TimeoutSec
		params.RichEvents = opts.RichEvents
	}
	raw, err := protocol.EncodeParams(params)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	turnQ := newFrameQueue(turnQueueFrames, turnQueueBytes)
	s.activeTurn = id
	s.turnQ = turnQ
	s.mu.Unlock()
	if err := s.writeFrame(protocol.Frame{V: protocol.Version, ID: id, Method: "user_turn", Params: raw}); err != nil {
		s.failConnection(err)
		return nil, err
	}
	defer func() {
		s.mu.Lock()
		if s.activeTurn == id {
			s.activeTurn = ""
			s.turnQ = nil
		}
		s.mu.Unlock()
		turnQ.close(context.Canceled)
	}()

	supportsCancel := v2API && s.GuestProtocol >= 2
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	waitCtx := ctx
	if supportsCancel {
		var stopWait context.CancelCauseFunc
		waitCtx, stopWait = context.WithCancelCause(context.Background())
		defer stopWait(context.Canceled)
		go s.watchCancel(ctx, id, stopWatch, stopWait)
	}

	out := &TurnOutcome{}
	for {
		frame, ok, err := turnQ.pop(waitCtx)
		if !ok {
			if err != nil {
				if supportsCancel {
					if cause := context.Cause(waitCtx); cause != nil {
						return out, cause
					}
				}
				return out, err
			}
			return out, errors.New("guest connection closed")
		}
		if frame.Method == "agent_event" && frame.ID == id {
			ev, err := protocol.DecodeParams[protocol.AgentEvent](frame.Params)
			if err != nil {
				return out, err
			}
			if ev.Kind == "result" {
				out.Usage = ev.Usage
				out.StopReason = ev.StopReason
			}
			if onEvent != nil {
				onEvent(ev)
			}
			continue
		}
		if frame.Error != nil {
			if frame.Error.Code == "canceled" {
				out.Canceled = true
			}
			return out, frame.Error
		}
		return out, nil
	}
}

// watchCancel forwards cancellation to a protocol-2+ guest and fails the turn
// if the guest never acknowledges within cancelResponseTimeout.
func (s *Sandbox) watchCancel(ctx context.Context, turnID string, stopWatch <-chan struct{}, stopWait context.CancelCauseFunc) {
	select {
	case <-ctx.Done():
	case <-stopWatch:
		return
	}
	raw, err := protocol.EncodeParams(protocol.CancelTurnParams{ID: turnID})
	if err != nil {
		return
	}
	cancelCtx, cancel := context.WithTimeout(context.Background(), cancelResponseTimeout)
	defer cancel()
	err = s.writeFrameContext(cancelCtx, protocol.Frame{V: protocol.Version, ID: turnID + "-cancel", Method: "cancel_turn", Params: raw})
	if err != nil {
		s.failConnection(err)
		stopWait(err)
		return
	}
	select {
	case <-stopWatch:
	case <-cancelCtx.Done():
		stopWait(&protocol.Error{
			Code:    "timeout",
			Message: "guest did not acknowledge cancel within " + cancelResponseTimeout.String(),
		})
	}
}

func (s *Sandbox) PushSecrets(ctx context.Context, model config.Model, secrets map[string]string) error {
	if len(secrets) == 0 {
		return nil
	}
	if s.GuestProtocol < 2 {
		return fmt.Errorf("guest image predates secret push; run make image")
	}
	if s.GuestProtocol == 2 {
		fmt.Fprintf(os.Stderr, "abox: guest speaks protocol 2; pushing legacy secrets (run make image to upgrade)\n")
	}
	modelKey := model.EnvName()
	rest := map[string]string{}
	for k, v := range secrets {
		if k != modelKey {
			rest[k] = v
		}
	}
	var modelSecrets map[string]string
	if s.GuestProtocol < 3 {
		if v, ok := secrets[modelKey]; ok {
			modelSecrets = map[string]string{modelKey: v}
		}
	}
	if err := s.SetModel(ctx, model, modelSecrets); err != nil {
		return err
	}
	if len(rest) > 0 {
		return s.SetMCPTokens(ctx, rest)
	}
	return nil
}

func (s *Sandbox) SetMCPTokens(ctx context.Context, secrets map[string]string) error {
	return s.Call(ctx, "set_mcp_tokens", protocol.SetMCPTokensParams{Secrets: secrets}, nil)
}

func (s *Sandbox) SetModel(ctx context.Context, model config.Model, secrets map[string]string) error {
	if s.GuestProtocol >= 3 {
		secrets = nil
	}
	return s.Call(ctx, "set_model", protocol.SetModelParams{
		Model:   model.ToGuest(),
		Secrets: secrets,
	}, nil)
}

func (s *Sandbox) TransferArchive(ctx context.Context, archive []byte) error {
	const chunk = protocol.MaxArchiveChunk
	for off := 0; off < len(archive); off += chunk {
		end := off + chunk
		if end > len(archive) {
			end = len(archive)
		}
		params := protocol.ArchiveChunkParams{
			Offset: int64(off),
			Last:   end == len(archive),
			Data:   archive[off:end],
		}
		var res protocol.ArchiveChunkResult
		if err := s.Call(ctx, "archive_chunk", params, &res); err != nil {
			return err
		}
	}
	return nil
}

func (s *Sandbox) Stop() error {
	if s.conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		_ = s.Call(ctx, "shutdown", map[string]bool{"ok": true}, nil)
		cancel()
		if s.lifeCancel != nil {
			s.lifeCancel()
		}
		_ = s.conn.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() {
			s.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = s.cmd.Process.Kill()
		}
	}
	return nil
}

func cloneFile(src, dst string) error {
	_ = os.Remove(dst)
	if err := unix.Clonefile(src, dst, 0); err == nil {
		return os.Chmod(dst, 0o600)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func lookPath(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	cand := filepath.Join(filepath.Dir(exe), name)
	if _, err := os.Stat(cand); err == nil {
		return cand
	}
	return ""
}
