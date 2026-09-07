package tui

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/AdminTurnedDevOps/ABox/protocol"
)

// entryKind tags a transcript line so the renderer can style it without
// storing ANSI in the log. Log lines stay plain: they are width-wrapped and
// persisted to transcript.json, and escape sequences would break both.
type entryKind int

const (
	entryAssistant entryKind = iota
	entryUser
	entryTool
	entryError
	entryNotice
	entryBlank
)

type entry struct {
	kind entryKind
	text string
}

const (
	userPrefix  = "you: "
	toolPrefix  = "  ▸ "
	errorPrefix = "error: "
)

// noticePrefixes are host-side status lines the model never produces.
var noticePrefixes = []string{"commands:", "connected ", "credential ", "mcp "}

func entriesFromLog(lines []string) []entry {
	out := make([]entry, 0, len(lines))
	for _, line := range lines {
		out = append(out, classifyLine(line))
	}
	return out
}

func classifyLine(line string) entry {
	switch {
	case line == "":
		return entry{kind: entryBlank}
	case strings.HasPrefix(line, userPrefix):
		return entry{kind: entryUser, text: strings.TrimPrefix(line, userPrefix)}
	case strings.HasPrefix(line, toolPrefix):
		return entry{kind: entryTool, text: strings.TrimPrefix(line, toolPrefix)}
	case strings.HasPrefix(line, errorPrefix):
		return entry{kind: entryError, text: strings.TrimPrefix(line, errorPrefix)}
	case isNotice(line):
		return entry{kind: entryNotice, text: line}
	default:
		return entry{kind: entryAssistant, text: line}
	}
}

func isNotice(line string) bool {
	for _, p := range noticePrefixes {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	return false
}

func logFromEntries(entries []entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.plain())
	}
	return out
}

func (e entry) plain() string {
	switch e.kind {
	case entryBlank:
		return ""
	case entryUser:
		return userPrefix + e.text
	case entryTool:
		return toolPrefix + e.text
	case entryError:
		return errorPrefix + e.text
	default:
		return e.text
	}
}

// headerData is the status rail's content, already reduced to display strings.
type headerData struct {
	model string
	vm    string
	net   string
	key   string
}

const (
	minPanelWidth = 24
	headerTitle   = "ABox"
)

// renderHeader draws the instrument rail: a titled top rule, two rows of
// aligned label/value pairs, and a seam that joins the transcript panel below.
func renderHeader(t theme, h headerData, width int) string {
	if width < minPanelWidth {
		width = minPanelWidth
	}
	inner := width - 4 // two border cells plus one pad cell each side

	title := t.style(t.accent).Render(headerTitle)
	titleRun := lipgloss.Width(headerTitle) + 3 // "┏━ " prefix
	fill := width - titleRun - 2                // trailing space and "┓"
	if fill < 0 {
		fill = 0
	}
	rule := t.style(t.line)
	top := rule.Render("┏━ ") + title + rule.Render(" "+strings.Repeat("━", fill)+"┓")
	seam := rule.Render("┡" + strings.Repeat("━", width-2) + "┩")

	half := inner / 2
	rows := []string{
		fieldPair(t, "MODEL", h.model, half, "VM", h.vm, inner-half),
		fieldPair(t, "NET", h.net, half, "KEY", h.key, inner-half),
	}
	out := []string{top}
	for _, r := range rows {
		out = append(out, rule.Render("┃ ")+r+rule.Render(" ┃"))
	}
	return strings.Join(append(out, seam), "\n")
}

// Label columns are fixed so values line up down the rail.
const (
	leftLabelW  = 6
	rightLabelW = 4
)

// fieldPair lays out two label/value cells on one rail row. Only the right
// column carries a status dot: MODEL and NET are settings, not health.
func fieldPair(t theme, leftLabel, leftVal string, leftW int, rightLabel, rightVal string, rightW int) string {
	return field(t, leftLabel, leftVal, leftLabelW, leftW, false) +
		field(t, rightLabel, rightVal, rightLabelW, rightW, true)
}

func field(t theme, label, value string, labelW, w int, dotted bool) string {
	cell := padTo(t.style(t.dim).Render(label), labelW)
	body := value
	if dotted {
		body = t.style(t.statusTone(value)).Render(statusSymbol(value)) + " " + value
	}
	return padTo(cell+t.style(t.text).Render(body), w)
}

// padTo pads or truncates to an exact cell width, measuring visible width so
// styled text lines up the same as plain text.
func padTo(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if got := lipgloss.Width(s); got < w {
		return s + strings.Repeat(" ", w-got)
	} else if got > w {
		return truncTo(s, w)
	}
	return s
}

