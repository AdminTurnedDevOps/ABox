package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/credsource"
	"github.com/AdminTurnedDevOps/ABox/internal/hostbroker"
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
	modeCredSourcePick
	modeCredModelPick
	modeCredName
	modeApproval
)

type model struct {
	cfg            config.File
	sel            config.Model
	sandbox        *runtime.Sandbox
	hostBroker     *hostbroker.Broker
	ta             textarea.Model
	keyIn          textinput.Model
	mode           uiMode
	slashSel       int
	provSel        int
	provPick       config.ProviderProfile
	credSourceSel  int
	credSource     cloudCredSource
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
	resolver       *credsource.Resolver
	selKeyStatus   string
	provKeyStatus  map[string]string
	mcpKeyStatus   map[string]string
	approvalReq    *runCommandApprovalRequest
	approvalAllow  bool
	approvals      chan *runCommandApprovalRequest
	spin           spinner.Model
}

type evMsg protocol.AgentEvent
type errMsg error
type doneMsg struct{}
type approvalMsg struct{ req *runCommandApprovalRequest }
type approvalCanceledMsg struct{ req *runCommandApprovalRequest }

type runCommandApprovalRequest struct {
	ctx      context.Context
	params   protocol.RunCommandApprovalParams
	response chan protocol.ApprovalDecision
	settled  chan struct{}
}

// Presence is cached: the render path must not shell out to keychain or HTTP.
type credStatusMsg struct {
	sel     string
	prov    map[string]string
	mcp     map[string]string
	partial bool
}

func New(cfg config.File, sel config.Model, sb *runtime.Sandbox, broker *hostbroker.Broker, vmState string, log []string, transcriptPath string) model {
	ta := textarea.New()
	ta.Placeholder = "Ask ABox Anything"
	ta.Focus()
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	// Mark only the first line, so continuation rows read as one input field.
	ta.SetPromptFunc(2, func(info textarea.PromptInfo) string {
		if info.LineNumber == 0 {
			return "› "
		}
		return "  "
	})
	ta.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("shift+enter", "alt+enter"),
		key.WithHelp("shift+enter", "newline"),
	)
	ki := textinput.New()
	ki.EchoMode = textinput.EchoPassword
	ki.EchoCharacter = '•'
	ki.Placeholder = "paste API key"
	ki.Prompt = "key> "
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	return model{
		cfg: cfg, sel: sel, sandbox: sb, hostBroker: broker, ta: ta, keyIn: ki, spin: sp,
		vmState: vmState, log: log, transcriptPath: transcriptPath,
		approvals: make(chan *runCommandApprovalRequest),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, checkCredStatus(m.cfg, m.sel, m.resolver, false), waitApproval(m.approvals))
}

func checkCredStatus(cfg config.File, sel config.Model, r *credsource.Resolver, mcp bool) tea.Cmd {
	if r == nil {
		r = credsource.NewResolver()
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		msg := credStatusMsg{prov: map[string]string{}, mcp: map[string]string{}}
		msg.sel = credStatusLabel(ctx, r, sel.CredentialReference())
		for _, p := range providerChoices() {
			model, ok := cfg.ModelNamed(p.Name)
			if !ok {
				model = p.ModelConfig()
			}
			msg.prov[p.Name] = credStatusLabel(ctx, r, model.CredentialReference())
		}
		if mcp {
			for _, s := range mcpServers(cfg) {
				msg.mcp[s.Name] = credStatusLabel(ctx, r, s.CredentialReference())
			}
			msg.partial = false
		} else {
			msg.partial = true
		}
		return msg
	}
}

