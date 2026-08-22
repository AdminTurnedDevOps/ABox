package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type File struct {
	Models       []Model      `yaml:"models"`
	Connectivity Connectivity `yaml:"connectivity"`
	MCPServers   []MCPServer  `yaml:"mcp_servers,omitempty"`
	Runtime      Runtime      `yaml:"runtime"`
	Resources    Resources    `yaml:"resources"`
}

type Model struct {
	Name          string `yaml:"name"`
	Provider      string `yaml:"provider"`
	Model         string `yaml:"model"`
	CredentialEnv string `yaml:"credential_env"`
	BaseURL       string `yaml:"base_url,omitempty"`
}

type Connectivity struct {
	Mode        string `yaml:"mode"`
	Enforcement string `yaml:"enforcement,omitempty"`
}

type MCPServer struct {
	Name          string   `yaml:"name"`
	URL           string   `yaml:"url"`
	CredentialEnv string   `yaml:"credential_env,omitempty"`
	ClientID      string   `yaml:"client_id,omitempty"`
	Scopes        []string `yaml:"scopes,omitempty"`
	ToolAllowlist []string `yaml:"tool_allowlist,omitempty"`
}

var mcpNameRE = regexp.MustCompile(`^[a-z0-9-]+$`)

type Runtime struct {
	Isolation string `yaml:"isolation"`
	Backend   string `yaml:"backend"`
	Network   string `yaml:"network"`
	Image     string `yaml:"image,omitempty"`
	VMMPath   string `yaml:"vmm_path,omitempty"`
}

type Resources struct {
	VCPU   int `yaml:"vcpu"`
	RAMMiB int `yaml:"ram_mib"`
}

func Defaults() File {
	return File{
		Models: []Model{
			{Name: "grok-default", Provider: "xai", Model: "grok-4", CredentialEnv: "XAI_API_KEY", BaseURL: "https://api.x.ai/v1"},
			{Name: "openai-default", Provider: "openai", Model: "gpt-4.1", CredentialEnv: "OPENAI_API_KEY", BaseURL: "https://api.openai.com/v1"},
			{Name: "claude-default", Provider: "anthropic", Model: "claude-sonnet-4-20250514", CredentialEnv: "ANTHROPIC_API_KEY", BaseURL: "https://api.anthropic.com"},
		},
		Connectivity: Connectivity{Mode: "direct"},
		Runtime: Runtime{
			Isolation: "microvm",
			Backend:   "libkrun",
			Network:   "deny-by-default",
		},
		Resources: Resources{VCPU: 1, RAMMiB: 768},
	}
}

func Load() (File, string, error) {
	if err := EnsureLayout(); err != nil {
		return File{}, Path(), err
	}
	cfg := Defaults()
	path := Path()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, path, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, path, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, path, err
	}
	return cfg, path, nil
}

