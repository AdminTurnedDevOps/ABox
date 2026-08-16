package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/AdminTurnedDevOps/ABox/internal/agent"
	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/runtime"
)

type uiMode int

const (
	modeChat uiMode = iota
	modeProviderPick
	modeProviderKey
)

type model struct {
	cfg      config.File
	sel      config.Model
	sandbox  *runtime.Sandbox
	loop     *agent.Loop
	ta       textarea.Model
	keyIn    textinput.Model
	mode     uiMode
	slashSel int
	provSel  int
	provPick providerChoice
	log      []string
	width    int
	height   int
	busy     bool
	vmState  string
	err      string
	cancel   context.CancelFunc
	events   <-chan agent.UIEvent
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
	ta.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("shift+enter", "alt+enter"),
		key.WithHelp("shift+enter", "newline"),
	)
	ki := textinput.New()
	ki.EchoMode = textinput.EchoPassword
	ki.EchoCharacter = '•'
	ki.Placeholder = "paste API key"
	ki.Prompt = "key> "
	loop := &agent.Loop{Model: sel, Sandbox: sb}
	return model{cfg: cfg, sel: sel, sandbox: sb, loop: loop, ta: ta, keyIn: ki, vmState: vmState}
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
			if m.mode != modeChat {
				m.mode = modeChat
				m.keyIn.Blur()
				m.ta.Focus()
				m.err = ""
				return m, nil
			}
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "ctrl+d":
			return m, tea.Quit
		case "esc":
			if m.mode != modeChat {
				m.mode = modeChat
				m.keyIn.Blur()
				m.ta.Focus()
				m.err = ""
				return m, nil
			}
		case "up":
			if m.mode == modeProviderPick {
				if m.provSel > 0 {
					m.provSel--
				}
				return m, nil
			}
			if m.showingSlash() && m.slashSel > 0 {
				m.slashSel--
				return m, nil
			}
		case "down":
			if m.mode == modeProviderPick {
				if m.provSel < len(providerChoices())-1 {
					m.provSel++
				}
				return m, nil
			}
			if cmds := filterSlash(m.ta.Value()); m.showingSlash() && m.slashSel < len(cmds)-1 {
				m.slashSel++
				return m, nil
			}
		case "k":
			if m.mode == modeProviderPick {
				if m.provSel > 0 {
					m.provSel--
				}
				return m, nil
			}
		case "j":
			if m.mode == modeProviderPick {
				if m.provSel < len(providerChoices())-1 {
					m.provSel++
				}
				return m, nil
			}
		case "enter", "ctrl+m":
			if m.busy {
				return m, nil
			}
			if m.mode == modeProviderPick {
				return m.acceptProvider()
			}
			if m.mode == modeProviderKey {
				return m.saveProviderKey()
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
	if m.mode == modeProviderKey {
		var cmd tea.Cmd
		m.keyIn, cmd = m.keyIn.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	if m.showingSlash() {
		cmds := filterSlash(m.ta.Value())
		if m.slashSel >= len(cmds) {
			m.slashSel = max(0, len(cmds)-1)
		}
	}
	return m, cmd
}

func (m model) showingSlash() bool {
	return m.mode == modeChat && strings.HasPrefix(m.ta.Value(), "/")
}

func (m model) submit() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.ta.Value())
	if text == "" {
		return m, nil
	}
	if strings.HasPrefix(text, "/") {
		return m.runSlash(text)
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

func (m model) runSlash(text string) (tea.Model, tea.Cmd) {
	name := strings.Fields(text)[0]
	if cmds := filterSlash(text); len(cmds) > 0 && (name == "/" || !slashExact(name)) {
		name = cmds[m.slashSel].Name
	}
	m.ta.Reset()
	m.slashSel = 0
	switch name {
	case "/provider":
		m.mode = modeProviderPick
		m.provSel = 0
		m.err = ""
		return m, nil
	case "/help":
		m.log = append(m.log, "commands:")
		for _, c := range slashCommands {
			m.log = append(m.log, "  "+c.Name+"  "+c.Help)
		}
		return m, nil
	default:
		m.err = "unknown command " + name + "  (try /provider)"
		return m, nil
	}
}

func slashExact(name string) bool {
	for _, c := range slashCommands {
		if c.Name == name {
			return true
		}
	}
	return false
}

func (m model) acceptProvider() (tea.Model, tea.Cmd) {
	choices := providerChoices()
	if m.provSel < 0 || m.provSel >= len(choices) {
		return m, nil
	}
	m.provPick = choices[m.provSel]
	m.mode = modeProviderKey
	m.keyIn.SetValue("")
	m.keyIn.Focus()
	m.ta.Blur()
	m.err = ""
	return m, textinput.Blink
}

func (m model) saveProviderKey() (tea.Model, tea.Cmd) {
	key := strings.TrimSpace(m.keyIn.Value())
	if key == "" {
		m.err = "API key is empty"
		return m, nil
	}
	sel, err := applyProviderKey(m.cfg, m.provPick, key)
	m.keyIn.SetValue("")
	m.keyIn.Blur()
	m.ta.Focus()
	m.mode = modeChat
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.sel = sel
	m.loop.Model = sel
	m.err = ""
	m.log = append(m.log, "connected "+m.provPick.Label+"  (key stored locally, not logged)")
	return m, nil
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
		body = muted.Render("Type / for commands. /provider sets Grok, OpenAI, or Anthropic keys. Enter sends.")
	}

	composer := m.ta.View()
	extra := ""
	footer := muted.Render("enter send  ·  /provider  ·  ctrl+c quit")
	switch m.mode {
	case modeProviderPick:
		var b strings.Builder
		b.WriteString("Select provider\n")
		for i, p := range providerChoices() {
			mark := "  "
			if i == m.provSel {
				mark = "> "
			}
			status := "no key"
			if p.Env != "" && strings.TrimSpace(os.Getenv(p.Env)) != "" {
				status = "key ok"
			}
			b.WriteString(mark + p.Label + "  " + status + "\n")
		}
		composer = b.String()
		footer = muted.Render("enter select  ·  esc back")
	case modeProviderKey:
		composer = "API key for " + m.provPick.Label + "\n" + m.keyIn.View()
		footer = muted.Render("enter save  ·  esc cancel  ·  key is not shown in the log")
	default:
		if m.showingSlash() {
			var b strings.Builder
			cmds := filterSlash(m.ta.Value())
			for i, c := range cmds {
				mark := "  "
				if i == m.slashSel {
					mark = "> "
				}
				b.WriteString(mark + c.Name + "  " + c.Help + "\n")
			}
			if b.Len() == 0 {
				b.WriteString("  no matching commands\n")
			}
			extra = b.String()
		}
	}
	if m.busy {
		footer = muted.Render("running…  ctrl+c cancel")
	}
	if m.err != "" {
		footer = lipgloss.NewStyle().Foreground(lipgloss.Color("#B54A4A")).Render(m.err)
	}

	content := canvas.Render(strings.Join([]string{header, notice, "", body, "", extra + composer, footer}, "\n"))
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