func credStatusLabel(ctx context.Context, r *credsource.Resolver, ref config.CredentialRef) string {
	if credsource.Present(ctx, r, credsource.FromConfig(ref)) {
		return "key ok"
	}
	return "no key"
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !m.busy {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ta.SetWidth(max(20, m.width-4))
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.mode == modeApproval {
				m.resolveApproval(protocol.ApprovalDeny)
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			}
			if m.mode != modeChat {
				m.cancelCredInput()
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
			if m.mode == modeApproval {
				m.resolveApproval(protocol.ApprovalDeny)
				return m, waitApproval(m.approvals)
			}
			if m.mode != modeChat {
				m.cancelCredInput()
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
			if m.mode == modeCredSourcePick {
				if m.credSourceSel > 0 {
					m.credSourceSel--
				}
				return m, nil
			}
			if m.mode == modeProviderPick || m.mode == modeCredModelPick {
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
			if m.mode == modeCredSourcePick {
				if m.credSourceSel < len(cloudCredentialChoices())-1 {
					m.credSourceSel++
				}
				return m, nil
			}
			if m.mode == modeProviderPick || m.mode == modeCredModelPick {
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
			if m.mode == modeApproval {
				m.approvalAllow = false
				return m, nil
			}
			if m.mode == modeMCPPick {
				if m.mcpSel > 0 {
					m.mcpSel--
				}
				return m, nil
			}
			if m.mode == modeCredSourcePick {
				if m.credSourceSel > 0 {
					m.credSourceSel--
				}
				return m, nil
			}
			if m.mode == modeProviderPick || m.mode == modeCredModelPick {
				if m.provSel > 0 {
					m.provSel--
				}
				return m, nil
			}
		case "j":
			if m.mode == modeApproval {
				m.approvalAllow = true
				return m, nil
			}
			if m.mode == modeMCPPick {
				if m.mcpSel < len(mcpServers(m.cfg))-1 {
					m.mcpSel++
				}
				return m, nil
			}
			if m.mode == modeCredSourcePick {
				if m.credSourceSel < len(cloudCredentialChoices())-1 {
					m.credSourceSel++
				}
				return m, nil
			}
			if m.mode == modeProviderPick || m.mode == modeCredModelPick {
				if m.provSel < len(providerChoices())-1 {
					m.provSel++
				}
				return m, nil
			}
		case "left":
			if m.mode == modeApproval {
				m.approvalAllow = false
				return m, nil
			}
		case "right":
			if m.mode == modeApproval {
				m.approvalAllow = true
				return m, nil
			}
		case "enter", "ctrl+m":
			if m.mode == modeApproval {
				decision := protocol.ApprovalDeny
				if m.approvalAllow {
					decision = protocol.ApprovalAllowOnce
				}
				m.resolveApproval(decision)
				return m, waitApproval(m.approvals)
			}
			if m.busy {
				return m, nil
			}
			if m.mode == modeProviderPick {
				return m.acceptProvider()
			}
			if m.mode == modeProviderKey {
				return m.saveProviderKey()
			}
			if m.mode == modeCredSourcePick {
				return m.acceptCredSource()
			}
			if m.mode == modeCredModelPick {
				return m.acceptCredModel()
			}
			if m.mode == modeCredName {
				return m.saveCloudCredential()
			}
			if m.mode == modeMCPPick {
				return m.acceptMCP()
			}
			if m.mode == modeMCPKey {
				return m.saveMCPKey()
			}
			return m.submit()
		}
	case credStatusMsg:
		m.selKeyStatus = msg.sel
		for name, status := range msg.prov {
			if m.provKeyStatus == nil {
				m.provKeyStatus = map[string]string{}
			}
			m.provKeyStatus[name] = status
		}
		if !msg.partial {
			m.mcpKeyStatus = msg.mcp
		}
		return m, nil
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
	case approvalMsg:
		if msg.req.ctx.Err() != nil {
			m.resolveRequest(msg.req, protocol.ApprovalDeny)
			return m, waitApproval(m.approvals)
		}
		m.approvalReq = msg.req
		m.approvalAllow = false
		m.mode = modeApproval
		m.ta.Blur()
		return m, waitApprovalCancellation(msg.req)
	case approvalCanceledMsg:
		if m.approvalReq == msg.req {
			m.resolveApproval(protocol.ApprovalDeny)
			return m, waitApproval(m.approvals)
		}
		return m, nil
	}
	if m.mode == modeProviderKey || m.mode == modeMCPKey || m.mode == modeCredName {
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
		_, _ = m.sandbox.UserTurnCtx(ctx, text, runtime.TurnOptions{}, func(e protocol.AgentEvent) {
			select {
			case ch <- e:
			case <-ctx.Done():
			}
		})
		close(ch)
	}()
	return m, tea.Batch(waitEvent(ch), m.spin.Tick)
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
		if len(m.provKeyStatus) == 0 {
			return m, checkCredStatus(m.cfg, m.sel, m.resolver, false)
		}
		return m, nil
	case "/credential":
		m.mode = modeCredSourcePick
		m.credSourceSel = 0
		m.provSel = 0
		m.err = ""
		return m, nil
	case "/mcp":
		m.mode = modeMCPPick
		m.mcpSel = 0
		m.err = ""
		return m, checkCredStatus(m.cfg, m.sel, m.resolver, true)
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

func (m *model) cancelCredInput() {
	m.keyIn.SetValue("")
	m.keyIn.EchoMode = textinput.EchoPassword
	m.keyIn.EchoCharacter = '•'
	m.keyIn.Placeholder = "paste API key"
	m.keyIn.Prompt = "key> "
}

func (m model) acceptCredSource() (tea.Model, tea.Cmd) {
	choices := cloudCredentialChoices()
	if m.credSourceSel < 0 || m.credSourceSel >= len(choices) {
		return m, nil
	}
	m.credSource = choices[m.credSourceSel]
	m.mode = modeCredModelPick
	m.provSel = 0
	m.err = ""
	return m, nil
}

func (m model) acceptCredModel() (tea.Model, tea.Cmd) {
	choices := providerChoices()
	if m.provSel < 0 || m.provSel >= len(choices) {
		return m, nil
	}
	m.provPick = choices[m.provSel]
	m.mode = modeCredName
	m.keyIn.EchoMode = textinput.EchoNormal
	m.keyIn.Placeholder = m.credSource.Placeholder
	m.keyIn.Prompt = m.credSource.Prompt
	m.keyIn.SetValue("")
	m.keyIn.Focus()
	m.ta.Blur()
	m.err = ""
	return m, textinput.Blink
}

func (m model) saveCloudCredential() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.keyIn.Value())
	m.cancelCredInput()
	m.keyIn.Blur()
	m.ta.Focus()
	m.mode = modeChat
	if name == "" {
		m.err = "credential name is empty"
		return m, nil
	}
	cfg, sel, note, err := applyCloudCredential(m.cfg, m.provPick, config.CredentialRef{
		Source: m.credSource.Source, Name: name,
	})
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.cfg = cfg
	if m.sandbox != nil {
		if err := m.sandbox.SetModel(context.Background(), sel, nil); err != nil {
			m.err = "saved on host but guest agent update failed: " + err.Error()
			return m, nil
		}
	}
	m.updateHostBroker(cfg, sel)
	m.sel = sel
	m.err = ""
	m.log = append(m.log, "credential "+m.provPick.Label+" -> "+m.credSource.Label+" "+name+"  ("+note+")")
	m.saveTranscript()
	return m, checkCredStatus(m.cfg, m.sel, m.resolver, false)
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
	cfg, env, note, err := applyMCPKey(m.cfg, m.mcpPick, key)
	m.keyIn.SetValue("")
	m.keyIn.Blur()
	m.ta.Focus()
	m.mode = modeChat
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.cfg = cfg
	m.mcpKeyStatus = map[string]string{m.mcpPick.Name: "key ok"}
	if m.hostBroker != nil {
		if err := m.hostBroker.UpdateMCP(cfg); err != nil {
			m.err = "saved on host but host MCP update failed: " + err.Error()
			return m, nil
		}
		if err := m.hostBroker.SetMCPTokens(map[string]string{env: key}); err != nil {
			m.err = "saved on host but host MCP token refresh failed: " + err.Error()
			return m, nil
		}
	} else if m.sandbox != nil {
		m.err = "saved on host but host MCP broker is unavailable"
		return m, nil
	}
	m.err = ""
	m.log = append(m.log, "mcp "+m.mcpPick.Name+" token saved ("+note+")  (OAuth: abox mcp login "+m.mcpPick.Name+")")
	m.saveTranscript()
	return m, nil
}

func (m model) saveProviderKey() (tea.Model, tea.Cmd) {
	key := strings.TrimSpace(m.keyIn.Value())
	if key == "" {
		m.err = "API key is empty"
		return m, nil
	}
	cfg, sel, note, err := applyProviderKey(m.cfg, m.provPick, key)
	m.keyIn.SetValue("")
	m.keyIn.Blur()
	m.ta.Focus()
	m.mode = modeChat
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.cfg = cfg
	if m.sandbox != nil {
		secrets := map[string]string{m.provPick.Env: key}
		if err := m.sandbox.SetModel(context.Background(), sel, secrets); err != nil {
			m.err = "saved on host but guest agent update failed: " + err.Error()
			return m, nil
		}
	}
	m.updateHostBroker(cfg, sel)
	m.sel = sel
	m.selKeyStatus = "key ok"
	if m.provKeyStatus == nil {
		m.provKeyStatus = map[string]string{}
	}
	m.provKeyStatus[m.provPick.Name] = "key ok"
	m.err = ""
	m.log = append(m.log, "connected "+m.provPick.Label+"  ("+note+")")
	m.saveTranscript()
	return m, nil
}

func (m model) updateHostBroker(cfg config.File, sel config.Model) {
	if m.hostBroker != nil {
		m.hostBroker.UpdateModel(cfg, sel)
	}
}

func (m model) approveRunCommand(ctx context.Context, params protocol.RunCommandApprovalParams) (protocol.ApprovalDecision, error) {
	req := &runCommandApprovalRequest{
		ctx:      ctx,
		params:   params,
		response: make(chan protocol.ApprovalDecision, 1),
		settled:  make(chan struct{}),
	}
	select {
	case m.approvals <- req:
	case <-ctx.Done():
		return protocol.ApprovalDeny, ctx.Err()
	}
	select {
	case decision := <-req.response:
		if ctx.Err() != nil {
			return protocol.ApprovalDeny, ctx.Err()
		}
		return decision, nil
	case <-ctx.Done():
		return protocol.ApprovalDeny, ctx.Err()
	}
}

func (m *model) resolveApproval(decision protocol.ApprovalDecision) {
	if m.approvalReq == nil {
		return
	}
	if m.approvalReq.ctx.Err() != nil {
		decision = protocol.ApprovalDeny
	}
	m.resolveRequest(m.approvalReq, decision)
	m.approvalReq = nil
	m.approvalAllow = false
	m.mode = modeChat
	m.ta.Focus()
}

func (m *model) resolveRequest(req *runCommandApprovalRequest, decision protocol.ApprovalDecision) {
	select {
	case req.response <- decision:
	default:
	}
	select {
	case <-req.settled:
	default:
		close(req.settled)
	}
}

func waitApproval(ch <-chan *runCommandApprovalRequest) tea.Cmd {
	return func() tea.Msg {
		return approvalMsg{req: <-ch}
	}
}

func waitApprovalCancellation(req *runCommandApprovalRequest) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-req.ctx.Done():
			return approvalCanceledMsg{req: req}
		case <-req.settled:
			return nil
		}
	}
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

