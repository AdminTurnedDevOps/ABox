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

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/runtime"
	"github.com/AdminTurnedDevOps/ABox/internal/session"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

type uiMode int

const (
	modeChat uiMode = iota
	modeProviderPick
	modeProviderKey
	modeMCPPick
	modeMCPKey
)

type model struct {
	cfg            config.File
	sel            config.Model
	sandbox        *runtime.Sandbox
	ta             textarea.Model
	keyIn          textinput.Model
	mode           uiMode
	slashSel       int
	provSel        int
	provPick       config.ProviderProfile
	mcpSel         int
	mcpPick        config.MCPServer
	log            []string
	transcriptPath string
	width          int
	height         int
	busy           bool
	vmState        string
	err            string
	cancel         context.CancelFunc
	events         <-chan protocol.AgentEvent
}

type evMsg protocol.AgentEvent
type errMsg error
type doneMsg struct{}

func New(cfg config.File, sel config.Model, sb *runtime.Sandbox, vmState string, log []string, transcriptPath string) model {
	ta := textarea.New()
	ta.Placeholder = "Ask ABox Anything"
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
	return model{cfg: cfg, sel: sel, sandbox: sb, ta: ta, keyIn: ki, vmState: vmState, log: log, transcriptPath: transcriptPath}
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
			if m.mode == modeMCPPick {
				if m.mcpSel > 0 {
					m.mcpSel--
				}
				return m, nil
			}
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
			if m.mode == modeMCPPick {
				if m.mcpSel < len(mcpServers(m.cfg))-1 {
					m.mcpSel++
				}
				return m, nil
			}
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
			if m.mode == modeMCPPick {
				if m.mcpSel > 0 {
					m.mcpSel--
				}
				return m, nil
			}
			if m.mode == modeProviderPick {
				if m.provSel > 0 {
					m.provSel--
				}
				return m, nil
			}
		case "j":
			if m.mode == modeMCPPick {
				if m.mcpSel < len(mcpServers(m.cfg))-1 {
					m.mcpSel++
				}
				return m, nil
			}
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
			if m.mode == modeMCPPick {
				return m.acceptMCP()
			}
			if m.mode == modeMCPKey {
				return m.saveMCPKey()
			}
			return m.submit()
		}
	case evMsg:
		switch msg.Kind {
		case "text":
			m.appendLast(msg.Text)
		case "tool":
			m.log = append(m.log, formatToolLine(msg.Tool, msg.Status, msg.Text, msg.Err))
			m.saveTranscript()
		case "error":
			m.err = msg.Err
			m.log = append(m.log, "error: "+msg.Err)
			m.busy = false
			m.saveTranscript()
		case "done":
			m.busy = false
			m.saveTranscript()
		}
		if m.events != nil && msg.Kind != "done" && msg.Kind != "error" {
			return m, waitEvent(m.events)
		}
		return m, nil
	case errMsg:
		m.err = msg.Error()
		m.busy = false
		m.log = append(m.log, "error: "+m.err)
		m.saveTranscript()
		return m, nil
	case doneMsg:
		m.busy = false
		m.saveTranscript()
		return m, nil
	}
	if m.mode == modeProviderKey || m.mode == modeMCPKey {
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
	if m.sandbox == nil {
		m.err = "agent runs only in the microVM (vm not ready)"
		return m, nil
	}
	m.ta.Reset()
	m.log = append(m.log, "you: "+text, "")
	m.saveTranscript()
	m.busy = true
	m.err = ""
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	ch := make(chan protocol.AgentEvent, 32)
	m.events = ch
	go func() {
		_ = m.sandbox.UserTurn(ctx, text, func(e protocol.AgentEvent) { ch <- e })
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
	case "/mcp":
		m.mode = modeMCPPick
		m.mcpSel = 0
		m.err = ""
		return m, nil
	case "/help":
		m.log = append(m.log, "commands:")
		for _, c := range slashCommands {
			m.log = append(m.log, "  "+c.Name+"  "+c.Help)
		}
		m.saveTranscript()
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

func (m model) acceptMCP() (tea.Model, tea.Cmd) {
	servers := mcpServers(m.cfg)
	if m.mcpSel < 0 || m.mcpSel >= len(servers) {
		m.err = "no MCP servers configured"
		m.mode = modeChat
		return m, nil
	}
	m.mcpPick = servers[m.mcpSel]
	m.mode = modeMCPKey
	m.keyIn.SetValue("")
	m.keyIn.Focus()
	m.ta.Blur()
	m.err = ""
	return m, textinput.Blink
}

func (m model) saveMCPKey() (tea.Model, tea.Cmd) {
	key := strings.TrimSpace(m.keyIn.Value())
	if key == "" {
		m.err = "token is empty"
		return m, nil
	}
	env, err := applyMCPKey(m.mcpPick, key)
	m.keyIn.SetValue("")
	m.keyIn.Blur()
	m.ta.Focus()
	m.mode = modeChat
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	if m.sandbox != nil {
		if err := m.sandbox.SetMCPTokens(context.Background(), map[string]string{env: key}); err != nil {
			m.err = "saved on host but guest MCP update failed: " + err.Error()
			return m, nil
		}
	}
	m.err = ""
	m.log = append(m.log, "mcp "+m.mcpPick.Name+" token saved  (OAuth: abox mcp login "+m.mcpPick.Name+")")
	m.saveTranscript()
	return m, nil
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
	if m.sandbox != nil {
		secrets := map[string]string{m.provPick.Env: key}
		if err := m.sandbox.SetModel(context.Background(), sel, secrets); err != nil {
			m.err = "saved on host but guest agent update failed: " + err.Error()
			return m, nil
		}
	}
	m.err = ""
	m.log = append(m.log, "connected "+m.provPick.Label+"  (agent in microVM)")
	m.saveTranscript()
	return m, nil
}

func waitEvent(ch <-chan protocol.AgentEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return doneMsg{}
		}
		return evMsg(ev)
	}
}

