package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/AdminTurnedDevOps/ABox/internal/agent"
	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/runtime"
)

type model struct {
	cfg     config.File
	sel     config.Model
	sandbox *runtime.Sandbox
	loop    *agent.Loop
	ta      textarea.Model
	log     []string
	width   int
	height  int
	busy    bool
	vmState string
	err     string
	cancel  context.CancelFunc
	events  <-chan agent.UIEvent
}

type evMsg agent.UIEvent
type errMsg error
type doneMsg struct{}

func New(cfg config.File, sel config.Model, sb *runtime.Sandbox, vmState string) model {
	ta := textarea.New()
	ta.Placeholder = "Ask ABox to work in the guest…"
	ta.Focus()
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	loop := &agent.Loop{Model: sel, Sandbox: sb}
	return model{cfg: cfg, sel: sel, sandbox: sb, loop: loop, ta: ta, vmState: vmState}
}

func (m model) Init() tea.Cmd { return textarea.Blink }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ta.SetWidth(max(20, m.width-4))
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "ctrl+d":
			return m, tea.Quit
		case "ctrl+j", "alt+enter":
			if m.busy {
				return m, nil
			}
			return m.submit()
		}
	case evMsg:
		switch msg.Kind {
		case "text":
			m.appendLast(msg.Text)
		case "tool":
			m.log = append(m.log, fmt.Sprintf("  ▸ %s %s %s", msg.Tool, msg.Status, msg.Text))
		case "error":
			m.err = msg.Err
			m.log = append(m.log, "error: "+msg.Err)
			m.busy = false
		case "done":
			m.busy = false
		}
		if m.events != nil && msg.Kind != "done" && msg.Kind != "error" {
			return m, waitEvent(m.events)
		}
		return m, nil
	case errMsg:
		m.err = msg.Error()
		m.busy = false
		m.log = append(m.log, "error: "+m.err)
		return m, nil
	case doneMsg:
		m.busy = false
		return m, nil
	}
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return m, cmd
}

func (m model) submit() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.ta.Value())
	if text == "" {
		return m, nil
	}
	m.ta.Reset()
	m.log = append(m.log, "you: "+text, "")
	m.busy = true
	m.err = ""
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	ch := make(chan agent.UIEvent, 32)
	m.events = ch
	m.loop.OnEvent = func(e agent.UIEvent) { ch <- e }
	go func() {
		_ = m.loop.Turn(ctx, text)
		close(ch)
	}()
	return m, waitEvent(ch)
}

func waitEvent(ch <-chan agent.UIEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return doneMsg{}
		}
		return evMsg(ev)
	}
}

func (m *model) appendLast(s string) {
	if len(m.log) == 0 {
		m.log = append(m.log, s)
		return
	}
	last := m.log[len(m.log)-1]
	if strings.HasPrefix(last, "you:") || strings.HasPrefix(last, "  ▸") || strings.HasPrefix(last, "error:") {
		m.log = append(m.log, s)
		return
	}
	m.log[len(m.log)-1] = last + s
}

func (m model) View() tea.View {
	canvas := lipgloss.NewStyle().Foreground(lipgloss.Color("#F4F4F5")).Background(lipgloss.Color("#050505"))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#71717A"))
	bar := lipgloss.NewStyle().Foreground(lipgloss.Color("#F4F4F5")).Background(lipgloss.Color("#141416")).Padding(0, 1)
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color("#D4A017"))

	cred := "no key"
	if m.sel.CredentialPresent() {
		cred = "key ok"
	}
	header := bar.Render(fmt.Sprintf("ABox  %s/%s  vm:%s  net:%s  %s", m.sel.Provider, m.sel.Model, m.vmState, m.cfg.Connectivity.Mode, cred))
	notice := warn.Render("experimental · isolation Planned until hardware tests pass · tools run only in the guest")

	bodyH := max(3, m.height-10)
	if m.height == 0 {
		bodyH = 16
	}
	body := strings.Join(tail(m.log, bodyH), "\n")
	if body == "" {
		body = muted.Render("Prompt when ready. Enter is newline; ctrl+j sends. Tools never execute on the host.")
	}

	footer := muted.Render("ctrl+j send  ·  ctrl+c quit  ·  default action is non-destructive")
	if m.busy {
		footer = muted.Render("running…  ctrl+c cancel")
	}
	if m.err != "" {
		footer = lipgloss.NewStyle().Foreground(lipgloss.Color("#B54A4A")).Render(m.err)
	}

	content := canvas.Render(strings.Join([]string{header, notice, "", body, "", m.ta.View(), footer}, "\n"))
	v := tea.NewView(content)
	v.AltScreen = true
	v.BackgroundColor = lipgloss.Color("#050505")
	return v
}

func tail(in []string, n int) []string {
	if len(in) <= n {
		return in
	}
	return in[len(in)-n:]
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func Run(cfg config.File, sel config.Model, sb *runtime.Sandbox, vmState string) error {
	p := tea.NewProgram(New(cfg, sel, sb, vmState))
	_, err := p.Run()
	return err
}
