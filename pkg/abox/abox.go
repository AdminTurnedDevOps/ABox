package abox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/credsource"
	"github.com/AdminTurnedDevOps/ABox/internal/hostbroker"
	"github.com/AdminTurnedDevOps/ABox/internal/repository"
	"github.com/AdminTurnedDevOps/ABox/internal/runtime"
	"github.com/AdminTurnedDevOps/ABox/internal/session"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

// ErrGuestTooOld is returned when an operation requires a protocol-2+ guest.
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
	n, scrubErr := session.ScrubSecretsEverywhere()
	if n > 0 {
		fmt.Fprintf(os.Stderr, "abox: scrubbed plaintext secrets from %d old session(s)\n", n)
	}
	if scrubErr != nil {
		return nil, fmt.Errorf("scrub legacy session secrets: %w", scrubErr)
	}
	cfg, cfgPath, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	resolver := credsource.NewResolver()
	sel, ok := cfg.ModelNamed(opts.Model)
	if !ok {
		resolver.Close()
		return nil, fmt.Errorf("no model profile %q (config %s)", opts.Model, cfgPath)
	}
	if err := os.MkdirAll(config.SessionRoot(), 0o700); err != nil {
		resolver.Close()
		return nil, err
	}

	var sess *session.Session
	var snap repository.Snapshot
	if resume {
		loaded, err := loadResume(opts.RepoPath, resumeID)
		if err != nil {
			resolver.Close()
			return nil, err
		}
		sess = loaded
	} else {
		created, err := session.Create(opts.RepoPath, "pending")
		if err != nil {
			resolver.Close()
			return nil, fmt.Errorf("create session: %w", err)
		}
		sess = created
		opened, err := repository.OpenForSession(opts.RepoPath, filepath.Join(sess.Dir, "host-tree"))
		if err != nil {
			resolver.Close()
			return nil, fmt.Errorf("snapshot repo: %w", err)
		}
		snap = opened
		sess.RepoRoot = snap.Root
		sess.HEAD = snap.HEAD
		if err := sess.WriteMeta(); err != nil {
			resolver.Close()
			return nil, err
		}
	}

	image := opts.Image
	if image == "" {
		image = cfg.Runtime.Image
	}
	if err := runtime.Prepare(sess, image, sel, resume); err != nil {
		resolver.Close()
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
		resolver.Close()
		return nil, fmt.Errorf("start vm: %w", err)
	}
	if sb.GuestProtocol < 2 {
		sb.Stop()
		resolver.Close()
		if resume {
			return nil, fmt.Errorf("%w: cannot resume protocol-1 session %q after secretless config rewrite; rebuild the guest image and start a new session", ErrGuestTooOld, sess.ID)
		}
		return nil, fmt.Errorf("%w: protocol-1 guest cannot use the secretless config; rebuild the guest image", ErrGuestTooOld)
	}
	if sb.GuestProtocol < 4 {
		sb.Stop()
		resolver.Close()
		return nil, fmt.Errorf("%w: guest protocol %d cannot enforce brokered MCP and command approvals; rebuild the guest image and start a new session", ErrGuestTooOld, sb.GuestProtocol)
	}
	broker, err := hostbroker.New(cfg, sel, resolver)
	if err != nil {
		sb.Stop()
		resolver.Close()
		return nil, err
	}
	sb.SetGuestCallHandler(broker)
	if !resume {
		archive, err := repository.ArchiveHEAD(snap.Root)
		if err != nil {
			sb.Stop()
			resolver.Close()
			return nil, fmt.Errorf("archive repo: %w", err)
		}
		if err := sb.TransferArchive(ctx, archive); err != nil {
			sb.Stop()
			resolver.Close()
			return nil, fmt.Errorf("transfer repo: %w", err)
		}
	}
	return &Session{cfg: cfg, sess: sess, sb: sb, sel: sel, resolver: resolver, broker: broker}, nil
}

func credentialStartupError(resolveErr, pushErr error) error {
	var errs []error
	if resolveErr != nil {
		errs = append(errs, fmt.Errorf("resolve credentials: %w", resolveErr))
	}
	if pushErr != nil {
		errs = append(errs, fmt.Errorf("push resolved credentials: %w", pushErr))
	}
	return errors.Join(errs...)
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
	Approvals   bool
	MCPBroker   bool
}