func (m model) saveTranscript() {
	if m.transcriptPath == "" {
		return
	}
	_ = session.WriteTranscript(m.transcriptPath, m.log)
}

func LogFromHistory(h []protocol.HistoryLine) []string {
	var log []string
	for _, line := range h {
		switch line.Kind {
		case "user":
			log = append(log, "you: "+line.Text, "")
		case "text":
			if line.Text != "" {
				log = append(log, line.Text)
			}
		case "tool":
			log = append(log, formatToolLine(line.Tool, line.Status, line.Text, line.Err))
		}
	}
	return log
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

	cred := "no key"
	if m.sel.CredentialPresent() {
		cred = "key ok"
	}
	header := bar.Render(fmt.Sprintf("ABox  %s/%s  vm:%s  net:%s  %s", m.sel.Provider, m.sel.Model, m.vmState, m.cfg.Connectivity.Mode, cred))

	wrapW := m.width
	if wrapW <= 0 {
		wrapW = 80
	}
	bodyH := max(3, m.height-8)
	if m.height == 0 {
		bodyH = 16
	}
	wrapped := wrapLog(m.log, wrapW)
	body := strings.Join(tail(wrapped, bodyH), "\n")
	if body == "" {
		body = muted.Render("Type / for commands. /provider sets Grok, OpenAI, or Anthropic keys.")
	}

	composer := m.ta.View()
	extra := ""
	footer := ""
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
	case modeProviderKey:
		composer = "API key for " + m.provPick.Label + "\n" + m.keyIn.View()
	case modeMCPPick:
		servers := mcpServers(m.cfg)
		var b strings.Builder
		b.WriteString("MCP servers  (OAuth: abox mcp login <name>)\n")
		if len(servers) == 0 {
			b.WriteString("  none configured\n")
		}
		for i, s := range servers {
			mark := "  "
			if i == m.mcpSel {
				mark = "> "
			}
			status := "no token"
			env := config.TokenEnv(s)
			if env != "" && strings.TrimSpace(os.Getenv(env)) != "" {
				status = "token ok"
			}
			b.WriteString(mark + s.Name + "  " + s.URL + "  " + status + "\n")
		}
		composer = b.String()
	case modeMCPKey:
		composer = "Bearer token for " + m.mcpPick.Name + "\n" + m.keyIn.View()
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
	if m.err != "" {
		footer = lipgloss.NewStyle().Foreground(lipgloss.Color("#B54A4A")).Render(m.err)
	}

	parts := []string{header, "", body, "", extra + composer}
	if footer != "" {
		parts = append(parts, footer)
	}
	content := canvas.Render(strings.Join(parts, "\n"))
	v := tea.NewView(content)
	v.AltScreen = true
	v.BackgroundColor = lipgloss.Color("#050505")
	return v
}

func formatToolLine(tool, status, text, errText string) string {
	switch {
	case status == "error" && errText != "":
		return "  ▸ " + tool + " failed: " + errText
	case tool == "search" && (text == "" || text == "no matches" || text == "null"):
		return "  ▸ searched the guest repo (no files matched)"
	case text == "" || text == "null":
		return "  ▸ " + tool
	default:
		return "  ▸ " + tool + "  " + text
	}
}

func wrapLog(lines []string, width int) []string {
	if width < 8 {
		width = 8
	}
	var out []string
	for _, line := range lines {
		out = append(out, wrapLine(line, width)...)
	}
	return out
}

func wrapLine(s string, width int) []string {
	s = strings.ReplaceAll(s, "\t", "    ")
	if s == "" {
		return []string{""}
	}
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		for len(para) > width {
			cut := strings.LastIndex(para[:width], " ")
			if cut < width/4 {
				cut = width
			}
			lines = append(lines, strings.TrimRight(para[:cut], " "))
			para = strings.TrimLeft(para[cut:], " ")
		}
		lines = append(lines, para)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
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

func Run(cfg config.File, sel config.Model, sb *runtime.Sandbox, vmState string, log []string, transcriptPath string) error {
	p := tea.NewProgram(New(cfg, sel, sb, vmState, log, transcriptPath))
	_, err := p.Run()
	return err
}