func truncTo(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	runes := []rune(s)
	var b strings.Builder
	used, styled := 0, false
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\x1b' { // copy escape sequences whole; they cost no cells
			j := i
			for j < len(runes) && runes[j] != 'm' {
				j++
			}
			if j < len(runes) {
				j++
			}
			seq := string(runes[i:j])
			b.WriteString(seq)
			styled = seq != "\x1b[m"
			i = j - 1
			continue
		}
		rw := lipgloss.Width(string(runes[i]))
		if used+rw > w {
			break
		}
		used += rw
		b.WriteRune(runes[i])
	}
	if styled { // never leave the terminal in a style the cut discarded
		b.WriteString("\x1b[m")
	}
	return b.String()
}

const gutterBar = "▎"

// wrapCells wraps to a visible-cell budget, breaking on word boundaries and
// never splitting a rune. The previous byte-sliced wrap corrupted multibyte
// text at the width bound.
func wrapCells(s string, width int) []string {
	if width < 4 {
		width = 4
	}
	s = strings.ReplaceAll(s, "\t", "    ")
	var out []string
	for _, para := range strings.Split(s, "\n") {
		out = append(out, wrapParagraph(para, width)...)
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

func wrapParagraph(para string, width int) []string {
	if para == "" {
		return []string{""}
	}
	var lines []string
	runes := []rune(para)
	for len(runes) > 0 {
		if lipgloss.Width(string(runes)) <= width {
			lines = append(lines, string(runes))
			break
		}
		cut, used := 0, 0
		for i, r := range runes {
			rw := lipgloss.Width(string(r))
			if used+rw > width {
				break
			}
			used += rw
			cut = i + 1
		}
		if cut == 0 {
			cut = 1 // a single rune wider than the budget still has to advance
		}
		if sp := lastSpace(runes[:cut]); sp > width/4 {
			cut = sp
		}
		lines = append(lines, strings.TrimRight(string(runes[:cut]), " "))
		runes = []rune(strings.TrimLeft(string(runes[cut:]), " "))
	}
	return lines
}

func lastSpace(runes []rune) int {
	for i := len(runes) - 1; i >= 0; i-- {
		if runes[i] == ' ' {
			return i
		}
	}
	return -1
}

// gutter returns the speaker bar, its label, and the tone for the body text.
func (t theme) gutter(kind entryKind) (label string, tone color.Color, inline bool) {
	switch kind {
	case entryUser:
		return "you", t.accent, false
	case entryTool:
		return "tool", t.tool, true
	case entryError:
		return "error", t.danger, true
	case entryNotice:
		return "", t.dim, true
	default:
		return "abox", t.text, false
	}
}

// renderTranscript lays out the conversation: prose speakers get a labeled
// gutter with an indented body, short machine lines stay inline.
func renderTranscript(t theme, entries []entry, width, height int) string {
	var lines []string
	for _, e := range entries {
		lines = append(lines, renderEntry(t, e, width)...)
	}
	return strings.Join(tail(lines, height), "\n")
}

func renderEntry(t theme, e entry, width int) []string {
	if e.kind == entryBlank {
		return []string{""}
	}
	label, tone, inline := t.gutter(e.kind)
	if label == "" { // notices carry no speaker
		var out []string
		for _, l := range wrapCells(e.text, width) {
			out = append(out, t.style(tone).Render(l))
		}
		return out
	}
	head := t.style(tone).Render(gutterBar + label)
	if inline {
		wrapped := wrapCells(e.text, width-lipgloss.Width(gutterBar+label)-2)
		out := []string{head + t.style(t.text).Render("  "+wrapped[0])}
		for _, l := range wrapped[1:] {
			out = append(out, t.style(t.dim).Render("   "+l))
		}
		return out
	}
	out := []string{head}
	for _, l := range wrapCells(e.text, width-1) {
		out = append(out, t.style(t.text).Render(" "+l))
	}
	return out
}

// renderPanel draws a titled heavy-border box at an exact width. tone colors
// the border and title so a panel can signal severity (danger for approvals).
func renderPanel(t theme, title string, tone color.Color, body []string, width int) string {
	if width < minPanelWidth {
		width = minPanelWidth
	}
	inner := width - 4
	rule := t.style(tone)

	fill := width - lipgloss.Width(title) - 5 // "┏━ ", " ", "┓"
	if fill < 0 {
		fill = 0
	}
	out := []string{rule.Render("┏━ ") + t.title(tone).Render(title) + rule.Render(" "+strings.Repeat("━", fill)+"┓")}
	for _, line := range body {
		out = append(out, rule.Render("┃ ")+padTo(line, inner)+rule.Render(" ┃"))
	}
	return strings.Join(append(out, rule.Render("┗"+strings.Repeat("━", width-2)+"┛")), "\n")
}

// renderApproval is the run_command consent prompt. It shows exactly what will
// execute, where, and for how long, because this is the last gate before
// model-authored shell runs in the guest.
func renderApproval(t theme, p protocol.RunCommandApprovalParams, allow bool, width int) string {
	workdir := p.WorkDir
	if workdir == "" {
		workdir = protocol.GuestRepoDir
	}
	inner := width - 4

	label := func(k, v string) string {
		return t.style(t.dim).Render(padTo(k, 9)) + t.style(t.text).Render(v)
	}
	body := []string{
		t.style(t.tool).Render("run_command"),
		"",
	}
	// Quoted: a command carrying newlines or box-drawing characters could
	// otherwise paint convincing fake rows inside this dialog.
	for _, line := range wrapCells(strconv.Quote(p.Command), inner-2) {
		body = append(body, t.style(t.text).Render("  "+line))
	}
	body = append(body,
		"",
		label("workdir", strconv.Quote(workdir)),
		label("timeout", strconv.Itoa(p.TimeoutSec)+"s"),
		"",
		approvalChoices(t, allow),
	)
	return renderPanel(t, "APPROVAL REQUIRED", t.danger, body, width)
}

// approvalChoices brackets the focused choice so the selection survives
// NO_COLOR, and tints allow/deny with their semantic tones.
func approvalChoices(t theme, allow bool) string {
	deny, allowLabel := "  Deny  ", "  Allow once  "
	if allow {
		allowLabel = "[ Allow once ]"
	} else {
		deny = "[ Deny ]"
	}
	return t.style(t.ok).Render(allowLabel) + "   " + t.style(t.danger).Render(deny)
}

// pickerRow is one selectable line in a menu panel.
type pickerRow struct {
	label  string
	detail string
	status string
}

// renderPicker draws a menu panel. The focused row is marked with a caret and
// an accent tint; the caret carries the selection so it survives NO_COLOR.
func renderPicker(t theme, title string, rows []pickerRow, sel int, width int) string {
	inner := width - 4
	body := make([]string, 0, len(rows)+1)
	if len(rows) == 0 {
		body = append(body, t.style(t.dim).Render("none configured"))
		return renderPanel(t, title, t.line, body, width)
	}
	for i, r := range rows {
		caret, tone := "  ", t.text
		if i == sel {
			caret, tone = t.style(t.accent).Render("▸ "), t.accent
		}
		line := caret + t.style(tone).Render(r.label)
		if r.detail != "" {
			line += t.style(t.dim).Render("  " + r.detail)
		}
		if r.status != "" {
			dot := t.style(t.statusTone(r.status)).Render(statusSymbol(r.status))
			status := dot + t.style(t.dim).Render(" "+r.status)
			line = padTo(line, inner-lipgloss.Width(r.status)-2) + status
		}
		body = append(body, padTo(line, inner))
	}
	return renderPanel(t, title, t.line, body, width)
}

// footerHints lists the few keys that actually work in the current mode.
// Progressive disclosure: the floor is always visible, the rest lives in /help.
func footerHints(mode uiMode) []string {
	switch mode {
	case modeApproval:
		return []string{"←/→ select", "enter confirm", "esc deny"}
	case modeProviderPick, modeMCPPick, modeCredSourcePick, modeCredModelPick:
		return []string{"↑/↓ move", "enter select", "esc cancel"}
	case modeProviderKey, modeMCPKey, modeCredName:
		return []string{"enter save", "esc cancel"}
	default:
		return []string{"enter send", "/ commands", "^c quit"}
	}
}

func renderFooter(t theme, mode uiMode, width int) string {
	hints := footerHints(mode)
	parts := make([]string, 0, len(hints))
	for _, h := range hints {
		parts = append(parts, t.style(t.dim).Render(h))
	}
	return truncTo(" "+strings.Join(parts, t.style(t.line).Render("  ·  ")), width)
}

// renderActivity is the streaming indicator. It occupies the assistant gutter
// so text lands where the spinner was, with no layout jump.
func renderActivity(t theme, busy bool, frame string) string {
	if !busy {
		return ""
	}
	return t.style(t.text).Render(gutterBar+"abox") +
		t.style(t.accent).Render("  "+frame) +
		t.style(t.dim).Render(" thinking…")
}

// activityVisible reports whether the spinner line should be drawn. Once the
// assistant's text starts arriving it occupies the same gutter, so showing
// both would render the speaker twice for the whole response.
func activityVisible(entries []entry, busy bool) bool {
	if !busy {
		return false
	}
	if n := len(entries); n > 0 {
		last := entries[n-1]
		return !(last.kind == entryAssistant && last.text != "")
	}
	return true
}
