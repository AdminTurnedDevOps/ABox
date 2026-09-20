package runtime

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/guestimage"
	"github.com/AdminTurnedDevOps/ABox/internal/session"
	"github.com/AdminTurnedDevOps/ABox/internal/vmmconfig"
	"github.com/AdminTurnedDevOps/ABox/protocol"
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
	diagnosticLimit = 32 << 10
)

type processWaiter struct {
	done chan struct{}
	err  error
}

func newProcessWaiter(cmd *exec.Cmd) *processWaiter {
	w := &processWaiter{done: make(chan struct{})}
	go func() {
		w.err = cmd.Wait()
		close(w.done)
	}()
	return w
}

func (w *processWaiter) wait() error {
	if w == nil {
		return nil
	}
	<-w.done
	return w.err
}

type boundedDiagnostics struct {
	mu        sync.Mutex
	buf       []byte
	truncated bool
}

func (w *boundedDiagnostics) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	remaining := diagnosticLimit - len(w.buf)
	if remaining > 0 {
		n := len(p)
		if n > remaining {
			n = remaining
		}
		w.buf = append(w.buf, p[:n]...)
	}
	if len(p) > remaining {
		w.truncated = true
	}
	return len(p), nil
}

func (w *boundedDiagnostics) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	text := strings.TrimSpace(string(w.buf))
	if w.truncated {
		text += " [diagnostics truncated]"
	}
	return text
}

func mapHelperDiagnostic(message string) string {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "undefined symbol"):
		return "incompatible libkrun package or ABI: " + message
	case strings.Contains(lower, "error while loading shared libraries") && strings.Contains(lower, "libkrun"):
		return "libkrun is linked but not loadable; verify the runtime loader path and run ldconfig: " + message
	case strings.Contains(lower, "libkrunfw") && (strings.Contains(lower, "not found") || strings.Contains(lower, "cannot open")):
		return "the firmware library required by the installed libkrun package is not loadable; verify the matching libkrunfw package and loader cache: " + message
	default:
		return message
	}
}

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

type RunCommandApprover interface {
	ApproveRunCommand(context.Context, protocol.RunCommandApprovalParams) (protocol.ApprovalDecision, error)
}

type RunCommandApproverFunc func(context.Context, protocol.RunCommandApprovalParams) (protocol.ApprovalDecision, error)

func (f RunCommandApproverFunc) ApproveRunCommand(ctx context.Context, params protocol.RunCommandApprovalParams) (protocol.ApprovalDecision, error) {
	return f(ctx, params)
}

