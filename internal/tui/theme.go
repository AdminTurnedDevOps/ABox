package tui

import (
	"image/color"
	"os"

	"charm.land/lipgloss/v2"
)

// theme is the instrument-panel palette. Colors are named by role, not by
// appearance, so the same render code serves the colored and NO_COLOR paths.
type theme struct {
	colored bool

	ground    color.Color // page background
	surface   color.Color // panel fill, one step above ground
	line      color.Color // idle border
	lineFocus color.Color // focused border
	text      color.Color // body copy
	dim       color.Color // secondary copy, labels
	accent    color.Color // brand, user gutter, focus
	tool      color.Color // tool activity
	ok        color.Color // healthy state
	danger    color.Color // errors, denial
}

func newTheme(colored bool) theme {
	return theme{
		colored:   colored,
		ground:    lipgloss.Color("#0B0E11"),
		surface:   lipgloss.Color("#12161B"),
		line:      lipgloss.Color("#232A31"),
		lineFocus: lipgloss.Color("#3FD5C7"),
		text:      lipgloss.Color("#E6EDF3"),
		dim:       lipgloss.Color("#7D8B99"),
		accent:    lipgloss.Color("#3FD5C7"),
		tool:      lipgloss.Color("#E3B341"),
		ok:        lipgloss.Color("#56D364"),
		danger:    lipgloss.Color("#F85149"),
	}
}

// colorEnabled honors NO_COLOR unconditionally: any non-empty value disables
// color. https://no-color.org
func colorEnabled() bool {
	return os.Getenv("NO_COLOR") == ""
}

// style returns a foreground style, or a bare style when color is disabled so
// no escape sequences reach the terminal.
func (t theme) style(fg color.Color) lipgloss.Style {
	if !t.colored {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(fg)
}

// statusSymbol pairs every state with a glyph so status survives NO_COLOR;
// color reinforces the symbol rather than carrying the meaning alone.
func statusSymbol(state string) string {
	switch state {
	case "ready", "key ok", "ok", "token ok":
		return "●"
	case "failed", "unavailable", "no key", "no token":
		return "○"
	default:
		return "◐"
	}
}

// statusTone maps a state to the color that reinforces its symbol.
func (t theme) statusTone(state string) color.Color {
	switch statusSymbol(state) {
	case "●":
		return t.ok
	case "○":
		return t.danger
	default:
		return t.dim
	}
}

// canvas paints the page ground so the alt-screen does not borrow the host
// terminal's background.
func (t theme) canvas() lipgloss.Style {
	if !t.colored {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(t.text).Background(t.ground)
}

// title is the panel heading style: bold only when styling is enabled, so the
// NO_COLOR path emits no escape sequences at all.
func (t theme) title(fg color.Color) lipgloss.Style {
	if !t.colored {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(fg).Bold(true)
}
