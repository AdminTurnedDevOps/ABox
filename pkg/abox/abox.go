package abox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/credentials"
	"github.com/AdminTurnedDevOps/ABox/internal/repository"
	"github.com/AdminTurnedDevOps/ABox/internal/runtime"
	"github.com/AdminTurnedDevOps/ABox/internal/session"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

// ErrGuestTooOld is returned when a v2-only Turn option is used against a v1 guest.
var ErrGuestTooOld = runtime.ErrGuestTooOld

type Options struct {
	RepoPath    string
	Model       string
	Image       string
	VMMPath     string
	VCPU        int
	RAMMiB      int
	BootTimeout time.Duration
}

func (o Options) withDefaults() Options {
	if o.RepoPath == "" {
		wd, err := os.Getwd()
		if err == nil {
			o.RepoPath = wd
		}
	}
	if o.BootTimeout == 0 {
		o.BootTimeout = 45 * time.Second
	}
	return o
}

func Open(ctx context.Context, opts Options) (*Session, error) {
	return open(ctx, opts, false, "")
}

func Resume(ctx context.Context, sessionID string, opts Options) (*Session, error) {
	return open(ctx, opts, true, sessionID)
}

func open(ctx context.Context, opts Options, resume bool, resumeID string) (*Session, error) {
	opts = opts.withDefaults()
	cfg, cfgPath, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	if err := credentials.ApplyToEnv(); err != nil {
		return nil, fmt.Errorf("credentials: %w", err)
	}
	sel, ok := cfg.ModelNamed(opts.Model)
	if !ok {
		return nil, fmt.Errorf("no model profile %q (config %s)", opts.Model, cfgPath)
	}
	if err := os.MkdirAll(config.SessionRoot(), 0o700); err != nil {
		return nil, err
	}

	var sess *session.Session
	var snap repository.Snapshot
	if resume {
		loaded, err := loadResume(opts.RepoPath, resumeID)
		if err != nil {
			return nil, err
		}
		sess = loaded
	} else {
		created, err := session.Create(opts.RepoPath, "pending")
		if err != nil {
			return nil, fmt.Errorf("create session: %w", err)
		}
		sess = created
		opened, err := repository.OpenForSession(opts.RepoPath, filepath.Join(sess.Dir, "host-tree"))
		if err != nil {
			return nil, fmt.Errorf("snapshot repo: %w", err)
		}
		snap = opened
		sess.RepoRoot = snap.Root
		sess.HEAD = snap.HEAD
		if err := sess.WriteMeta(); err != nil {
			return nil, err
		}
	}

	image := opts.Image
	if image == "" {
		image = cfg.Runtime.Image
	}
	mcpServers, err := cfg.ResolvedMCPServers()
	if err != nil {
		return nil, err
	}
	if err := runtime.Prepare(sess, image, sel, cfg.SecretsFromEnv(), mcpServers, resume); err != nil {
		return nil, err
	}
	vcpu, ram := cfg.Resources.Resolved()
	if opts.VCPU > 0 {
		vcpu = opts.VCPU
	}
	if opts.RAMMiB > 0 {
		ram = opts.RAMMiB
	}
	bootCtx := ctx
	if opts.BootTimeout > 0 {
		var cancel context.CancelFunc
		bootCtx, cancel = context.WithTimeout(ctx, opts.BootTimeout)
		defer cancel()
	}
	vmm := opts.VMMPath
	if vmm == "" {
		vmm = cfg.Runtime.VMMPath
	}
	sb, err := runtime.Start(bootCtx, sess, vmm, vcpu, ram)
	if err != nil {
		return nil, fmt.Errorf("start vm: %w", err)
	}
	if !resume {
		archive, err := repository.ArchiveHEAD(snap.Root)
		if err != nil {
			sb.Stop()
			return nil, fmt.Errorf("archive repo: %w", err)
		}
		if err := sb.TransferArchive(ctx, archive); err != nil {
			sb.Stop()
			return nil, fmt.Errorf("transfer repo: %w", err)
		}
	}
	return &Session{cfg: cfg, sess: sess, sb: sb, sel: sel}, nil
}

func loadResume(repoPath, id string) (*session.Session, error) {
	if id != "" {
		return session.Load(id)
	}
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		abs = repoPath
	}
	roots := []string{abs}
	if top, err := repository.TopLevel(repoPath); err == nil {
		roots = append(roots, top)
	}
	return session.LatestForRepo(roots...)
}