type Sandbox struct {
	Sess          *session.Session
	History       []protocol.HistoryLine
	GuestProtocol int
	cmd           *exec.Cmd
	conn          net.Conn
	liveness      *os.File
	process       *processWaiter
	OnGuestCall   GuestCallHandler

	writeOnce sync.Once
	writeGate chan struct{}
	turnMu    sync.Mutex

	mu         sync.Mutex
	nextID     int
	calls      map[string]*frameQueue
	activeTurn string
	turnCtx    context.Context
	turnCancel context.CancelFunc
	turnQ      *frameQueue
	approver   RunCommandApprover
	lifeCtx    context.Context
	lifeCancel context.CancelFunc
	guestSlots chan struct{}
	cancelSlot chan struct{}
	busyQueue  chan protocol.Frame

	readOnce sync.Once
	failOnce sync.Once
	stopOnce sync.Once
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

type prepareOptions struct {
	resume               bool
	allowOlderProtocol   bool
	allowLegacyDarwinImg bool
}

func Prepare(sess *session.Session, imagePath string, model config.Model, resume bool) error {
	return prepare(sess, imagePath, model, prepareOptions{resume: resume, allowLegacyDarwinImg: true})
}

func PrepareProbe(sess *session.Session, imagePath string, model config.Model) error {
	return prepare(sess, imagePath, model, prepareOptions{allowOlderProtocol: true, allowLegacyDarwinImg: true})
}

func prepare(sess *session.Session, imagePath string, model config.Model, opts prepareOptions) error {
	acquired, err := sess.AcquireRuntimeLock()
	if err != nil {
		return err
	}
	success := false
	defer func() {
		if acquired && !success {
			_ = sess.ReleaseRuntimeLock()
		}
	}()
	sess.DiagnosticProbe = opts.allowOlderProtocol
	defaultImagePath := config.GuestImagePath()
	if imagePath == "" {
		imagePath = defaultImagePath
	}
	backend, err := hostVMMBackend()
	if err != nil {
		return err
	}
	if opts.resume {
		if _, err := os.Stat(sess.RootDisk()); err != nil {
			return fmt.Errorf("resume: session disk missing at %s", sess.RootDisk())
		}
		if sess.GuestArch == "" {
			if goruntime.GOOS != "darwin" || goruntime.GOARCH != "arm64" {
				return fmt.Errorf("resume: session %s predates image compatibility metadata; start a new session on %s/%s", sess.ID, goruntime.GOOS, goruntime.GOARCH)
			}
			sess.GuestArch = "arm64"
			sess.VMMBackend = "hvf"
			digest, err := guestimage.Digest(sess.RootDisk())
			if err != nil {
				return fmt.Errorf("hash legacy session disk: %w", err)
			}
			sess.ImageSHA256 = digest
			if err := sess.WriteMeta(); err != nil {
				return fmt.Errorf("backfill legacy session metadata: %w", err)
			}
		}
		if sess.GuestArch != goruntime.GOARCH {
			return fmt.Errorf("resume: session guest architecture is %s, host requires %s", sess.GuestArch, goruntime.GOARCH)
		}
		if sess.VMMBackend != backend {
			return fmt.Errorf("resume: session VMM backend is %s, host requires %s", sess.VMMBackend, backend)
		}
		legacyDarwin := goruntime.GOOS == "darwin" && goruntime.GOARCH == "arm64" && sess.ManifestSchema == 0
		if !legacyDarwin {
			if sess.ManifestSchema != guestimage.Schema || strings.TrimSpace(sess.ImageID) == "" || !validSHA256(sess.ImageSHA256) {
				return fmt.Errorf("resume: session %s has incomplete or unsupported image compatibility metadata; start a new session", sess.ID)
			}
		} else if !validSHA256(sess.ImageSHA256) {
			return fmt.Errorf("resume: legacy session %s has no verifiable disk identity; start a new session", sess.ID)
		}
		if sess.GuestProtocol > protocol.Version {
			return fmt.Errorf("resume: session guest protocol %d is newer than host protocol %d", sess.GuestProtocol, protocol.Version)
		}
		if sess.GuestProtocol != 0 && !opts.allowOlderProtocol && sess.GuestProtocol != protocol.Version {
			return fmt.Errorf("resume: session guest protocol %d is incompatible with required protocol %d", sess.GuestProtocol, protocol.Version)
		}
		if sess.GuestProtocol == 0 && !legacyDarwin {
			return fmt.Errorf("resume: session %s does not record a guest protocol; start a new session", sess.ID)
		}
	} else {
		if filepath.Clean(imagePath) == filepath.Clean(defaultImagePath) {
			lock, err := guestimage.AcquireSharedLock(imagePath)
			if err != nil {
				return err
			}
			defer lock.Close()
		}
		image, err := guestimage.Load(imagePath)
		if err != nil {
			if !opts.allowLegacyDarwinImg || !isLegacyDarwinImage(imagePath) || !errors.Is(err, guestimage.ErrManifestMissing) {
				return fmt.Errorf("guest image %s is unusable: %w (run: make image)", imagePath, err)
			}
			resolved, resolveErr := filepath.EvalSymlinks(imagePath)
			if resolveErr != nil {
				return fmt.Errorf("guest image missing at %s (run: make image)", imagePath)
			}
			if err := cloneFile(resolved, sess.RootDisk()); err != nil {
				return fmt.Errorf("clone legacy session disk: %w", err)
			}
			digest, err := guestimage.Digest(sess.RootDisk())
			if err != nil {
				_ = os.Remove(sess.RootDisk())
				return fmt.Errorf("hash legacy session disk: %w", err)
			}
			sess.GuestArch = "arm64"
			sess.ImageSHA256 = digest
			sess.VMMBackend = backend
		} else {
			if image.Manifest.Arch != goruntime.GOARCH {
				return fmt.Errorf("guest image architecture is %s, host requires %s", image.Manifest.Arch, goruntime.GOARCH)
			}
			if image.Manifest.Protocol > protocol.Version {
				return fmt.Errorf("guest image protocol %d is newer than host protocol %d", image.Manifest.Protocol, protocol.Version)
			}
			if !opts.allowOlderProtocol && image.Manifest.Protocol != protocol.Version {
				return fmt.Errorf("guest image protocol %d is incompatible with required protocol %d", image.Manifest.Protocol, protocol.Version)
			}
			if err := cloneFile(image.Path, sess.RootDisk()); err != nil {
				return fmt.Errorf("clone session disk: %w", err)
			}
			digest, err := guestimage.Digest(sess.RootDisk())
			if err != nil {
				_ = os.Remove(sess.RootDisk())
				return fmt.Errorf("hash session disk: %w", err)
			}
			if digest != image.Manifest.SHA256 {
				_ = os.Remove(sess.RootDisk())
				return fmt.Errorf("guest image digest mismatch: manifest has %s, cloned image has %s", image.Manifest.SHA256, digest)
			}
			sess.ManifestSchema = image.Manifest.Schema
			sess.GuestArch = image.Manifest.Arch
			sess.ImageID = image.Manifest.ImageID
			sess.ImageSHA256 = image.Manifest.SHA256
			sess.GuestProtocol = image.Manifest.Protocol
			sess.VMMBackend = backend
		}
		if err := sess.WriteMeta(); err != nil {
			_ = os.Remove(sess.RootDisk())
			return fmt.Errorf("write session image metadata: %w", err)
		}
	}
	if err := sess.WriteGuestConfig(model); err != nil {
		return err
	}
	data, err := os.ReadFile(sess.GuestConfigJSON())
	if err != nil {
		return err
	}
	if err := session.WritePaddedConfig(sess.ConfigDisk(), data); err != nil {
		return err
	}
	success = true
	return nil
}

func validSHA256(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	digest, err := hex.DecodeString(value)
	return err == nil && len(digest) == 32
}

func hostVMMBackend() (string, error) {
	switch goruntime.GOOS {
	case "darwin":
		return "hvf", nil
	case "linux":
		return "kvm", nil
	default:
		return "", fmt.Errorf("unsupported VMM host %s/%s", goruntime.GOOS, goruntime.GOARCH)
	}
}

func isLegacyDarwinImage(path string) bool {
	return goruntime.GOOS == "darwin" && goruntime.GOARCH == "arm64" && filepath.Base(path) == config.LegacyGuestImageName
}

func Start(ctx context.Context, sess *session.Session, vmmPath string, vcpu int, ram int) (*Sandbox, error) {
	acquired, err := sess.AcquireRuntimeLock()
	if err != nil {
		return nil, err
	}
	lockTransferred := false
	defer func() {
		if acquired && !lockTransferred {
			_ = sess.ReleaseRuntimeLock()
		}
	}()
	if vmmPath == "" {
		vmmPath = lookPath("abox-vmm")
	}
	if vmmPath == "" {
		return nil, fmt.Errorf("abox-vmm not found; build with make build")
	}
	resolvedVMM := exec.Command(vmmPath).Path
	if err := cleanupStaleHelper(sess, resolvedVMM); err != nil {
		return nil, err
	}
	_ = os.Remove(sess.RPCSocket())
	ln, err := net.Listen("unix", sess.RPCSocket())
	if err != nil {
		return nil, fmt.Errorf("listen rpc: %w", err)
	}
	if err := os.Chmod(sess.RPCSocket(), 0o600); err != nil {
		ln.Close()
		_ = os.Remove(sess.RPCSocket())
		return nil, err
	}
	defer func() {
		_ = ln.Close()
		_ = os.Remove(sess.RPCSocket())
	}()

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
		return nil, err
	}

	cmd := exec.Command(resolvedVMM)
	cmd.Dir = sess.Dir
	cmd.Env = vmmEnvironment()
	livenessRead, livenessWrite, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create VMM liveness pipe: %w", err)
	}
	defer livenessRead.Close()
	livenessOwned := true
	defer func() {
		if livenessOwned {
			_ = livenessWrite.Close()
		}
	}()
	cmd.ExtraFiles = []*os.File{livenessRead}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	diagnostics := &boundedDiagnostics{}
	output := io.MultiWriter(os.Stderr, diagnostics)
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("start abox-vmm: %w", err)
	}
	if err := recordHelper(sess, cmd.Process.Pid, cmd.Path); err != nil {
		_ = stdin.Close()
		_ = livenessWrite.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("record VMM helper identity: %w", err)
	}
	_ = livenessRead.Close()
	process := newProcessWaiter(cmd)
	reapFailure := func() {
		_ = stdin.Close()
		_ = livenessWrite.Close()
		_ = cmd.Process.Kill()
		_ = process.wait()
		_ = clearHelper(sess)
	}
	helperError := func(prefix string) error {
		waitErr := process.wait()
		_ = clearHelper(sess)
		status := "unknown status"
		if cmd.ProcessState != nil {
			status = cmd.ProcessState.String()
		}
		if waitErr != nil {
			status = waitErr.Error()
		}
		message := diagnostics.String()
		if message != "" {
			return fmt.Errorf("%s (%s): %s", prefix, status, message)
		}
		return fmt.Errorf("%s (%s)", prefix, status)
	}
	if _, err := stdin.Write(payload); err != nil {
		reapFailure()
		if message := mapHelperDiagnostic(diagnostics.String()); message != "" {
			return nil, fmt.Errorf("write abox-vmm config: %w: %s", err, message)
		}
		return nil, fmt.Errorf("write abox-vmm config: %w", err)
	}
	if err := stdin.Close(); err != nil {
		reapFailure()
		if message := mapHelperDiagnostic(diagnostics.String()); message != "" {
			return nil, fmt.Errorf("close abox-vmm config: %w: %s", err, message)
		}
		return nil, fmt.Errorf("close abox-vmm config: %w", err)
	}

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
		reapFailure()
		return nil, ctx.Err()
	case <-process.done:
		return nil, helperError("abox-vmm exited before guest RPC connected")
	case a := <-ch:
		if a.err != nil {
			select {
			case <-process.done:
				return nil, helperError("abox-vmm exited before guest RPC connected")
			default:
			}
			reapFailure()
			return nil, fmt.Errorf("guest rpc accept: %w", a.err)
		}
		conn = a.c
	}
	select {
	case <-process.done:
		_ = conn.Close()
		return nil, helperError("abox-vmm exited during guest RPC connect")
	default:
	}

	livenessOwned = false
	sb := &Sandbox{
		Sess: sess, cmd: cmd, conn: conn, liveness: livenessWrite, process: process,
		calls: map[string]*frameQueue{},
	}
	if err := sb.waitHello(ctx); err != nil {
		result := err
		select {
		case <-process.done:
			result = helperError("abox-vmm exited before guest hello")
		default:
		}
		_ = sb.Stop()
		return nil, result
	}
	lockTransferred = true
	return sb, nil
}

