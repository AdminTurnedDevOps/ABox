package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateRejectsUnknownMode(t *testing.T) {
	c := Defaults()
	c.Connectivity.Mode = "wide-open"
	if err := c.Validate(); err == nil {
		t.Fatal("expected error")
	}
}

func TestDefaultsValid(t *testing.T) {
	if err := Defaults().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDirUsesDotAbox(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ABOX_HOME", "")
	got := Dir()
	want := filepath.Join(home, ".abox")
	if got != want {
		t.Fatalf("Dir()=%q want %q", got, want)
	}
	if Path() != filepath.Join(want, "config.yaml") {
		t.Fatalf("Path()=%q", Path())
	}
}

func TestDirIgnoresLegacyApplicationSupport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ABOX_HOME", "")
	legacy := filepath.Join(home, "Library", "Application Support", "ABox")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "config.yaml"), []byte("connectivity:\n  mode: direct\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Dir()
	want := filepath.Join(home, ".abox")
	if got != want {
		t.Fatalf("Dir()=%q want %q", got, want)
	}
}

func TestEnsureLayoutSeedsLegacyConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ABOX_HOME", "")
	legacy := filepath.Join(home, "Library", "Application Support", "ABox")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte("connectivity:\n  mode: offline\n")
	if err := os.WriteFile(filepath.Join(legacy, "config.yaml"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(home, ".abox")) {
		t.Fatal("expected ~/.abox")
	}
	got, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("got %q", got)
	}
}

func TestDirRespectsABOX_HOME(t *testing.T) {
	override := t.TempDir()
	t.Setenv("ABOX_HOME", override)
	if Dir() != override {
		t.Fatalf("Dir()=%q", Dir())
	}
}

func TestEnsureLayoutCreatesHomeAndConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ABOX_HOME", "")
	if err := EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(Dir())
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsDir() {
		t.Fatal("expected ~/.abox directory")
	}
	if st.Mode().Perm() != 0o700 {
		t.Fatalf("dir perm %o", st.Mode().Perm())
	}
	cfg, path, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(home, ".abox", "config.yaml") {
		t.Fatalf("path %q", path)
	}
	if cfg.Connectivity.Mode != "direct" {
		t.Fatalf("mode %q", cfg.Connectivity.Mode)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config perm %o", info.Mode().Perm())
	}
}

func TestAddMCPServerDirect(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ABOX_HOME", "")
	cfg := Defaults()
	if err := cfg.AddMCPServer("direct", MCPServer{
		Name:          "github",
		URL:           "https://api.githubcopilot.com/mcp/",
		CredentialEnv: "GITHUB_MCP_TOKEN",
	}); err != nil {
		t.Fatal(err)
	}
	if cfg.Connectivity.Mode != "direct" || len(cfg.MCPServers) != 1 {
		t.Fatalf("%#v", cfg)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	got, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.MCPServers) != 1 || got.MCPServers[0].Name != "github" {
		t.Fatalf("%#v", got.MCPServers)
	}
}

func TestAddMCPServerAgentgateway(t *testing.T) {
	cfg := Defaults()
	if err := cfg.AddMCPServer("agentgateway", MCPServer{
		Name: "agw",
		URL:  "https://agw.example/mcp",
	}); err != nil {
		t.Fatal(err)
	}
	if cfg.Connectivity.Mode != "agentgateway" {
		t.Fatalf("mode %q", cfg.Connectivity.Mode)
	}
	if cfg.Connectivity.Enforcement != "required" {
		t.Fatalf("enforcement %q", cfg.Connectivity.Enforcement)
	}
	if len(cfg.MCPServers) != 1 || cfg.MCPServers[0].CredentialEnv != "" {
		t.Fatalf("%#v", cfg.MCPServers)
	}
}

func TestAddMCPServerAgentgatewayRejectsSecondOrigin(t *testing.T) {
	cfg := Defaults()
	if err := cfg.AddMCPServer("agentgateway", MCPServer{Name: "agw", URL: "https://agw.example/mcp"}); err != nil {
		t.Fatal(err)
	}
	err := cfg.AddMCPServer("agentgateway", MCPServer{Name: "github", URL: "https://api.githubcopilot.com/mcp/"})
	if err == nil {
		t.Fatal("expected second origin rejected")
	}
}

