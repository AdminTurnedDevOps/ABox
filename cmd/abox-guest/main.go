//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/agent"
	"github.com/AdminTurnedDevOps/ABox/internal/guest/egress"
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
	if err := egress.ConfigureGuestResolver(); err != nil {
		fmt.Fprintf(os.Stderr, "abox-guest: resolver: %v\n", err)
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	applySecrets(cfg.Secrets)
	repo := tools.Repo{Root: cfg.RepoDir}
	if err := os.MkdirAll(repo.Root, 0o755); err != nil {
		return err
	}
	loop := &agent.Loop{Model: agent.ModelFromGuest(cfg.Model), Repo: repo}
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

	var archive bytes.Buffer
	for {
		frame, err := protocol.ReadFrame(conn)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if frame.Method == "user_turn" {
			if err := handleTurn(conn, loop, frame); err != nil {
				return err
			}
			continue
		}
		resp := handle(loop, repo, &archive, frame)
		if err := protocol.WriteFrame(conn, resp); err != nil {
			return err
		}
		if frame.Method == "shutdown" {
			return nil
		}
	}
}

func handleTurn(conn net.Conn, loop *agent.Loop, req protocol.Frame) error {
	p, err := protocol.DecodeParams[protocol.UserTurnParams](req.Params)
	if err != nil {
		return protocol.WriteFrame(conn, protocol.Frame{ID: req.ID, Error: &protocol.Error{Code: "guest", Message: err.Error()}})
	}
	loop.OnEvent = func(ev protocol.AgentEvent) {
		raw, _ := protocol.EncodeParams(ev)
		_ = protocol.WriteFrame(conn, protocol.Frame{ID: req.ID, Method: "agent_event", Params: raw})
	}
	if err := loop.Turn(context.Background(), p.Text); err != nil {
		return protocol.WriteFrame(conn, protocol.Frame{ID: req.ID, Error: &protocol.Error{Code: "agent", Message: err.Error()}})
	}
	ok, _ := protocol.EncodeParams(map[string]bool{"ok": true})
	return protocol.WriteFrame(conn, protocol.Frame{ID: req.ID, Result: ok})
}

func applySecrets(secrets map[string]string) {
	for k, v := range secrets {
		if k != "" && v != "" {
			_ = os.Setenv(k, v)
		}
	}
}

func handle(loop *agent.Loop, repo tools.Repo, archive *bytes.Buffer, req protocol.Frame) protocol.Frame {
	out := protocol.Frame{V: protocol.Version, ID: req.ID}
	var err error
	switch req.Method {
	case "set_model":
		p, e := protocol.DecodeParams[protocol.SetModelParams](req.Params)
		if e != nil {
			err = e
			break
		}
		applySecrets(p.Secrets)
		loop.Model = agent.ModelFromGuest(p.Model)
		out.Result, _ = protocol.EncodeParams(map[string]bool{"ok": true})
	case "list_files":
		p, e := protocol.DecodeParams[protocol.ListFilesParams](req.Params)
		if e != nil {
			err = e
			break
		}
		paths, e := repo.List(p.Path, p.Depth, p.Limit)
		if e != nil {
			err = e
			break
		}
		out.Result, _ = protocol.EncodeParams(protocol.ListFilesResult{Paths: paths})
	case "read_file":
		p, e := protocol.DecodeParams[protocol.ReadFileParams](req.Params)
		if e != nil {
			err = e
			break
		}
		content, bin, trunc, e := repo.Read(p.Path, p.MaxBytes)
		if e != nil {
			err = e
			break
		}
		out.Result, _ = protocol.EncodeParams(protocol.ReadFileResult{Content: content, Binary: bin, Trunc: trunc})
	case "search":
		p, e := protocol.DecodeParams[protocol.SearchParams](req.Params)
		if e != nil {
			err = e
			break
		}
		matches, e := repo.Search(p.Query, p.Path, p.Limit)
		if e != nil {
			err = e
			break
		}
		out.Result, _ = protocol.EncodeParams(protocol.SearchResult{Matches: matches})
	case "apply_patch":
		p, e := protocol.DecodeParams[protocol.ApplyPatchParams](req.Params)
		if e != nil {
			err = e
			break
		}
		output, e := repo.ApplyPatch(p.Patch)
		out.Result, _ = protocol.EncodeParams(protocol.ApplyPatchResult{OK: e == nil, Output: output})
		err = e
	case "run_command":
		p, e := protocol.DecodeParams[protocol.RunCommandParams](req.Params)
		if e != nil {
			err = e
			break
		}
		timeout := time.Duration(p.Timeout) * time.Second
		exit, stdout, stderr, dur, trunc, e := repo.Run(p.Command, p.WorkDir, timeout, tools.DefaultMaxOutput)
		out.Result, _ = protocol.EncodeParams(protocol.RunCommandResult{
			ExitCode: exit, Stdout: stdout, Stderr: stderr, Duration: dur.String(), Trunc: trunc,
		})
		err = e
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
		out.Result, _ = protocol.EncodeParams(map[string]bool{"ok": true})
	default:
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
		cfg.RepoDir = "/work/repo"
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
	_ = os.MkdirAll("/work/repo", 0o755)
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