type Event = protocol.AgentEvent

type Capabilities struct {
	Protocol    int
	Cancel      bool
	RichEvents  bool
	TurnOptions bool
}

type TurnOpts struct {
	MaxTurns   int
	Timeout    time.Duration
	RichEvents bool
}

type TurnResult struct {
	Usage      *protocol.UsageInfo
	StopReason string
	Canceled   bool
}

type Session struct {
	cfg  config.File
	sess *session.Session
	sb   *runtime.Sandbox
	sel  config.Model
}

func (s *Session) ID() string { return s.sess.ID }

func (s *Session) Capabilities() Capabilities {
	p := s.sb.GuestProtocol
	return Capabilities{
		Protocol:    p,
		Cancel:      p >= 2,
		RichEvents:  p >= 2,
		TurnOptions: p >= 2,
	}
}

func (s *Session) History() []protocol.HistoryLine {
	return s.sb.History
}

func (s *Session) Turn(ctx context.Context, prompt string, onEvent func(Event)) (*TurnResult, error) {
	return s.turn(ctx, prompt, TurnOpts{}, onEvent)
}

func (s *Session) TurnOpts(ctx context.Context, prompt string, opts TurnOpts, onEvent func(Event)) (*TurnResult, error) {
	return s.turn(ctx, prompt, opts, onEvent)
}

func (s *Session) turn(ctx context.Context, prompt string, opts TurnOpts, onEvent func(Event)) (*TurnResult, error) {
	rtOpts := runtime.TurnOptions{
		MaxTurns:   opts.MaxTurns,
		RichEvents: opts.RichEvents,
	}
	if opts.Timeout > 0 {
		rtOpts.TimeoutSec = int(opts.Timeout.Seconds())
	}
	if s.sb.GuestProtocol < 2 {
		if opts.MaxTurns > 0 || opts.Timeout > 0 || opts.RichEvents {
			return nil, fmt.Errorf("%w: guest speaks protocol %d", ErrGuestTooOld, s.sb.GuestProtocol)
		}
		err := s.sb.UserTurn(ctx, prompt, onEvent)
		return &TurnResult{}, err
	}
	if !rtOpts.RichEvents {
		rtOpts.RichEvents = true
	}
	out, err := s.sb.UserTurnCtx(ctx, prompt, rtOpts, onEvent)
	res := &TurnResult{}
	if out != nil {
		res.Usage = out.Usage
		res.StopReason = out.StopReason
		res.Canceled = out.Canceled
	}
	if err != nil && errors.Is(err, runtime.ErrGuestTooOld) {
		return res, fmt.Errorf("%w: %v", ErrGuestTooOld, err)
	}
	return res, err
}

func (s *Session) SetModel(ctx context.Context, model string) error {
	sel, ok := s.cfg.ModelNamed(model)
	if !ok {
		return fmt.Errorf("no model profile %q", model)
	}
	s.sel = sel
	return s.sb.SetModel(ctx, sel, s.cfg.SecretsFromEnv())
}

func (s *Session) SetMCPTokens(ctx context.Context, secrets map[string]string) error {
	return s.sb.SetMCPTokens(ctx, secrets)
}

func (s *Session) ExportPatch(ctx context.Context) (patch, summary string, err error) {
	var res protocol.ExportPatchResult
	err = s.sb.Call(ctx, "export_patch", map[string]bool{"ok": true}, &res)
	return res.Patch, res.Summary, err
}

func (s *Session) ListFiles(ctx context.Context, path string, depth, limit int) ([]string, error) {
	var res protocol.ListFilesResult
	err := s.sb.Call(ctx, "list_files", protocol.ListFilesParams{Path: path, Depth: depth, Limit: limit}, &res)
	return res.Paths, err
}

func (s *Session) ReadFile(ctx context.Context, path string, maxBytes int) (protocol.ReadFileResult, error) {
	var res protocol.ReadFileResult
	err := s.sb.Call(ctx, "read_file", protocol.ReadFileParams{Path: path, MaxBytes: maxBytes}, &res)
	return res, err
}

func (s *Session) RunCommand(ctx context.Context, command string, timeoutSec int) (protocol.RunCommandResult, error) {
	var res protocol.RunCommandResult
	err := s.sb.Call(ctx, "run_command", protocol.RunCommandParams{Command: command, Timeout: timeoutSec}, &res)
	return res, err
}

func (s *Session) Close() error {
	if s.sb == nil {
		return nil
	}
	return s.sb.Stop()
}
