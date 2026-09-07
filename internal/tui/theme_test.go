package tui

import (
	"strings"
	"testing"
)

func TestThemeWithoutColorEmitsNoEscapeSequences(t *testing.T) {
	th := newTheme(false)
	for name, got := range map[string]string{
		"accent": th.style(th.accent).Render("hi"),
		"danger": th.style(th.danger).Render("hi"),
		"dim":    th.style(th.dim).Render("hi"),
	} {
		if strings.Contains(got, "\x1b") {
			t.Errorf("%s style emitted an escape sequence with color disabled: %q", name, got)
		}
		if got != "hi" {
			t.Errorf("%s style = %q, want %q", name, got, "hi")
		}
	}
}

func TestThemeWithColorEmitsEscapeSequences(t *testing.T) {
	// Guards the inverse: a theme that never colors would pass the test above.
	if got := newTheme(true).style(newTheme(true).accent).Render("hi"); !strings.Contains(got, "\x1b") {
		t.Errorf("accent style with color enabled = %q, want an escape sequence", got)
	}
}

func TestStatusSymbolReflectsState(t *testing.T) {
	cases := map[string]string{
		"ready":       "●",
		"key ok":      "●",
		"failed":      "○",
		"unavailable": "○",
		"no key":      "○",
		"not-started": "◐",
		"":            "◐",
	}
	for state, want := range cases {
		if got := statusSymbol(state); got != want {
			t.Errorf("statusSymbol(%q) = %q, want %q", state, got, want)
		}
	}
}
