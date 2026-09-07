package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"github.com/AdminTurnedDevOps/ABox/protocol"
)

func TestEntriesFromLogClassifiesEachSpeaker(t *testing.T) {
	got := entriesFromLog([]string{
		"you: hi",
		"",
		"Hello there",
		"  ▸ run_command  exit=0",
		"error: boom",
		"commands:",
	})
	want := []entry{
		{kind: entryUser, text: "hi"},
		{kind: entryBlank},
		{kind: entryAssistant, text: "Hello there"},
		{kind: entryTool, text: "run_command  exit=0"},
		{kind: entryError, text: "boom"},
		{kind: entryNotice, text: "commands:"},
	}
	if len(got) != len(want) {
		t.Fatalf("entriesFromLog returned %d entries, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].kind != want[i].kind || got[i].text != want[i].text {
			t.Errorf("entry %d = {%v %q}, want {%v %q}", i, got[i].kind, got[i].text, want[i].kind, want[i].text)
		}
	}
}

func TestLogFromEntriesRoundTripsTranscriptFormat(t *testing.T) {
	// transcript.json must keep the exact on-disk shape it had before styling.
	original := []string{"you: hi", "", "Hello there", "  ▸ run_command  exit=0", "error: boom"}
	if got := logFromEntries(entriesFromLog(original)); !equalStrings(got, original) {
		t.Errorf("round trip = %#v, want %#v", got, original)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRenderHeaderShowsEveryStatusFieldWithinWidth(t *testing.T) {
	th := newTheme(false)
	h := headerData{model: "xai/grok-4", vm: "ready", net: "direct", key: "key ok"}
	got := renderHeader(th, h, 60)

	for _, want := range []string{"ABox", "MODEL", "xai/grok-4", "VM", "ready", "NET", "direct", "KEY", "●"} {
		if !strings.Contains(got, want) {
			t.Errorf("header missing %q:\n%s", want, got)
		}
	}
	for i, line := range strings.Split(got, "\n") {
		if w := lipgloss.Width(line); w > 60 {
			t.Errorf("header line %d is %d cells wide, want <= 60: %q", i, w, line)
		}
	}
}

func TestRenderHeaderMarksFailedVMWithHollowDot(t *testing.T) {
	th := newTheme(false)
	got := renderHeader(th, headerData{model: "m", vm: "failed", net: "direct", key: "no key"}, 60)
	if !strings.Contains(got, "○") {
		t.Errorf("failed vm should render a hollow dot:\n%s", got)
	}
}

func TestRenderHeaderDotsOnlyHealthFields(t *testing.T) {
	// NET and MODEL are settings, not health: a status dot there is noise.
	got := renderHeader(newTheme(false), headerData{model: "xai/grok-4", vm: "ready", net: "direct", key: "key ok"}, 64)
	if strings.Contains(got, "◐") {
		t.Errorf("only VM and KEY carry status dots; got a pending dot on a setting:\n%s", got)
	}
	if strings.Count(got, "●") != 2 {
		t.Errorf("want exactly two health dots (VM, KEY), got %d:\n%s", strings.Count(got, "●"), got)
	}
}

func TestRenderHeaderAlignsValueColumns(t *testing.T) {
	got := renderHeader(newTheme(false), headerData{model: "xai/grok-4", vm: "ready", net: "direct", key: "key ok"}, 64)
	rows := strings.Split(got, "\n")[1:3]
	if got, want := strings.Index(rows[0], "xai/grok-4"), strings.Index(rows[1], "direct"); got != want {
		t.Errorf("left values start at columns %d and %d, want them aligned:\n%s", got, want, strings.Join(rows, "\n"))
	}
	if got, want := strings.Index(rows[0], "ready"), strings.Index(rows[1], "key ok"); got != want {
		t.Errorf("right values start at columns %d and %d, want them aligned:\n%s", got, want, strings.Join(rows, "\n"))
	}
}

func TestWrapCellsKeepsMultibyteRunesIntact(t *testing.T) {
	// The old byte-sliced wrap cut multibyte runes in half at the width bound.
	got := wrapCells(strings.Repeat("日本語", 12), 20)
	for i, line := range got {
		if !utf8.ValidString(line) {
			t.Errorf("line %d is not valid UTF-8: %q", i, line)
		}
		if w := lipgloss.Width(line); w > 20 {
			t.Errorf("line %d is %d cells wide, want <= 20: %q", i, w, line)
		}
	}
}

func TestWrapCellsBreaksOnWordBoundary(t *testing.T) {
	got := wrapCells("the quick brown fox jumps", 12)
	if len(got) < 2 {
		t.Fatalf("expected a wrap, got %#v", got)
	}
	if strings.HasSuffix(got[0], " ") || strings.HasPrefix(got[1], " ") {
		t.Errorf("wrap should trim the break space: %#v", got)
	}
}

func TestRenderTranscriptGivesEachSpeakerAGutter(t *testing.T) {
	th := newTheme(false)
	got := renderTranscript(th, []entry{
		{kind: entryUser, text: "hi"},
		{kind: entryAssistant, text: "hello back"},
		{kind: entryTool, text: "run_command  exit=0"},
		{kind: entryError, text: "boom"},
	}, 60, 40)

	for _, want := range []string{"▎you", "hi", "▎abox", "hello back", "▎tool", "run_command", "▎error", "boom"} {
		if !strings.Contains(got, want) {
			t.Errorf("transcript missing %q:\n%s", want, got)
		}
	}
}

func TestRenderTranscriptRespectsHeightAndWidth(t *testing.T) {
	var entries []entry
	for i := 0; i < 50; i++ {
		entries = append(entries, entry{kind: entryAssistant, text: strings.Repeat("word ", 30)})
	}
	got := renderTranscript(newTheme(false), entries, 40, 10)
	lines := strings.Split(got, "\n")
	if len(lines) > 10 {
		t.Errorf("transcript rendered %d lines, want <= 10", len(lines))
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > 40 {
			t.Errorf("line %d is %d cells wide, want <= 40: %q", i, w, line)
		}
	}
}

func TestRenderApprovalShowsCommandContext(t *testing.T) {
	got := renderApproval(newTheme(false), protocol.RunCommandApprovalParams{
		Command: "curl -s https://api.example.com", WorkDir: "/work/repo", TimeoutSec: 60,
	}, false, 60)

	for _, want := range []string{"APPROVAL REQUIRED", "run_command", "curl -s https://api.example.com", "/work/repo", "60s"} {
		if !strings.Contains(got, want) {
			t.Errorf("approval dialog missing %q:\n%s", want, got)
		}
	}
	for i, line := range strings.Split(got, "\n") {
		if w := lipgloss.Width(line); w > 60 {
			t.Errorf("line %d is %d cells wide, want <= 60: %q", i, w, line)
		}
	}
}

func TestRenderApprovalMarksTheSelectedChoice(t *testing.T) {
	p := protocol.RunCommandApprovalParams{Command: "ls", WorkDir: ".", TimeoutSec: 5}
	denied := renderApproval(newTheme(false), p, false, 60)
	allowed := renderApproval(newTheme(false), p, true, 60)

	if !strings.Contains(denied, "[ Deny ]") {
		t.Errorf("deny selection should be bracketed:\n%s", denied)
	}
	if !strings.Contains(allowed, "[ Allow once ]") {
		t.Errorf("allow selection should be bracketed:\n%s", allowed)
	}
	if denied == allowed {
		t.Error("selection state must change the rendering")
	}
}

func TestRenderApprovalDefaultsWorkdirToRepoRoot(t *testing.T) {
	got := renderApproval(newTheme(false), protocol.RunCommandApprovalParams{Command: "ls", TimeoutSec: 5}, false, 60)
	if !strings.Contains(got, protocol.GuestRepoDir) {
		t.Errorf("empty workdir should display the guest repo root %q:\n%s", protocol.GuestRepoDir, got)
	}
}

func TestRenderPickerMarksSelectedRow(t *testing.T) {
	rows := []pickerRow{
		{label: "Grok (xAI)", status: "key ok"},
		{label: "OpenAI", status: "no key"},
		{label: "Anthropic", status: "no key"},
	}
	got := renderPicker(newTheme(false), "Select provider", rows, 1, 60)

	for _, want := range []string{"Select provider", "Grok (xAI)", "OpenAI", "Anthropic"} {
		if !strings.Contains(got, want) {
			t.Errorf("picker missing %q:\n%s", want, got)
		}
	}
	lines := strings.Split(got, "\n")
	var selected, unselected string
	for _, l := range lines {
		if strings.Contains(l, "OpenAI") {
			selected = l
		}
		if strings.Contains(l, "Anthropic") {
			unselected = l
		}
	}
	if !strings.Contains(selected, "▸") {
		t.Errorf("selected row should carry a caret: %q", selected)
	}
	if strings.Contains(unselected, "▸") {
		t.Errorf("unselected row should not carry a caret: %q", unselected)
	}
}

func TestRenderPickerHandlesEmptyList(t *testing.T) {
	got := renderPicker(newTheme(false), "MCP servers", nil, 0, 60)
	if !strings.Contains(got, "none configured") {
		t.Errorf("empty picker should say so:\n%s", got)
	}
}

func TestRenderFooterIsContextual(t *testing.T) {
	th := newTheme(false)
	chat := renderFooter(th, modeChat, 60)
	approval := renderFooter(th, modeApproval, 60)

	if !strings.Contains(chat, "commands") {
		t.Errorf("chat footer should advertise the command menu:\n%s", chat)
	}
	if !strings.Contains(approval, "deny") {
		t.Errorf("approval footer should mention denial:\n%s", approval)
	}
	if chat == approval {
		t.Error("footer hints must change with mode")
	}
	if w := lipgloss.Width(approval); w > 60 {
		t.Errorf("footer is %d cells wide, want <= 60", w)
	}
}

func TestRenderActivityOnlyShowsWhileBusy(t *testing.T) {
	th := newTheme(false)
	if got := renderActivity(th, false, "⠼"); got != "" {
		t.Errorf("idle activity line = %q, want empty", got)
	}
	got := renderActivity(th, true, "⠼")
	if !strings.Contains(got, "⠼") || !strings.Contains(got, "thinking") {
		t.Errorf("busy activity line = %q, want the spinner frame and a label", got)
	}
	if !strings.Contains(got, gutterBar) {
		t.Errorf("activity line should align with the speaker gutter: %q", got)
	}
}

func TestRenderApprovalEscapesControlCharactersSoTheDialogCannotBeForged(t *testing.T) {
	// A command carrying newlines and box-drawing could otherwise paint fake
	// dialog rows and trick the operator into approving something else.
	forged := "ls\n┃  [ Allow once ]  ┃\ntimeout  1s"
	got := renderApproval(newTheme(false), protocol.RunCommandApprovalParams{
		Command: forged, WorkDir: ".", TimeoutSec: 5,
	}, false, 60)

	if strings.Contains(got, "ls\n┃") {
		t.Errorf("raw newline from the command reached the dialog:\n%s", got)
	}
	if !strings.Contains(got, `\n`) {
		t.Errorf("newlines should render escaped:\n%s", got)
	}
	if n := strings.Count(got, "[ Deny ]"); n != 1 {
		t.Errorf("dialog shows %d deny buttons, want exactly 1:\n%s", n, got)
	}
}

func TestTruncToNeverCutsInsideAnEscapeSequence(t *testing.T) {
	th := newTheme(true)
	styled := th.style(th.accent).Render("abcdefghij") + th.style(th.danger).Render("klmnop")
	got := truncTo(styled, 8)
	if strings.Count(got, "\x1b")%2 != 0 {
		t.Errorf("truncation left a dangling escape sequence: %q", got)
	}
	if w := lipgloss.Width(got); w > 8 {
		t.Errorf("truncated width = %d, want <= 8: %q", w, got)
	}
}

func TestActivityHidesOnceTextStartsArriving(t *testing.T) {
	// Streaming text already occupies the assistant gutter; a second
	// "thinking" line beneath it renders a duplicate speaker.
	streaming := []entry{{kind: entryUser, text: "hi"}, {kind: entryBlank}, {kind: entryAssistant, text: "partial repl"}}
	if activityVisible(streaming, true) {
		t.Error("activity line should be hidden while assistant text is streaming")
	}

	waiting := []entry{{kind: entryUser, text: "hi"}, {kind: entryBlank}}
	if !activityVisible(waiting, true) {
		t.Error("activity line should show while waiting for the first token")
	}
	if activityVisible(waiting, false) {
		t.Error("activity line should never show when idle")
	}

	afterTool := []entry{{kind: entryTool, text: "run_command  exit=0"}}
	if !activityVisible(afterTool, true) {
		t.Error("activity line should show while the model works after a tool call")
	}
}