// EnsureLayout creates the ABox home directory on first run (0700), plus
// sessions/ and images/. If config.yaml is missing, it writes Defaults().
// Other CLI harnesses do the same: Codex writes ~/.codex on first run,
// OpenCode creates ~/.config/opencode and the config file, Claude Code
// creates ~/.claude when it first persists settings or session data.
func EnsureLayout() error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", Dir(), err)
	}
	if err := os.MkdirAll(SessionRoot(), 0o700); err != nil {
		return fmt.Errorf("create sessions dir: %w", err)
	}
	if err := os.MkdirAll(ImageDir(), 0o700); err != nil {
		return fmt.Errorf("create images dir: %w", err)
	}
	path := Path()
	if exists(path) {
		return nil
	}
	data, err := yaml.Marshal(Defaults())
	if err != nil {
		return fmt.Errorf("marshal default config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func (c File) Validate() error {
	switch c.Connectivity.Mode {
	case "", "offline", "direct", "agentgateway":
	default:
		return fmt.Errorf("unknown connectivity mode %q", c.Connectivity.Mode)
	}
	switch c.Connectivity.Enforcement {
	case "", "optional", "required":
	default:
		return fmt.Errorf("unknown connectivity enforcement %q", c.Connectivity.Enforcement)
	}
	if c.Connectivity.Mode == "agentgateway" {
		if len(c.MCPServers) == 0 {
			return fmt.Errorf("agentgateway mode requires mcp_servers with the gateway URL")
		}
		if c.Connectivity.Enforcement == "required" && len(c.MCPServers) != 1 {
			return fmt.Errorf("agentgateway required allows exactly one mcp_servers entry")
		}
	}
	seen := map[string]struct{}{}
	for i, s := range c.MCPServers {
		if err := s.validate(); err != nil {
			return fmt.Errorf("mcp_servers[%d]: %w", i, err)
		}
		if _, ok := seen[s.Name]; ok {
			return fmt.Errorf("duplicate mcp server name %q", s.Name)
		}
		seen[s.Name] = struct{}{}
	}
	if c.Runtime.Isolation != "" && c.Runtime.Isolation != "microvm" {
		return fmt.Errorf("isolation must be microvm")
	}
	if c.Runtime.Backend != "" && c.Runtime.Backend != "libkrun" {
		return fmt.Errorf("backend must be libkrun")
	}
	if c.Resources.VCPU < 0 || c.Resources.RAMMiB < 0 {
		return fmt.Errorf("resources must be non-negative")
	}
	return nil
}

func (s MCPServer) validate() error {
	if !mcpNameRE.MatchString(s.Name) {
		return fmt.Errorf("name %q must match %s", s.Name, mcpNameRE)
	}
	if err := validateHTTPSURL("url", s.URL); err != nil {
		return err
	}
	if s.CredentialEnv != "" && !validEnvName(s.CredentialEnv) {
		return fmt.Errorf("invalid credential_env %q", s.CredentialEnv)
	}
	return nil
}

func validateHTTPSURL(field, raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("%s is required", field)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("%s must be https", field)
	}
	if u.User != nil {
		return fmt.Errorf("%s must not contain userinfo", field)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%s host is required", field)
	}
	if ip := net.ParseIP(host); ip != nil {
		return fmt.Errorf("%s must not be an IP literal", field)
	}
	return nil
}

func validEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'A' && r <= 'Z', r == '_':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// ResolvedMCPServers returns the MCP URLs the guest may dial.
// Offline yields none. Direct and agentgateway both use mcp_servers[].url.
func (c File) ResolvedMCPServers() ([]MCPServer, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.Connectivity.Mode == "offline" {
		return nil, nil
	}
	return c.MCPServers, nil
}

func TokenEnv(server MCPServer) string {
	if server.CredentialEnv != "" {
		return server.CredentialEnv
	}
	n := strings.ToUpper(strings.ReplaceAll(server.Name, "-", "_"))
	return "ABOX_MCP_" + n + "_TOKEN"
}

func (c File) ModelNamed(name string) (Model, bool) {
	for _, m := range c.Models {
		if m.Name == name {
			return m, true
		}
	}
	if name == "" && len(c.Models) > 0 {
		return c.Models[0], true
	}
	return Model{}, false
}

func (m Model) CredentialPresent() bool {
	if m.CredentialEnv == "" {
		return false
	}
	return strings.TrimSpace(os.Getenv(m.CredentialEnv)) != ""
}

func Path() string {
	return filepath.Join(Dir(), "config.yaml")
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Dir is ~/.abox, matching other CLI harnesses. ABOX_HOME overrides.
// If ~/.abox does not exist yet but the old macOS Application Support
// tree does, that legacy path is used so existing installs keep working.
func Dir() string {
	if override := strings.TrimSpace(os.Getenv("ABOX_HOME")); override != "" {
		return override
	}
	home := homeDir()
	if home == "" {
		return filepath.Join(".", "var", "abox")
	}
	modern := filepath.Join(home, ".abox")
	legacy := filepath.Join(home, "Library", "Application Support", "ABox")
	if exists(modern) {
		return modern
	}
	if exists(legacy) {
		return legacy
	}
	return modern
}

func AppSupportDir() string { return Dir() }

func CacheDir() string {
	return filepath.Join(Dir(), "cache")
}

func ImageDir() string {
	modern := filepath.Join(Dir(), "images")
	if exists(modern) {
		return modern
	}
	home := homeDir()
	if home != "" {
		legacy := filepath.Join(home, "Library", "Caches", "ABox", "images")
		if exists(legacy) {
			return legacy
		}
	}
	return modern
}

func SessionRoot() string {
	return filepath.Join(Dir(), "sessions")
}
