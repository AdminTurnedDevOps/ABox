package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModelGuestRoundTrip(t *testing.T) {
	orig := Model{Name: "grok-default", Provider: "xai", Model: "grok-4", CredentialEnv: "XAI_API_KEY", BaseURL: "https://api.x.ai/v1"}
	got := ModelFromGuest(orig.ToGuest())
	if got != orig {
		t.Fatalf("got %+v want %+v", got, orig)
	}
}

func TestValidEnvName(t *testing.T) {
	if !ValidEnvName("XAI_API_KEY") || !ValidEnvName("A1") || ValidEnvName("1A") || ValidEnvName("xai") || ValidEnvName("") {
		t.Fatal("ValidEnvName mismatch")
	}
}

func TestDefaultProvidersDriveDefaults(t *testing.T) {
	cfg := Defaults()
	ps := DefaultProviders()
	if len(cfg.Models) != len(ps) {
		t.Fatalf("models %d providers %d", len(cfg.Models), len(ps))
	}
	for i, p := range ps {
		if cfg.Models[i] != p.ModelConfig() {
			t.Fatalf("model[%d]=%+v want %+v", i, cfg.Models[i], p.ModelConfig())
		}
	}
	if cfg.Resources.VCPU != 1 || cfg.Resources.RAMMiB != 768 {
		t.Fatalf("resources %+v", cfg.Resources)
	}
	vcpu, ram := Resources{}.Resolved()
	if vcpu != 1 || ram != 768 {
		t.Fatalf("resolved %d %d", vcpu, ram)
	}
}

func TestModelEnvName(t *testing.T) {
	if got := (Model{Name: "grok-default", Provider: "xai"}).EnvName(); got != "XAI_API_KEY" {
		t.Fatalf("canonical provider env: %q", got)
	}
	if got := (Model{Name: "custom", CredentialEnv: "MY_KEY"}).EnvName(); got != "MY_KEY" {
		t.Fatalf("explicit env: %q", got)
	}
	if got := (Model{Name: "my-model", Provider: "other"}).EnvName(); got != "ABOX_MODEL_MY_MODEL_KEY" {
		t.Fatalf("derived env: %q", got)
	}
}

func TestModelCredentialReference(t *testing.T) {
	if got := (Model{CredentialEnv: "X"}).CredentialReference(); got != (CredentialRef{Source: "env", Name: "X"}) {
		t.Fatalf("alias: %+v", got)
	}
	if got := (Model{Name: "g", Provider: "xai"}).CredentialReference(); got != (CredentialRef{Source: "env", Name: "XAI_API_KEY"}) {
		t.Fatalf("canonical: %+v", got)
	}
	explicit := CredentialRef{Source: "vault", Name: "secret/abox/x", Field: "api_key"}
	if got := (Model{Credential: &explicit}).CredentialReference(); got != explicit {
		t.Fatalf("explicit: %+v", got)
	}
}

func TestMCPServerCredentialReference(t *testing.T) {
	if got := (MCPServer{Name: "gh"}).CredentialReference(); got != (CredentialRef{Source: "env", Name: "ABOX_MCP_GH_TOKEN"}) {
		t.Fatalf("derived: %+v", got)
	}
	if got := (MCPServer{Name: "gh", CredentialEnv: "GH"}).CredentialReference(); got != (CredentialRef{Source: "env", Name: "GH"}) {
		t.Fatalf("alias: %+v", got)
	}
}

func TestValidateRejectsCredentialAndCredentialEnvBoth(t *testing.T) {
	c := Defaults()
	c.Models[0].Credential = &CredentialRef{Source: "env", Name: "X"}
	c.Models[0].CredentialEnv = "X"
	if err := c.Validate(); err == nil {
		t.Fatal("expected both-set rejection")
	}
	c2 := Defaults()
	c2.MCPServers = []MCPServer{{
		Name: "gh", URL: "https://api.githubcopilot.com/mcp/",
		CredentialEnv: "GH", Credential: &CredentialRef{Source: "env", Name: "GH"},
	}}
	if err := c2.Validate(); err == nil {
		t.Fatal("expected mcp both-set rejection")
	}
}

func TestValidateRejectsUnknownCredentialSource(t *testing.T) {
	c := Defaults()
	c.Models[0].CredentialEnv = ""
	c.Models[0].Credential = &CredentialRef{Source: "kube", Name: "x"}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "unknown source") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateFieldVersionRules(t *testing.T) {
	c := Defaults()
	c.Models[0].CredentialEnv = ""
	c.Models[0].Credential = &CredentialRef{Source: "keychain", Name: "K", Field: "f"}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "field is not supported") {
		t.Fatalf("got %v", err)
	}
	c = Defaults()
	c.Models[0].CredentialEnv = ""
	c.Models[0].Credential = &CredentialRef{Source: "aws", Name: "prod/x", Version: "1"}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "version is not supported") {
		t.Fatalf("got %v", err)
	}
	c = Defaults()
	c.Models[0].CredentialEnv = ""
	c.Models[0].Credential = &CredentialRef{Source: "vault", Name: "secret/a/b", Field: "api_key", Version: "3"}
	if err := c.Validate(); err != nil {
		t.Fatalf("vault with field+version should pass: %v", err)
	}
	c = Defaults()
	c.Models[0].CredentialEnv = ""
	c.Models[0].Credential = &CredentialRef{Source: "azure", Name: "https://testkv.vault.azure.net/secrets/x", Version: "v1"}
	if err := c.Validate(); err != nil {
		t.Fatalf("azure with version should pass: %v", err)
	}
}