func TestAddMCPServerRequiresKnownMode(t *testing.T) {
	cfg := Defaults()
	if err := cfg.AddMCPServer("stdio", MCPServer{Name: "x", URL: "https://example.com/mcp"}); err == nil {
		t.Fatal("expected unknown mode rejected")
	}
}

func TestEnsureLayoutDoesNotOverwriteConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ABOX_HOME", "")
	if err := EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	custom := []byte("connectivity:\n  mode: offline\n")
	if err := os.WriteFile(Path(), custom, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(custom) {
		t.Fatalf("overwrote config: %q", got)
	}
}

func TestMCPServerRejectsHTTP(t *testing.T) {
	c := Defaults()
	c.MCPServers = []MCPServer{{Name: "github", URL: "http://api.githubcopilot.com/mcp/"}}
	if err := c.Validate(); err == nil {
		t.Fatal("expected http url rejected")
	}
}

func TestMCPServerRejectsIPLiteral(t *testing.T) {
	c := Defaults()
	c.MCPServers = []MCPServer{{Name: "local", URL: "https://127.0.0.1/mcp"}}
	if err := c.Validate(); err == nil {
		t.Fatal("expected ip literal rejected")
	}
}

func TestMCPServerRejectsDuplicateNames(t *testing.T) {
	c := Defaults()
	c.MCPServers = []MCPServer{
		{Name: "github", URL: "https://api.githubcopilot.com/mcp/"},
		{Name: "github", URL: "https://api.githubcopilot.com/other"},
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected duplicate name rejected")
	}
}

func TestMCPServerRejectsBadName(t *testing.T) {
	c := Defaults()
	c.MCPServers = []MCPServer{{Name: "GitHub", URL: "https://api.githubcopilot.com/mcp/"}}
	if err := c.Validate(); err == nil {
		t.Fatal("expected uppercase name rejected")
	}
}

func TestAgentgatewayRequiredRejectsMultipleServers(t *testing.T) {
	c := Defaults()
	c.Connectivity.Mode = "agentgateway"
	c.Connectivity.Enforcement = "required"
	c.MCPServers = []MCPServer{
		{Name: "agw", URL: "https://agw.example/mcp"},
		{Name: "github", URL: "https://api.githubcopilot.com/mcp/"},
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected extra mcp_servers rejected")
	}
}

func TestAgentgatewayRequiresURL(t *testing.T) {
	c := Defaults()
	c.Connectivity.Mode = "agentgateway"
	if err := c.Validate(); err == nil {
		t.Fatal("expected missing mcp_servers rejected")
	}
}

func TestResolvedMCPServersDirect(t *testing.T) {
	c := Defaults()
	c.MCPServers = []MCPServer{{Name: "github", URL: "https://api.githubcopilot.com/mcp/", CredentialEnv: "GITHUB_MCP_TOKEN"}}
	got, err := c.ResolvedMCPServers()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "github" {
		t.Fatalf("got %#v", got)
	}
}

func TestResolvedMCPServersOfflineEmpty(t *testing.T) {
	c := Defaults()
	c.Connectivity.Mode = "offline"
	c.MCPServers = []MCPServer{{Name: "github", URL: "https://api.githubcopilot.com/mcp/"}}
	got, err := c.ResolvedMCPServers()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestResolvedMCPServersAgentgatewayUsesURL(t *testing.T) {
	c := Defaults()
	c.Connectivity.Mode = "agentgateway"
	c.Connectivity.Enforcement = "required"
	c.MCPServers = []MCPServer{{Name: "agw", URL: "https://agw.example/mcp"}}
	got, err := c.ResolvedMCPServers()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "agw" || got[0].URL != "https://agw.example/mcp" || got[0].CredentialEnv != "" {
		t.Fatalf("got %#v", got)
	}
}
