//go:build linux

// To configure the microVM. It is the daemon/workload inside of the VM. Its the long-running application inside the VM

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/agent"
	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/guest/brokerclient"
	"github.com/AdminTurnedDevOps/ABox/internal/guest/mcpclient"
	"github.com/AdminTurnedDevOps/ABox/internal/guest/tools"
	"github.com/AdminTurnedDevOps/ABox/protocol"
	"golang.org/x/sys/unix"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "abox-guest: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	prepMounts()
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	repo := tools.Repo{Root: cfg.RepoDir}
	if err := os.MkdirAll(repo.Root, 0o755); err != nil {
		return err
	}
	if len(cfg.Secrets) > 0 || len(cfg.MCPServers) > 0 {
		return fmt.Errorf("legacy guest config contains credentials or MCP endpoints; rebuild the session")
	}
	bclient := brokerclient.New()
	mcpClient := mcpclient.New(bclient)
	loop := &agent.Loop{
		Model:             config.ModelFromGuest(cfg.Model),
		Repo:              repo,
		MCP:               mcpClient,
		ContextFile:       agent.DefaultContextFile,
		Stream:            bclient.Stream,
		ApproveRunCommand: bclient.RequestRunCommandApproval,
	}
	if err := loop.LoadContext(); err != nil {
		fmt.Fprintf(os.Stderr, "abox-guest: context: %v\n", err)
	}
	conn, err := dialVsock(cfg.VsockPort)
	if err != nil {
		return fmt.Errorf("vsock: %w", err)
	}
	defer conn.Close()

	hello, _ := protocol.EncodeParams(protocol.HelloParams{
		SessionID:  cfg.SessionID,
		Capability: cfg.Capability,
		ImageID:    "abox-guest-dev",
		Protocol:   protocol.Version,
		GuestReady: true,
		History:    loop.History(),
	})
	if err := protocol.WriteFrame(conn, protocol.Frame{ID: "hello", Method: "hello", Params: hello}); err != nil {
		return err
	}
	ack, err := protocol.ReadFrame(conn)
	if err != nil {
		return fmt.Errorf("hello ack: %w", err)
	}
	if ack.Error != nil {
		return ack.Error
	}
	var ackRes protocol.HelloResult
	if len(ack.Result) == 0 || json.Unmarshal(ack.Result, &ackRes) != nil || !ackRes.Accepted {
		return fmt.Errorf("host rejected guest protocol")
	}
	bclient.SetHostProtocol(ackRes.Protocol)
	if ackRes.Protocol < 4 {
		return fmt.Errorf("host protocol %d cannot enforce brokered MCP and command approvals; run make build", ackRes.Protocol)
	}

	w := &connWriter{c: conn}
	bclient.AttachContext(w.writeContext)
	turns := &turnTracker{}
	shutdownHandled := false
	defer bclient.Close(errors.New("guest connection closed"))
	defer func() {
		if shutdownHandled {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		turns.cancelAllAndWait(ctx)
	}()
	var archive bytes.Buffer
	for {
		frame, err := protocol.ReadFrame(conn)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if bclient.HandleFrame(frame) {
			continue
		}
		switch frame.Method {
		case "user_turn":
			if !turns.start(frame.ID) {
				_ = w.write(protocol.Frame{V: protocol.Version, ID: frame.ID, Error: &protocol.Error{Code: "guest", Message: "turn already in progress"}})
				continue
			}
			go runTurn(w, turns, loop, bclient, frame)
		case "cancel_turn":
			p, e := protocol.DecodeParams[protocol.CancelTurnParams](frame.Params)
			if e != nil {
				_ = w.write(protocol.Frame{V: protocol.Version, ID: frame.ID, Error: &protocol.Error{Code: "guest", Message: e.Error()}})
				continue
			}
			turns.cancel(p.ID)
			ok, _ := protocol.EncodeParams(map[string]bool{"ok": true})
			_ = w.write(protocol.Frame{V: protocol.Version, ID: frame.ID, Result: ok})
		case "shutdown":
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			stopped := turns.cancelAllAndWait(ctx)
			cancel()
			shutdownHandled = true
			if !stopped {
				return fmt.Errorf("active turn did not stop before shutdown timeout")
			}
			_ = loop.SaveContext()
			ok, _ := protocol.EncodeParams(map[string]bool{"ok": true})
			if err := w.write(protocol.Frame{V: protocol.Version, ID: frame.ID, Result: ok}); err != nil {
				return err
			}
			return nil
		default:
			resp := handle(loop, repo, &archive, frame)
			if err := w.write(resp); err != nil {
				return err
			}
		}
	}
}

type connWriter struct {
	once sync.Once
	gate chan struct{}
	c    net.Conn
}

func (w *connWriter) write(f protocol.Frame) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return w.writeContext(ctx, f)
}