func TestAzureCredentialRejectsUntrustedAndAmbiguousURLs(t *testing.T) {
	for _, raw := range []string{
		"https://testkv.vault.azure.net.attacker.example/secrets/x",
		"https://testkv.vault.azure.net@attacker.example/secrets/x",
		"https://testkv.vault.azure.net:443/secrets/x",
		"https://testkv.vault.azure.net/secrets/x?redirect=https://attacker.example",
		"https://testkv.vault.azure.net/secrets/x/one/too-many",
		"https://testkv.vault.azure.net/secrets/x%2Fversion",
	} {
		c := Defaults()
		c.Models[0].CredentialEnv = ""
		c.Models[0].Credential = &CredentialRef{Source: "azure", Name: raw}
		if err := c.Validate(); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestAzureCredentialSupportsSovereignClouds(t *testing.T) {
	for _, raw := range []string{
		"https://testkv.vault.azure.net/secrets/x",
		"https://testkv.vault.usgovcloudapi.net/secrets/x",
		"https://testkv.vault.azure.cn/secrets/x",
		"https://testkv.vault.microsoftazure.de/secrets/x",
	} {
		if _, _, _, cloud, err := ParseAzureSecretReference(raw, "version-1"); err != nil {
			t.Fatalf("%s: %v", raw, err)
		} else if cloud.AuthorityHost == "" || cloud.VaultResource == "" {
			t.Fatalf("%s: incomplete cloud %#v", raw, cloud)
		}
	}
}

func TestValidateRejectsCredentialDestinationCollisions(t *testing.T) {
	c := Defaults()
	c.MCPServers = []MCPServer{
		{Name: "one", URL: "https://one.example/mcp", CredentialEnv: "SHARED_TOKEN"},
		{Name: "two", URL: "https://two.example/mcp", CredentialEnv: "SHARED_TOKEN"},
	}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "credential destination env") {
		t.Fatalf("mcp collision: %v", err)
	}

	c = Defaults()
	c.MCPServers = []MCPServer{{Name: "model", URL: "https://mcp.example/api", CredentialEnv: "XAI_API_KEY"}}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "credential destination env") {
		t.Fatalf("model collision: %v", err)
	}
}

func TestValidateRejectsUnsafeKeychainAccount(t *testing.T) {
	c := Defaults()
	c.Models[0].CredentialEnv = ""
	c.Models[0].Credential = &CredentialRef{Source: "keychain", Name: "safe-name; delete"}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "invalid keychain credential name") {
		t.Fatalf("got %v", err)
	}
}

func TestCredentialYAMLRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ABOX_HOME", "")
	c := Defaults()
	c.Models[0].CredentialEnv = ""
	c.Models[0].Credential = &CredentialRef{Source: "vault", Name: "secret/abox/grok", Field: "api_key", Version: "4"}
	c.MCPServers = []MCPServer{{
		Name: "gh", URL: "https://api.githubcopilot.com/mcp/",
		Credential: &CredentialRef{Source: "keychain", Name: "ABOX_MCP_GH_TOKEN"},
	}}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	got, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := c.Models[0].Credential
	if got.Models[0].Credential == nil || *got.Models[0].Credential != *want {
		t.Fatalf("model credential: %+v", got.Models[0].Credential)
	}
	if got.MCPServers[0].Credential == nil || got.MCPServers[0].Credential.Source != "keychain" {
		t.Fatalf("mcp credential: %+v", got.MCPServers[0].Credential)
	}
}

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

func TestEnsureLayoutScrubsLegacyCredentialsEvenWhenDestExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ABOX_HOME", "")
	legacy := filepath.Join(home, "Library", "Application Support", "ABox")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".abox"), 0o700); err != nil {
		t.Fatal(err)
	}
	modern := []byte("# ABox credentials. Mode 0600. Do not commit.\nXAI_API_KEY=modern-file-value\n")
	if err := os.WriteFile(filepath.Join(home, ".abox", "credentials.env"), modern, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "credentials.env"), []byte("XAI_API_KEY=legacy-file-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	gotModern, err := os.ReadFile(filepath.Join(home, ".abox", "credentials.env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotModern) != string(modern) {
		t.Fatalf("modern credentials rewritten: %q", gotModern)
	}
	gotLegacy, err := os.ReadFile(filepath.Join(legacy, "credentials.env"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gotLegacy), "legacy-file-value") || strings.Contains(string(gotLegacy), "XAI_API_KEY=") {
		t.Fatalf("legacy credentials not scrubbed: %q", gotLegacy)
	}
}

func TestEnsureLayoutSeedsThenScrubsLegacyCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ABOX_HOME", "")
	legacy := filepath.Join(home, "Library", "Application Support", "ABox")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "credentials.env"), []byte("XAI_API_KEY=legacy-file-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	gotModern, err := os.ReadFile(filepath.Join(home, ".abox", "credentials.env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gotModern), "legacy-file-value") {
		t.Fatalf("missing seeded credentials: %q", gotModern)
	}
	gotLegacy, err := os.ReadFile(filepath.Join(legacy, "credentials.env"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gotLegacy), "legacy-file-value") {
		t.Fatalf("source credentials not scrubbed: %q", gotLegacy)
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
