package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
)

func TestWriteGuestConfigIncludesMCP(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := Create("/repo", "deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	err = s.WriteGuestConfig(config.Model{Name: "grok"}, map[string]string{"ABOX_MCP_GH_TOKEN": "tok"}, []config.MCPServer{
		{Name: "gh", URL: "https://api.githubcopilot.com/mcp/", CredentialEnv: "ABOX_MCP_GH_TOKEN"},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.GuestConfigJSON())
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "api.githubcopilot.com") || !strings.Contains(body, "ABOX_MCP_GH_TOKEN") {
		t.Fatalf("guest config missing mcp: %s", body)
	}
}

func TestLoadRequiresRootRaw(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := Create("/repo/a", "h1")
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
	if got.ID != s.ID || got.RepoRoot != "/repo/a" {
		t.Fatalf("%#v", got)
	}
}

func TestLatestForRepoPicksNewestMatching(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	old, err := Create("/repo/app", "h1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old.RootDisk(), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	other, err := Create("/repo/other", "h2")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other.RootDisk(), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	newer, err := Create("/repo/app", "h3")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer.RootDisk(), []byte("c"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LatestForRepo("/repo/app")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != newer.ID {
		t.Fatalf("got %s want %s", got.ID, newer.ID)
	}
}

func TestLatestForRepoSkipsMissingDisk(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := Create("/repo/app", "h1")
	if err != nil {
		t.Fatal(err)
	}
	_ = s
	if _, err := LatestForRepo("/repo/app"); err == nil {
		t.Fatal("expected error when root.raw missing")
	}
}

func TestLatestForRepoNone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(config.SessionRoot(), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := LatestForRepo(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected no session error")
	}
}