func (w *connWriter) writeContext(ctx context.Context, f protocol.Frame) error {
	w.once.Do(func() {
		w.gate = make(chan struct{}, 1)
		w.gate <- struct{}{}
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.gate:
	}
	defer func() { w.gate <- struct{}{} }()
	done := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		select {
		case <-done:
		case <-ctx.Done():
			select {
			case <-done:
			default:
				_ = w.c.Close()
			}
		}
		close(watchDone)
	}()
	err := protocol.WriteFrame(w.c, f)
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

type turnTracker struct {
	mu       sync.Mutex
	active   string
	stop     context.CancelFunc
	canceled bool
	finished chan struct{}
}

func (t *turnTracker) start(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.active != "" {
		return false
	}
	t.active = id
	t.canceled = false
	t.finished = make(chan struct{})
	return true
}

func (t *turnTracker) setCancel(id string, cancel context.CancelFunc) {
	t.mu.Lock()
	shouldCancel := false
	if t.active == id {
		t.stop = cancel
		shouldCancel = t.canceled
	}
	t.mu.Unlock()
	if shouldCancel {
		cancel()
	}
}

func (t *turnTracker) cancel(id string) {
	t.mu.Lock()
	var stop context.CancelFunc
	if t.active == id {
		t.canceled = true
		stop = t.stop
	}
	t.mu.Unlock()
	if stop != nil {
		stop()
	}
}

func (t *turnTracker) cancelAllAndWait(ctx context.Context) bool {
	t.mu.Lock()
	if t.active == "" {
		t.mu.Unlock()
		return true
	}
	t.canceled = true
	stop := t.stop
	finished := t.finished
	t.mu.Unlock()
	if stop != nil {
		stop()
	}
	select {
	case <-finished:
		return true
	case <-ctx.Done():
		return false
	}
}

func (t *turnTracker) done(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.active == id {
		finished := t.finished
		t.active = ""
		t.stop = nil
		t.canceled = false
		t.finished = nil
		if finished != nil {
			close(finished)
		}
	}
}

func runTurn(w *connWriter, turns *turnTracker, loop *agent.Loop, bclient *brokerclient.Client, req protocol.Frame) {
	defer turns.done(req.ID)
	p, err := protocol.DecodeParams[protocol.UserTurnParams](req.Params)
	if err != nil {
		_ = w.write(protocol.Frame{ID: req.ID, Error: &protocol.Error{Code: "guest", Message: err.Error()}})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if p.TimeoutSec > 0 {
		var timeoutCancel context.CancelFunc
		ctx, timeoutCancel = context.WithTimeout(ctx, time.Duration(p.TimeoutSec)*time.Second)
		defer timeoutCancel()
	}
	turns.setCancel(req.ID, cancel)
	loop.MaxTurns = p.MaxTurns
	loop.Rich = p.RichEvents
	loop.TurnID = req.ID
	if p.RichEvents {
		loop.Stream = bclient.StreamWithUsage
	} else {
		loop.Stream = bclient.Stream
	}
	loop.OnEvent = func(ev protocol.AgentEvent) {
		raw, _ := protocol.EncodeParams(ev)
		_ = w.write(protocol.Frame{ID: req.ID, Method: "agent_event", Params: raw})
	}
	if err := loop.Turn(ctx, p.Text); err != nil {
		_ = loop.SaveContext()
		code := "agent"
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			code = "canceled"
		}
		_ = w.write(protocol.Frame{ID: req.ID, Error: &protocol.Error{Code: code, Message: err.Error()}})
		return
	}
	_ = loop.SaveContext()
	ok, _ := protocol.EncodeParams(map[string]bool{"ok": true})
	_ = w.write(protocol.Frame{ID: req.ID, Result: ok})
}

func handle(loop *agent.Loop, repo tools.Repo, archive *bytes.Buffer, req protocol.Frame) protocol.Frame {
	out := protocol.Frame{V: protocol.Version, ID: req.ID}
	var err error
	switch req.Method {
	case "get_context":
		out.Result, _ = protocol.EncodeParams(protocol.GetContextResult{History: loop.History()})
	case "set_model":
		p, e := protocol.DecodeParams[protocol.SetModelParams](req.Params)
		if e != nil {
			err = e
			break
		}
		loop.Model = config.ModelFromGuest(p.Model)
		out.Result, _ = protocol.EncodeParams(map[string]bool{"ok": true})
	case "archive_chunk":
		p, e := protocol.DecodeParams[protocol.ArchiveChunkParams](req.Params)
		if e != nil {
			err = e
			break
		}
		archive.Write(p.Data)
		if p.Last {
			if e := tools.ExtractTar(bytes.NewReader(archive.Bytes()), repo.Root); e != nil {
				err = e
				break
			}
			archive.Reset()
			if e := repo.InitBaseline(); e != nil {
				err = e
				break
			}
		}
		out.Result, _ = protocol.EncodeParams(protocol.ArchiveChunkResult{Written: int64(len(p.Data))})
	case "export_patch":
		patch, summary, e := repo.ExportPatch()
		if e != nil {
			err = e
			break
		}
		out.Result, _ = protocol.EncodeParams(protocol.ExportPatchResult{Patch: patch, Summary: summary})
	case "quiesce":
		if e := tools.Freeze(); e != nil {
			err = e
			break
		}
		out.Result, _ = protocol.EncodeParams(protocol.QuiesceResult{Frozen: true})
	case "set_time":
		p, e := protocol.DecodeParams[protocol.SetTimeParams](req.Params)
		if e != nil {
			err = e
			break
		}
		t := time.UnixMicro(p.UnixMicro)
		_ = execSetTime(t)
		out.Result, _ = protocol.EncodeParams(map[string]bool{"ok": true})
	case "shutdown":
		_ = loop.SaveContext()
		out.Result, _ = protocol.EncodeParams(map[string]bool{"ok": true})
	default:
		if tools.IsBuiltin(req.Method) {
			var result any
			result, err = repo.CallBuiltin(req.Method, req.Params, 0)
			if result != nil {
				out.Result, _ = protocol.EncodeParams(result)
			}
			break
		}
		err = fmt.Errorf("unknown method %q", req.Method)
	}
	if err != nil {
		out.Error = &protocol.Error{Code: "guest", Message: err.Error()}
	}
	return out
}

func loadConfig() (protocol.GuestConfig, error) {
	if cfg, err := readRawConfig("/dev/vdb"); err == nil {
		return cfg, nil
	}
	candidates := []string{
		"/abox-config/config.json",
		"/mnt/abox-config/config.json",
		"/etc/abox/config.json",
	}
	_ = os.MkdirAll("/abox-config", 0o755)
	_ = mountIfNeeded("/dev/vdb", "/abox-config")
	var last error
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			last = err
			continue
		}
		return parseGuestConfig(data)
	}
	return protocol.GuestConfig{}, fmt.Errorf("guest config: %w", last)
}

