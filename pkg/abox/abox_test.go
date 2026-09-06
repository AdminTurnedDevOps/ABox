package abox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/runtime"
	"github.com/AdminTurnedDevOps/ABox/internal/session"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

func TestErrGuestTooOldIsRuntime(t *testing.T) {
	if !errors.Is(ErrGuestTooOld, runtime.ErrGuestTooOld) {
		t.Fatal("SDK error must wrap runtime.ErrGuestTooOld")
	}
}

func TestOptionsDefaults(t *testing.T) {
	o := Options{}.withDefaults()
	if o.RepoPath == "" {
		t.Fatal("repo path should default to the working directory")
	}
	if o.BootTimeout != 45*time.Second {
		t.Fatalf("boot timeout %s", o.BootTimeout)
	}
}

func TestOptionsDefaultsPreserveValues(t *testing.T) {
	o := Options{RepoPath: "/repo", BootTimeout: 2 * time.Minute}.withDefaults()
	if o.RepoPath != "/repo" || o.BootTimeout != 2*time.Minute {
		t.Fatalf("defaults changed explicit values: %+v", o)
	}
}

func TestSessionMetadataAndCapabilities(t *testing.T) {
	history := []protocol.HistoryLine{{Kind: "text", Text: "hello"}}
	s := &Session{
		sess: &session.Session{ID: "session-id"},
		sb:   &runtime.Sandbox{GuestProtocol: 2, History: history},
	}
	if s.ID() != "session-id" {
		t.Fatalf("id %q", s.ID())
	}
	if got := s.Capabilities(); got.Protocol != 2 || !got.Cancel || !got.RichEvents || !got.TurnOptions {
		t.Fatalf("capabilities %+v", got)
	}
	if got := s.History(); len(got) != 1 || got[0].Text != "hello" {
		t.Fatalf("history %+v", got)
	}
}

func TestTurnOptsRejectsOldGuest(t *testing.T) {
	s := &Session{sb: &runtime.Sandbox{GuestProtocol: 1}}
	res, err := s.TurnOpts(context.Background(), "hi", TurnOpts{MaxTurns: 2}, nil)
	if res != nil || !errors.Is(err, ErrGuestTooOld) {
		t.Fatalf("result=%+v err=%v", res, err)
	}
}

func TestTurnResultPreservesUsage(t *testing.T) {
	usage := &protocol.UsageInfo{InputTokens: 11, OutputTokens: 5}
	got := turnResult(&runtime.TurnOutcome{Usage: usage, StopReason: "end_turn", Canceled: true})
	if got.Usage == nil || got.Usage.InputTokens != 11 || got.Usage.OutputTokens != 5 {
		t.Fatalf("usage %+v", got.Usage)
	}
	if got.StopReason != "end_turn" || !got.Canceled {
		t.Fatalf("result %+v", got)
	}
}

func TestSetModelRejectsProtocolOne(t *testing.T) {
	s := &Session{sb: &runtime.Sandbox{GuestProtocol: 1}}
	if err := s.SetModel(context.Background(), "grok-default"); !errors.Is(err, ErrGuestTooOld) {
		t.Fatalf("got %v", err)
	}
}

func TestSetModelRejectsConcurrentTurn(t *testing.T) {
	s := &Session{sb: &runtime.Sandbox{GuestProtocol: 3}}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := s.SetModel(context.Background(), "grok-default"); err == nil {
		t.Fatal("expected in-progress error")
	}
}

func TestCredentialStartupErrorReportsResolveAndPushFailures(t *testing.T) {
	resolveErr := errors.New("model credential unavailable")
	pushErr := errors.New("mcp token push failed")
	err := credentialStartupError(resolveErr, pushErr)
	if !errors.Is(err, resolveErr) || !errors.Is(err, pushErr) {
		t.Fatalf("combined error %v", err)
	}
	if err == nil || err.Error() != "resolve credentials: model credential unavailable\npush resolved credentials: mcp token push failed" {
		t.Fatalf("error text %q", err)
	}
}

func TestCredentialStartupErrorAllowsSuccessfulPartialPush(t *testing.T) {
	resolveErr := errors.New("one credential unavailable")
	err := credentialStartupError(resolveErr, nil)
	if !errors.Is(err, resolveErr) {
		t.Fatalf("error %v", err)
	}
}

func TestOpenReturnsLegacySessionScrubError(t *testing.T) {
	t.Setenv("ABOX_HOME", t.TempDir())
	legacy := filepath.Join(config.SessionRoot(), "legacy")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "guest-config.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	sess, err := Open(context.Background(), Options{RepoPath: t.TempDir()})
	if sess != nil || err == nil || !strings.Contains(err.Error(), "scrub legacy session secrets") {
		t.Fatalf("session=%v error=%v", sess, err)
	}
}

func TestLoadResumeByID(t *testing.T) {
	t.Setenv("ABOX_HOME", t.TempDir())
	created, err := session.Create(t.TempDir(), "head")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created.RootDisk(), []byte("disk"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadResume("ignored", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID {
		t.Fatalf("id %q", got.ID)
	}
}

func TestLoadResumeLatestForRepo(t *testing.T) {
	t.Setenv("ABOX_HOME", t.TempDir())
	repo := t.TempDir()
	created, err := session.Create(repo, "head")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created.RootDisk(), []byte("disk"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadResume(filepath.Join(repo, "."), "")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID {
		t.Fatalf("id %q", got.ID)
	}
}