type ApprovalDecision uint8

const (
	ApprovalDeny ApprovalDecision = iota
	ApprovalAllowOnce
)

type ApprovalRequest struct {
	Tool       string
	ToolID     string
	Command    string
	WorkDir    string
	TimeoutSec int
}

type Approver interface {
	Approve(context.Context, ApprovalRequest) (ApprovalDecision, error)
}

type ApproverFunc func(context.Context, ApprovalRequest) (ApprovalDecision, error)

func (f ApproverFunc) Approve(ctx context.Context, request ApprovalRequest) (ApprovalDecision, error) {
	return f(ctx, request)
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
	mu       sync.RWMutex
	cfg      config.File
	sess     *session.Session
	sb       *runtime.Sandbox
	sel      config.Model
	resolver *credsource.Resolver
	broker   *hostbroker.Broker
}

func (s *Session) ID() string { return s.sess.ID }

func (s *Session) Capabilities() Capabilities {
	p := s.sb.GuestProtocol
	return Capabilities{
		Protocol:    p,
		Cancel:      p >= 2,
		RichEvents:  p >= 2,
		TurnOptions: p >= 2,
		Approvals:   p >= 4,
		MCPBroker:   p >= 4,
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
	s.mu.RLock()
	defer s.mu.RUnlock()
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
		res = turnResult(out)
	}
	if err != nil && errors.Is(err, runtime.ErrGuestTooOld) {
		return res, fmt.Errorf("%w: %v", ErrGuestTooOld, err)
	}
	return res, err
}

func turnResult(out *runtime.TurnOutcome) *TurnResult {
	if out == nil {
		return &TurnResult{}
	}
	return &TurnResult{Usage: out.Usage, StopReason: out.StopReason, Canceled: out.Canceled}
}

func (s *Session) SetModel(ctx context.Context, model string) error {
	if !s.mu.TryLock() {
		return fmt.Errorf("cannot set model while a turn or model update is in progress")
	}
	defer s.mu.Unlock()
	if s.sb.GuestProtocol < 2 {
		return fmt.Errorf("%w: set model requires protocol 2, guest speaks %d", ErrGuestTooOld, s.sb.GuestProtocol)
	}
	cfg, _, err := config.Load()
	if err != nil {
		return fmt.Errorf("reload config: %w", err)
	}
	sel, ok := cfg.ModelNamed(model)
	if !ok {
		return fmt.Errorf("no model profile %q", model)
	}
	var secrets map[string]string
	if s.sb.GuestProtocol == 2 {
		ref := sel.CredentialReference()
		val, err := s.resolver.Resolve(ctx, credsource.FromConfig(ref))
		if err != nil {
			return fmt.Errorf("credential for model %q (%s %s): %w", sel.Name, ref.Source, ref.Name, err)
		}
		secrets = map[string]string{sel.EnvName(): string(val.Bytes)}
		val.Zero()
	}
	if err := s.sb.SetModel(ctx, sel, secrets); err != nil {
		return err
	}
	s.broker.UpdateModel(cfg, sel)
	s.cfg = cfg
	s.sel = sel
	return nil
}

func (s *Session) SetMCPTokens(ctx context.Context, secrets map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.broker.SetMCPTokens(secrets)
}

func (s *Session) SetApprover(approver Approver) error {
	if !s.mu.TryLock() {
		return fmt.Errorf("cannot set approver while a turn or model update is in progress")
	}
	defer s.mu.Unlock()
	if approver == nil {
		return s.sb.SetRunCommandApprover(nil)
	}
	return s.sb.SetRunCommandApprover(runtime.RunCommandApproverFunc(func(ctx context.Context, params protocol.RunCommandApprovalParams) (protocol.ApprovalDecision, error) {
		decision, err := approver.Approve(ctx, ApprovalRequest{
			Tool: "run_command", ToolID: params.ToolID, Command: params.Command,
			WorkDir: params.WorkDir, TimeoutSec: params.TimeoutSec,
		})
		if err != nil || decision != ApprovalAllowOnce {
			return protocol.ApprovalDeny, err
		}
		return protocol.ApprovalAllowOnce, nil
	}))
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
	err := s.sb.Stop()
	if s.broker != nil {
		err = errors.Join(err, s.broker.Close())
	}
	s.resolver.Close()
	return err
}