// headerHeight is the rail: top rule, two field rows, and the body seam.
const headerHeight = 4

func (m model) theme() theme { return newTheme(colorEnabled()) }

func (m model) credState() string {
	if m.selKeyStatus == "" {
		return "checking"
	}
	return m.selKeyStatus
}

func (m model) View() tea.View {
	th := m.theme()
	width, height := m.width, m.height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}

	header := renderHeader(th, headerData{
		model: m.sel.Provider + "/" + m.sel.Model,
		vm:    m.vmState,
		net:   m.cfg.Connectivity.Mode,
		key:   m.credState(),
	}, width)

	lower := m.renderLower(th, width)
	bodyH := height - headerHeight - len(strings.Split(lower, "\n")) - 2
	if bodyH < 3 {
		bodyH = 3
	}

	content := th.canvas().Render(strings.Join([]string{
		header,
		m.renderBody(th, width, bodyH),
		lower,
		renderFooter(th, m.mode, width),
	}, "\n"))

	v := tea.NewView(content)
	v.AltScreen = true
	if th.colored {
		v.BackgroundColor = th.ground
	}
	return v
}

// renderBody boxes the transcript directly beneath the header seam.
func (m model) renderBody(th theme, width, height int) string {
	inner := width - 4
	entries := entriesFromLog(m.log)
	if len(entries) == 0 {
		entries = []entry{{kind: entryNotice, text: "Type / for commands."}}
	}
	lines := strings.Split(renderTranscript(th, entries, inner, height), "\n")
	if act := renderActivity(th, activityVisible(entries, m.busy), m.spin.View()); act != "" {
		lines = tail(append(lines, act), height)
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	rule := th.style(th.line)
	out := make([]string, 0, height+1)
	for _, l := range lines {
		out = append(out, rule.Render("│ ")+padTo(l, inner)+rule.Render(" │"))
	}
	return strings.Join(append(out, rule.Render("└"+strings.Repeat("─", width-2)+"┘")), "\n")
}

// renderLower is the mode-dependent zone under the transcript: a menu, a
// prompt, the approval dialog, or the composer.
func (m model) renderLower(th theme, width int) string {
	switch m.mode {
	case modeApproval:
		if m.approvalReq != nil {
			return renderApproval(th, m.approvalReq.params, m.approvalAllow, width)
		}
	case modeProviderPick:
		return renderPicker(th, "Select provider", providerRows(m.provKeyStatus), m.provSel, width)
	case modeCredModelPick:
		return renderPicker(th, "Model for "+m.credSource.Label, providerRows(nil), m.provSel, width)
	case modeCredSourcePick:
		rows := make([]pickerRow, 0, 3)
		for _, c := range cloudCredentialChoices() {
			rows = append(rows, pickerRow{label: c.Label, detail: c.Note})
		}
		return renderPicker(th, "Credential store", rows, m.credSourceSel, width)
	case modeMCPPick:
		servers := mcpServers(m.cfg)
		rows := make([]pickerRow, 0, len(servers))
		for _, s := range servers {
			rows = append(rows, pickerRow{label: s.Name, detail: s.URL, status: mcpTokenState(m.mcpKeyStatus, s.Name)})
		}
		return renderPicker(th, "MCP servers", rows, m.mcpSel, width)
	case modeProviderKey:
		return m.renderPrompt(th, "API key for "+m.provPick.Label, width)
	case modeMCPKey:
		return m.renderPrompt(th, "Bearer token for "+m.mcpPick.Name, width)
	case modeCredName:
		return m.renderPrompt(th, m.credSource.Label+" for "+m.provPick.Label, width)
	}
	return m.renderComposer(th, width)
}

func providerRows(status map[string]string) []pickerRow {
	choices := providerChoices()
	rows := make([]pickerRow, 0, len(choices))
	for _, p := range choices {
		rows = append(rows, pickerRow{label: p.Label, status: status[p.Name]})
	}
	return rows
}

func mcpTokenState(status map[string]string, name string) string {
	switch status[name] {
	case "key ok":
		return "token ok"
	case "":
		return ""
	default:
		return "no token"
	}
}

func (m model) renderPrompt(th theme, title string, width int) string {
	return renderPanel(th, title, th.lineFocus, []string{padTo(m.keyIn.View(), width-4)}, width)
}

// renderComposer draws the input box, with the slash menu stacked above it
// when the user is typing a command.
func (m model) renderComposer(th theme, width int) string {
	var out []string
	if m.showingSlash() {
		cmds := filterSlash(m.ta.Value())
		rows := make([]pickerRow, 0, len(cmds))
		for _, c := range cmds {
			rows = append(rows, pickerRow{label: c.Name, detail: c.Help})
		}
		out = append(out, renderPicker(th, "commands", rows, m.slashSel, width))
	}
	body := make([]string, 0, 3)
	for _, l := range strings.Split(m.ta.View(), "\n") {
		body = append(body, padTo(l, width-4))
	}
	out = append(out, renderPanel(th, "input", th.lineFocus, body, width))
	if m.err != "" {
		out = append(out, th.style(th.danger).Render(" "+truncTo(m.err, width-1)))
	}
	return strings.Join(out, "\n")
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

func Run(cfg config.File, sel config.Model, sb *runtime.Sandbox, broker *hostbroker.Broker, vmState string, log []string, resolver *credsource.Resolver, transcriptPath string) error {
	if resolver == nil {
		resolver = credsource.NewResolver()
	}
	m := New(cfg, sel, sb, broker, vmState, log, transcriptPath)
	m.resolver = resolver
	if sb != nil {
		if broker == nil {
			return fmt.Errorf("host broker is required when the VM is ready")
		}
		sb.SetGuestCallHandler(broker)
		if err := sb.SetRunCommandApprover(runtime.RunCommandApproverFunc(m.approveRunCommand)); err != nil {
			return fmt.Errorf("configure run_command approval: %w", err)
		}
	}
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