func (s *Sandbox) waitHello(ctx context.Context) error {
	deadline := time.Now().Add(15 * time.Second)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = s.conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() {
		_ = s.conn.SetDeadline(time.Now())
	})
	defer stop()
	frame, err := protocol.ReadFrame(s.conn)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
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
	guestProtocol := hello.Protocol
	if guestProtocol == 0 {
		guestProtocol = 1
	}
	reject := func(message string) error {
		result, _ := protocol.EncodeParams(protocol.HelloResult{Accepted: false, Message: message, Protocol: protocol.Version})
		_ = protocol.WriteFrame(s.conn, protocol.Frame{ID: frame.ID, Result: result})
		return errors.New(message)
	}
	if guestProtocol > protocol.Version {
		return reject(fmt.Sprintf("guest protocol %d is newer than host protocol %d", guestProtocol, protocol.Version))
	}
	if guestProtocol != protocol.Version && !s.Sess.DiagnosticProbe {
		return reject(fmt.Sprintf("guest protocol %d is incompatible with required protocol %d", guestProtocol, protocol.Version))
	}
	if s.Sess.GuestProtocol != 0 && guestProtocol != s.Sess.GuestProtocol {
		return reject(fmt.Sprintf("guest protocol %d does not match session metadata %d", guestProtocol, s.Sess.GuestProtocol))
	}
	if s.Sess.ImageID != "" && hello.ImageID != s.Sess.ImageID {
		return reject(fmt.Sprintf("guest image id %q does not match session metadata %q", hello.ImageID, s.Sess.ImageID))
	}
	if guestProtocol == protocol.Version && strings.TrimSpace(hello.ImageID) == "" {
		return reject("guest did not report an image id")
	}
	if s.Sess.GuestProtocol == 0 {
		s.Sess.GuestProtocol = guestProtocol
		s.Sess.ImageID = hello.ImageID
		if err := s.Sess.WriteMeta(); err != nil {
			return reject(fmt.Sprintf("record guest compatibility metadata: %v", err))
		}
	}
	ok, _ := protocol.EncodeParams(protocol.HelloResult{Accepted: true, Protocol: protocol.Version})
	if err := protocol.WriteFrame(s.conn, protocol.Frame{ID: frame.ID, Result: ok}); err != nil {
		return err
	}
	s.GuestProtocol = guestProtocol
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
				if protocol.GuestMethodIsCancellation(frame.Method) {
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
		turnCancel := s.turnCancel
		s.turnQ = nil
		s.activeTurn = ""
		s.turnCtx = nil
		s.turnCancel = nil
		s.mu.Unlock()
		if turnCancel != nil {
			turnCancel()
		}
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
	minimum, known := protocol.GuestMethodMinVersion(frame.Method)
	if !known {
		out.Error = &protocol.Error{Code: "host", Message: "unknown guest method " + frame.Method}
		if err := s.writeFrame(out); err != nil {
			s.failConnection(err)
		}
		return
	}
	if s.GuestProtocol < minimum {
		out.Error = &protocol.Error{Code: "host", Message: fmt.Sprintf("%s requires protocol %d, guest speaks %d", frame.Method, minimum, s.GuestProtocol)}
		if err := s.writeFrame(out); err != nil {
			s.failConnection(err)
		}
		return
	}
	if frame.Method == "request_run_command_approval" {
		s.dispatchRunCommandApproval(frame, &out)
		if err := s.writeFrame(out); err != nil {
			s.failConnection(err)
		}
		return
	}
	s.mu.Lock()
	handler := s.OnGuestCall
	s.mu.Unlock()
	if handler == nil {
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
	res, perr := handler.Handle(s.lifeCtx, frame.Method, frame.Params, notify)
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

func (s *Sandbox) dispatchRunCommandApproval(frame protocol.Frame, out *protocol.Frame) {
	params, err := protocol.DecodeParams[protocol.RunCommandApprovalParams](frame.Params)
	if err != nil || params.TurnID == "" || len(params.Command) > protocol.MaxModelCommandBytes {
		out.Error = &protocol.Error{Code: "host", Message: "invalid run_command approval request"}
		return
	}
	s.mu.Lock()
	active := s.activeTurn
	turnCtx := s.turnCtx
	approver := s.approver
	s.mu.Unlock()
	decision := protocol.ApprovalDeny
	if active == params.TurnID && turnCtx != nil && approver != nil && turnCtx.Err() == nil {
		got, approveErr := approver.ApproveRunCommand(turnCtx, params)
		if approveErr == nil && got == protocol.ApprovalAllowOnce && turnCtx.Err() == nil {
			decision = protocol.ApprovalAllowOnce
		}
	}
	out.Result, _ = protocol.EncodeParams(protocol.RunCommandApprovalResult{Decision: decision})
}

func (s *Sandbox) SetGuestCallHandler(handler GuestCallHandler) {
	s.mu.Lock()
	s.OnGuestCall = handler
	s.mu.Unlock()
}

func (s *Sandbox) SetRunCommandApprover(approver RunCommandApprover) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeTurn != "" {
		return fmt.Errorf("cannot change command approver during a turn")
	}
	s.approver = approver
	return nil
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
	if s.GuestProtocol < 4 {
		return nil, fmt.Errorf("%w: command approval requires protocol 4, guest speaks %d", ErrGuestTooOld, s.GuestProtocol)
	}
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
	turnCtx, turnCancel := context.WithCancel(ctx)
	s.activeTurn = id
	s.turnCtx = turnCtx
	s.turnCancel = turnCancel
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
			s.turnCtx = nil
			s.turnCancel = nil
			s.turnQ = nil
		}
		s.mu.Unlock()
		turnCancel()
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
	if s.GuestProtocol >= 3 {
		return nil
	}
	if s.GuestProtocol < 2 {
		return fmt.Errorf("guest image predates secret push; run make image")
	}
	fmt.Fprintf(os.Stderr, "abox: guest speaks protocol 2; pushing one legacy model credential (run make image to upgrade)\n")
	modelKey := model.EnvName()
	modelSecrets := map[string]string{}
	if v, ok := secrets[modelKey]; ok {
		modelSecrets[modelKey] = v
	}
	return s.SetModel(ctx, model, modelSecrets)
}

func (s *Sandbox) SetMCPTokens(ctx context.Context, secrets map[string]string) error {
	return fmt.Errorf("MCP credentials are host-brokered and cannot be sent to the guest")
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
	s.stopOnce.Do(func() {
		if s.conn != nil {
			ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			_ = s.Call(ctx, "shutdown", map[string]bool{"ok": true}, nil)
			cancel()
			if s.lifeCancel != nil {
				s.lifeCancel()
			}
			_ = s.conn.Close()
		}
		s.stopHelper(shutdownTimeout)
		if s.Sess != nil {
			_ = os.Remove(s.Sess.RPCSocket())
			_ = clearHelper(s.Sess)
			_ = s.Sess.ReleaseRuntimeLock()
		}
	})
	return nil
}

func (s *Sandbox) stopHelper(timeout time.Duration) {
	if s.cmd != nil && s.cmd.Process != nil && s.process != nil {
		select {
		case <-s.process.done:
		default:
			_ = s.cmd.Process.Signal(helperStopSignal())
			select {
			case <-s.process.done:
			case <-time.After(timeout):
				_ = s.cmd.Process.Kill()
			}
		}
		_ = s.process.wait()
	}
	if s.liveness != nil {
		_ = s.liveness.Close()
	}
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
