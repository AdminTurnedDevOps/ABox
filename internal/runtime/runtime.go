package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/session"
	"github.com/AdminTurnedDevOps/ABox/protocol"
	"golang.org/x/sys/unix"
)

type Sandbox struct {
	Sess    *session.Session
	History []protocol.HistoryLine
	cmd     *exec.Cmd
	conn    net.Conn
	mu      sync.Mutex
	nextID  int
}

type VMMConfig struct {
	VCPU       uint8  `json:"vcpu"`
	RAMMiB     uint32 `json:"ram_mib"`
	RootDisk   string `json:"root_disk"`
	ConfigDisk string `json:"config_disk"`
	RPCSocket  string `json:"rpc_socket"`
	VsockPort  uint32 `json:"vsock_port"`
	ExecPath   string `json:"exec_path"`
	ConsoleLog string `json:"console_log"`
}

func Prepare(sess *session.Session, imagePath string, model config.Model, secrets map[string]string, mcpServers []config.MCPServer, resume bool) error {
	if imagePath == "" {
		imagePath = filepath.Join(config.ImageDir(), "abox-guest.raw")
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
	if err := sess.WriteGuestConfig(model, secrets, mcpServers); err != nil {
		return err
	}
	return writeConfigDisk(sess)
}

func writeConfigDisk(sess *session.Session) error {
	data, err := os.ReadFile(sess.GuestConfigJSON())
	if err != nil {
		return err
	}
	// Resume rewrites config.raw; the previous run left it mode 0400.
	_ = os.Chmod(sess.ConfigDisk(), 0o600)
	f, err := os.OpenFile(sess.ConfigDisk(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if _, err := f.Write(make([]byte, 1<<20-len(data))); err != nil {
		return err
	}
	return os.Chmod(sess.ConfigDisk(), 0o400)
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

	cfg := VMMConfig{
		VCPU:       uint8(vcpu),
		RAMMiB:     uint32(ram),
		RootDisk:   sess.RootDisk(),
		ConfigDisk: sess.ConfigDisk(),
		RPCSocket:  sess.RPCSocket(),
		VsockPort:  protocol.RPCPort,
		ExecPath:   "/usr/local/bin/abox-guest",
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

	sb := &Sandbox{Sess: sess, cmd: cmd, conn: conn}
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
	ok, _ := protocol.EncodeParams(protocol.HelloResult{Accepted: true})
	if err := protocol.WriteFrame(s.conn, protocol.Frame{ID: frame.ID, Result: ok}); err != nil {
		return err
	}
	s.History = hello.History
	_ = s.conn.SetDeadline(time.Time{})
	return nil
}

func (s *Sandbox) Call(ctx context.Context, method string, params any, result any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	id := fmt.Sprintf("%d", s.nextID)
	raw, err := protocol.EncodeParams(params)
	if err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = s.conn.SetDeadline(deadline)
		defer s.conn.SetDeadline(time.Time{})
	}
	if err := protocol.WriteFrame(s.conn, protocol.Frame{ID: id, Method: method, Params: raw}); err != nil {
		return err
	}
	frame, err := protocol.ReadFrame(s.conn)
	if err != nil {
		return err
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	id := fmt.Sprintf("%d", s.nextID)
	raw, err := protocol.EncodeParams(protocol.UserTurnParams{Text: text})
	if err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = s.conn.SetDeadline(deadline)
		defer s.conn.SetDeadline(time.Time{})
	}
	if err := protocol.WriteFrame(s.conn, protocol.Frame{ID: id, Method: "user_turn", Params: raw}); err != nil {
		return err
	}
	for {
		frame, err := protocol.ReadFrame(s.conn)
		if err != nil {
			return err
		}
		if frame.Method == "agent_event" {
			ev, err := protocol.DecodeParams[protocol.AgentEvent](frame.Params)
			if err != nil {
				return err
			}
			if onEvent != nil {
				onEvent(ev)
			}
			continue
		}
		if frame.ID == id {
			if frame.Error != nil {
				return frame.Error
			}
			return nil
		}
	}
}

func (s *Sandbox) SetMCPTokens(ctx context.Context, secrets map[string]string) error {
	return s.Call(ctx, "set_mcp_tokens", protocol.SetMCPTokensParams{Secrets: secrets}, nil)
}

func (s *Sandbox) SetModel(ctx context.Context, model config.Model, secrets map[string]string) error {
	return s.Call(ctx, "set_model", protocol.SetModelParams{
		Model: protocol.GuestModel{
			Name:          model.Name,
			Provider:      model.Provider,
			Model:         model.Model,
			CredentialEnv: model.CredentialEnv,
			BaseURL:       model.BaseURL,
		},
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
		_ = s.Call(context.Background(), "shutdown", map[string]bool{"ok": true}, nil)
		s.conn.Close()
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
