package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
)

func TestWriteGuestConfigExcludesMCPAndSecrets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := Create("/source")
	if err != nil {
		t.Fatal(err)
	}
	err = s.WriteGuestConfig(config.Model{Name: "grok"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.GuestConfigJSON())
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if strings.Contains(body, "mcp_servers") || strings.Contains(body, "api.githubcopilot.com") || strings.Contains(body, "ABOX_MCP_GH_TOKEN") {
		t.Fatalf("guest config leaked MCP policy: %s", body)
	}
	if strings.Contains(body, `"secrets"`) {
		t.Fatalf("guest config leaked a secrets key: %s", body)
	}
	st, err := os.Stat(s.GuestConfigJSON())
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm %o", st.Mode().Perm())
	}
}

func TestWritePaddedConfigLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.raw")
	data := []byte(`{"session_id":"x"}`)
	if err := WritePaddedConfig(path, data); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != ConfigDiskSize {
		t.Fatalf("size %d", len(raw))
	}
	if string(raw[:len(data)]) != string(data) {
		t.Fatalf("payload %q", raw[:len(data)])
	}
	if raw[len(data)] != 0 {
		t.Fatal("missing zero padding")
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o400 {
		t.Fatalf("perm %o", st.Mode().Perm())
	}
	// Rewriting a read-only file must still work (resume path).
	if err := WritePaddedConfig(path, []byte(`{"session_id":"y"}`)); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRequiresRootRaw(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := Create("/source/a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(s.ID); err == nil {
		t.Fatal("expected missing root.raw error")
	}
	if err := os.WriteFile(s.RootDisk(), []byte("disk"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != s.ID || got.SourceDir != "/source/a" {
		t.Fatalf("%#v", got)
	}
}

func TestTranscriptRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := Create("/source")
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{"you: hi", "", "hello"}
	if err := WriteTranscript(s.TranscriptPath(), lines); err != nil {
		t.Fatal(err)
	}
	got, err := ReadTranscript(s.TranscriptPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "you: hi" || got[2] != "hello" {
		t.Fatalf("%#v", got)
	}
}

func TestReadTranscriptMissing(t *testing.T) {
	got, err := ReadTranscript(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || got != nil {
		t.Fatalf("%v %#v", err, got)
	}
}

func TestLoadRejectsInvalidSessionID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, id := range []string{"", "../outside", "not-hex", strings.Repeat("a", 31)} {
		if _, err := Load(id); err == nil {
			t.Fatalf("expected %q to be rejected", id)
		}
	}
}
