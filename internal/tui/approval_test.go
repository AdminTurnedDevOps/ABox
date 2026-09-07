package tui

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

func TestApprovalDefaultsToDenyAndShowsExactRequest(t *testing.T) {
	command := "printf 'first\\nsecond' && " + strings.Repeat("x", 120) + "-END"
	req := approvalRequest(context.Background(), protocol.RunCommandApprovalParams{
		Command: command, WorkDir: "src/path with space", TimeoutSec: 37,
	})
	m := New(config.Defaults(), config.Model{}, nil, nil, "ready", nil, "")
	m.width = 40

	updated, _ := m.Update(approvalMsg{req: req})
	m = updated.(model)
	if m.mode != modeApproval || m.approvalAllow {
		t.Fatalf("mode=%v allow=%v", m.mode, m.approvalAllow)
	}
	view := m.View().Content
	for _, want := range []string{strconv.Quote(command), `guest workdir: "src/path with space"`, "timeout: 37s", "[Deny]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("approval view missing %q:\n%s", want, view)
		}
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)
	if got := <-req.response; got != protocol.ApprovalDeny {
		t.Fatalf("decision=%q", got)
	}
}

func TestApprovalSelectionAllowsOnce(t *testing.T) {
	req := approvalRequest(context.Background(), protocol.RunCommandApprovalParams{Command: "go test ./..."})
	m := New(config.Defaults(), config.Model{}, nil, nil, "ready", nil, "")
	m.mode = modeApproval
	m.approvalReq = req

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m = updated.(model)
	if !m.approvalAllow {
		t.Fatal("right did not select allow once")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m = updated.(model)
	if m.approvalAllow {
		t.Fatal("left did not select deny")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = updated.(model)
	if !m.approvalAllow {
		t.Fatal("j did not select allow once")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	m = updated.(model)
	if m.approvalAllow {
		t.Fatal("k did not select deny")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)
	if got := <-req.response; got != protocol.ApprovalAllowOnce {
		t.Fatalf("decision=%q", got)
	}
	if m.mode != modeChat || m.approvalReq != nil {
		t.Fatalf("mode=%v request=%v", m.mode, m.approvalReq)
	}
}

func TestApprovalEscapeDeniesAndListensAgain(t *testing.T) {
	first := approvalRequest(context.Background(), protocol.RunCommandApprovalParams{Command: "first"})
	m := New(config.Defaults(), config.Model{}, nil, nil, "ready", nil, "")
	m.mode = modeApproval
	m.approvalReq = first

	updated, next := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(model)
	if got := <-first.response; got != protocol.ApprovalDeny {
		t.Fatalf("escape decision=%q", got)
	}
	if next == nil {
		t.Fatal("escape did not resume approval listening")
	}

	msgCh := make(chan tea.Msg, 1)
	go func() { msgCh <- next() }()
	decisionCh := make(chan protocol.ApprovalDecision, 1)
	go func() {
		decision, _ := m.approveRunCommand(context.Background(), protocol.RunCommandApprovalParams{Command: "second"})
		decisionCh <- decision
	}()

	var msg tea.Msg
	select {
	case msg = <-msgCh:
	case <-time.After(time.Second):
		t.Fatal("later approval was not delivered")
	}
	updated, _ = m.Update(msg)
	m = updated.(model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := <-decisionCh; got != protocol.ApprovalDeny {
		t.Fatalf("later decision=%q", got)
	}
}

func TestApprovalBridgeHonorsCancellation(t *testing.T) {
	m := model{approvals: make(chan *runCommandApprovalRequest)}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		decision, err := m.approveRunCommand(ctx, protocol.RunCommandApprovalParams{Command: "sleep 10"})
		if decision != protocol.ApprovalDeny {
			result <- &unexpectedDecisionError{decision: decision}
			return
		}
		result <- err
	}()
	req := <-m.approvals
	if cap(req.response) != 1 {
		t.Fatalf("response channel capacity=%d", cap(req.response))
	}
	cancel()
	if err := <-result; err != context.Canceled {
		t.Fatalf("error=%v", err)
	}
}

type unexpectedDecisionError struct {
	decision protocol.ApprovalDecision
}

func (e *unexpectedDecisionError) Error() string {
	return "unexpected approval decision " + string(e.decision)
}

func approvalRequest(ctx context.Context, params protocol.RunCommandApprovalParams) *runCommandApprovalRequest {
	return &runCommandApprovalRequest{
		ctx: ctx, params: params,
		response: make(chan protocol.ApprovalDecision, 1),
		settled:  make(chan struct{}),
	}
}