func readRawConfig(dev string) (protocol.GuestConfig, error) {
	f, err := os.Open(dev)
	if err != nil {
		return protocol.GuestConfig{}, err
	}
	defer f.Close()
	buf := make([]byte, 64<<10)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return protocol.GuestConfig{}, err
	}
	return parseGuestConfig(buf[:n])
}

func parseGuestConfig(data []byte) (protocol.GuestConfig, error) {
	if i := bytes.IndexByte(data, 0); i >= 0 {
		data = data[:i]
	}
	var cfg protocol.GuestConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if cfg.RepoDir == "" {
		cfg.RepoDir = protocol.GuestRepoDir
	}
	if cfg.VsockPort == 0 {
		cfg.VsockPort = protocol.RPCPort
	}
	return cfg, nil
}

func prepMounts() {
	_ = os.MkdirAll("/proc", 0o755)
	_ = os.MkdirAll("/sys", 0o755)
	_ = os.MkdirAll("/dev", 0o755)
	_ = os.MkdirAll("/tmp", 0o1777)
	_ = os.MkdirAll(protocol.GuestRepoDir, 0o755)
	_ = os.MkdirAll("/var/lib/abox", 0o755)
	_ = mountIfNeeded("proc", "/proc")
	_ = mountIfNeeded("sysfs", "/sys")
	_ = mountIfNeeded("devtmpfs", "/dev")
}

func mountIfNeeded(src, dest string) error {
	if strings.HasPrefix(src, "/dev/") {
		return unix.Mount(src, dest, "ext4", 0, "")
	}
	var fstype string
	switch src {
	case "proc":
		fstype = "proc"
	case "sysfs":
		fstype = "sysfs"
	case "devtmpfs":
		fstype = "devtmpfs"
	default:
		return nil
	}
	return unix.Mount(src, dest, fstype, 0, "")
}

func dialVsock(port uint32) (net.Conn, error) {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, err
	}
	sa := &unix.SockaddrVM{CID: unix.VMADDR_CID_HOST, Port: port}
	var last error
	for i := 0; i < 50; i++ {
		if err := unix.Connect(fd, sa); err != nil {
			last = err
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if err := unix.SetNonblock(fd, false); err != nil {
			unix.Close(fd)
			return nil, err
		}
		return &vsockConn{fd: fd}, nil
	}
	unix.Close(fd)
	return nil, last
}

type vsockConn struct{ fd int }

func (c *vsockConn) Read(p []byte) (int, error) {
	n, err := unix.Read(c.fd, p)
	if n == 0 && err == nil {
		return 0, io.EOF
	}
	return n, err
}
func (c *vsockConn) Write(p []byte) (int, error) { return unix.Write(c.fd, p) }
func (c *vsockConn) Close() error                { return unix.Close(c.fd) }
func (c *vsockConn) LocalAddr() net.Addr         { return vsockAddr("vsock-local") }
func (c *vsockConn) RemoteAddr() net.Addr        { return vsockAddr("vsock-host") }
func (c *vsockConn) SetDeadline(t time.Time) error {
	_ = c.SetReadDeadline(t)
	return c.SetWriteDeadline(t)
}
func (c *vsockConn) SetReadDeadline(t time.Time) error {
	return nil
}
func (c *vsockConn) SetWriteDeadline(t time.Time) error { return nil }

type vsockAddr string

func (v vsockAddr) Network() string { return "vsock" }
func (v vsockAddr) String() string  { return string(v) }

func execSetTime(t time.Time) error {
	tv := unix.NsecToTimeval(t.UnixNano())
	return unix.Settimeofday(&tv)
}
