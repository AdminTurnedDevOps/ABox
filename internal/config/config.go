package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/AdminTurnedDevOps/ABox/internal/vmmconfig"
	"github.com/AdminTurnedDevOps/ABox/protocol"
	"gopkg.in/yaml.v3"
)

const GuestImageName = "abox-guest.raw"

type File struct {
	Models       []Model      `yaml:"models"`
	Connectivity Connectivity `yaml:"connectivity"`
	MCPServers   []MCPServer  `yaml:"mcp_servers,omitempty"`
	Runtime      Runtime      `yaml:"runtime"`
	Resources    Resources    `yaml:"resources"`
}

type Model struct {
	Name          string         `yaml:"name"`
	Provider      string         `yaml:"provider"`
	Model         string         `yaml:"model"`
	CredentialEnv string         `yaml:"credential_env,omitempty"` // DEPRECATED: alias for credential {source: env, name: X}
	Credential    *CredentialRef `yaml:"credential,omitempty"`
	BaseURL       string         `yaml:"base_url,omitempty"`
}

type CredentialRef struct {
	Source  string `yaml:"source"`
	Name    string `yaml:"name"`
	Field   string `yaml:"field,omitempty"`   // vault/aws only
	Version string `yaml:"version,omitempty"` // vault/azure only
}

type AzureCloud struct {
	KeyVaultDNSSuffix string
	AuthorityHost     string
	VaultResource     string
}

var azureClouds = []AzureCloud{
	{KeyVaultDNSSuffix: "vault.azure.net", AuthorityHost: "https://login.microsoftonline.com", VaultResource: "https://vault.azure.net"},
	{KeyVaultDNSSuffix: "vault.usgovcloudapi.net", AuthorityHost: "https://login.microsoftonline.us", VaultResource: "https://vault.usgovcloudapi.net"},
	{KeyVaultDNSSuffix: "vault.azure.cn", AuthorityHost: "https://login.chinacloudapi.cn", VaultResource: "https://vault.azure.cn"},
	{KeyVaultDNSSuffix: "vault.microsoftazure.de", AuthorityHost: "https://login.microsoftonline.de", VaultResource: "https://vault.microsoftazure.de"},
}

type Connectivity struct {
	Mode        string `yaml:"mode"`
	Enforcement string `yaml:"enforcement,omitempty"`
}

type MCPServer struct {
	Name          string         `yaml:"name"`
	URL           string         `yaml:"url"`
	CredentialEnv string         `yaml:"credential_env,omitempty"` // DEPRECATED: alias for credential {source: env, name: X}
	Credential    *CredentialRef `yaml:"credential,omitempty"`
	ClientID      string         `yaml:"client_id,omitempty"`
	Scopes        []string       `yaml:"scopes,omitempty"`
	ToolAllowlist []string       `yaml:"tool_allowlist,omitempty"`
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
		Models:       defaultModels(),
		Connectivity: Connectivity{Mode: "direct"},
		Runtime: Runtime{
			Isolation: "microvm",
			Backend:   "libkrun",
			Network:   "deny-by-default",
		},
		Resources: Resources{VCPU: vmmconfig.DefaultVCPU, RAMMiB: vmmconfig.DefaultRAMMiB},
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
	if err := os.MkdirAll(filepath.Join(Dir(), "images"), 0o700); err != nil {
		return fmt.Errorf("create images dir: %w", err)
	}
	if err := seedFromLegacy(); err != nil {
		return err
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

func seedFromLegacy() error {
	legacy := LegacyAppSupportDir()
	if legacy == "" {
		return nil
	}
	for _, name := range []string{"config.yaml", "credentials.env"} {
		dst := filepath.Join(Dir(), name)
		if exists(dst) {
			continue
		}
		src := filepath.Join(legacy, name)
		data, err := os.ReadFile(src)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			return fmt.Errorf("seed %s: %w", name, err)
		}
	}
	return scrubLegacyAppSupportCredentials(legacy)
}

// LegacyAppSupportDir is the pre-~/.abox macOS location. Dir() never returns
// it; it is only used to seed a missing ~/.abox and to scrub leftover secrets.
func LegacyAppSupportDir() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "ABox")
}

func scrubLegacyAppSupportCredentials(legacy string) error {
	path := filepath.Join(legacy, "credentials.env")
	if !exists(path) {
		return nil
	}
	body := []byte("# ABox credentials. Mode 0600. Do not commit.\n# Leftover Application Support copy; credentials now live under ~/.abox or the macOS keychain.\n")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return fmt.Errorf("scrub legacy credentials: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("scrub legacy credentials: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("scrub legacy credentials: %w", err)
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
	for i, m := range c.Models {
		if err := m.validate(); err != nil {
			return fmt.Errorf("models[%d]: %w", i, err)
		}
	}
	destinations := make(map[string]string, len(c.Models)+len(c.MCPServers))
	for _, m := range c.Models {
		if _, exists := destinations[m.EnvName()]; !exists {
			destinations[m.EnvName()] = fmt.Sprintf("model %q", m.Name)
		}
	}
	for _, s := range c.MCPServers {
		dest := TokenEnv(s)
		if previous, exists := destinations[dest]; exists {
			return fmt.Errorf("credential destination env %q is shared by %s and mcp server %q", dest, previous, s.Name)
		}
		destinations[dest] = fmt.Sprintf("mcp server %q", s.Name)
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
	if s.CredentialEnv != "" && s.Credential != nil {
		return fmt.Errorf("set either credential or credential_env, not both")
	}
	if s.CredentialEnv != "" && !ValidEnvName(s.CredentialEnv) {
		return fmt.Errorf("invalid credential_env %q", s.CredentialEnv)
	}
	if s.Credential != nil {
		if err := s.Credential.validate(); err != nil {
			return fmt.Errorf("credential: %w", err)
		}
	}
	return nil
}

func (m Model) validate() error {
	if m.BaseURL != "" {
		if err := validateHTTPSURL("base_url", m.BaseURL); err != nil {
			return err
		}
	}
	if m.CredentialEnv != "" && m.Credential != nil {
		return fmt.Errorf("set either credential or credential_env, not both")
	}
	if m.CredentialEnv != "" && !ValidEnvName(m.CredentialEnv) {
		return fmt.Errorf("invalid credential_env %q", m.CredentialEnv)
	}
	if m.Credential != nil {
		if err := m.Credential.validate(); err != nil {
			return fmt.Errorf("credential: %w", err)
		}
	}
	return nil
}

var credentialSources = map[string]struct{}{
	"env":      {},
	"keychain": {},
	"vault":    {},
	"azure":    {},
	"aws":      {},
}

func (c CredentialRef) validate() error {
	if _, ok := credentialSources[c.Source]; !ok {
		return fmt.Errorf("unknown source %q (want env, keychain, vault, azure, or aws)", c.Source)
	}
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("name is required")
	}
	switch c.Source {
	case "env", "keychain":
		if c.Field != "" {
			return fmt.Errorf("field is not supported for source %q", c.Source)
		}
		if c.Version != "" {
			return fmt.Errorf("version is not supported for source %q", c.Source)
		}
		if !ValidEnvName(c.Name) {
			return fmt.Errorf("invalid %s credential name %q", c.Source, c.Name)
		}
	case "vault":
		if c.Version != "" && !isNumeric(c.Version) {
			return fmt.Errorf("version %q must be numeric for vault", c.Version)
		}
	case "aws":
		if c.Version != "" {
			return fmt.Errorf("version is not supported for source %q", c.Source)
		}
	case "azure":
		if c.Field != "" {
			return fmt.Errorf("field is not supported for source %q", c.Source)
		}
		if _, _, _, _, err := ParseAzureSecretReference(c.Name, c.Version); err != nil {
			return err
		}
	}
	return nil
}

// ParseAzureSecretReference validates and canonicalizes an Azure Key Vault
// secret identifier. Only first-party data-plane DNS suffixes are accepted so
// callers can safely attach an Azure bearer token to the returned URI.
func ParseAzureSecretReference(name, requestedVersion string) (uri, secretName, version string, cloud AzureCloud, err error) {
	raw := strings.TrimSpace(name)
	u, parseErr := url.Parse(raw)
	if parseErr != nil || u.Scheme != "https" || u.Opaque != "" || u.Host == "" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		err = fmt.Errorf("azure credential name must be an https secret URI for Azure Key Vault like https://vault.vault.azure.net/secrets/name")
		return
	}
	host := strings.ToLower(u.Hostname())
	for _, candidate := range azureClouds {
		suffix := "." + candidate.KeyVaultDNSSuffix
		if strings.HasSuffix(host, suffix) {
			vaultName := strings.TrimSuffix(host, suffix)
			if strings.Contains(vaultName, ".") || !validAzureVaultName(vaultName) {
				break
			}
			cloud = candidate
			break
		}
	}
	if cloud.KeyVaultDNSSuffix == "" {
		err = fmt.Errorf("azure credential host %q is not a supported Azure Key Vault endpoint", u.Hostname())
		return
	}
	if u.RawPath != "" {
		err = fmt.Errorf("azure credential path must not contain escaped characters")
		return
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 2 || len(parts) > 3 || parts[0] != "secrets" || !validAzureSecretPart(parts[1]) {
		err = fmt.Errorf("azure credential name must point at /secrets/<name> with an optional version")
		return
	}
	secretName = parts[1]
	if len(parts) == 3 {
		if !validAzureSecretPart(parts[2]) {
			err = fmt.Errorf("azure credential URI has an invalid secret version")
			return
		}
		version = parts[2]
	} else if requestedVersion != "" {
		if !validAzureSecretPart(requestedVersion) {
			err = fmt.Errorf("azure credential version %q is invalid", requestedVersion)
			return
		}
		version = requestedVersion
	}
	u.Scheme = "https"
	u.Host = host
	u.Path = "/secrets/" + secretName
	if version != "" {
		u.Path += "/" + version
	}
	uri = u.String()
	return
}

func validAzureVaultName(name string) bool {
	if len(name) < 3 || len(name) > 24 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	if last := name[len(name)-1]; !asciiAlphaNumeric(last) {
		return false
	}
	for i := 1; i < len(name)-1; i++ {
		if !asciiAlphaNumeric(name[i]) && name[i] != '-' {
			return false
		}
	}
	return true
}

func validAzureSecretPart(part string) bool {
	if len(part) == 0 || len(part) > 127 {
		return false
	}
	for i := range part {
		if !asciiAlphaNumeric(part[i]) && part[i] != '-' {
			return false
		}
	}
	return true
}

func asciiAlphaNumeric(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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

func ValidEnvName(name string) bool {
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

func (m Model) EnvName() string {
	if m.CredentialEnv != "" {
		return m.CredentialEnv
	}
	for _, p := range DefaultProviders() {
		if p.Provider == m.Provider {
			return p.Env
		}
	}
	n := strings.ToUpper(strings.ReplaceAll(m.Name, "-", "_"))
	return "ABOX_MODEL_" + n + "_KEY"
}

func (m Model) CredentialReference() CredentialRef {
	if m.Credential != nil {
		return *m.Credential
	}
	if m.CredentialEnv != "" {
		return CredentialRef{Source: "env", Name: m.CredentialEnv}
	}
	return CredentialRef{Source: "env", Name: m.EnvName()}
}

func (s MCPServer) CredentialReference() CredentialRef {
	if s.Credential != nil {
		return *s.Credential
	}
	return CredentialRef{Source: "env", Name: TokenEnv(s)}
}

func (m Model) ToGuest() protocol.GuestModel {
	return protocol.GuestModel{
		Name:          m.Name,
		Provider:      m.Provider,
		Model:         m.Model,
		CredentialEnv: m.EnvName(),
		BaseURL:       m.BaseURL,
	}
}

func ModelFromGuest(m protocol.GuestModel) Model {
	return Model{
		Name:          m.Name,
		Provider:      m.Provider,
		Model:         m.Model,
		CredentialEnv: m.CredentialEnv,
		BaseURL:       m.BaseURL,
	}
}

func (r Resources) Resolved() (vcpu, ram int) {
	vcpu, ram = r.VCPU, r.RAMMiB
	if vcpu == 0 {
		vcpu = vmmconfig.DefaultVCPU
	}
	if ram == 0 {
		ram = vmmconfig.DefaultRAMMiB
	}
	return
}

func GuestImagePath() string {
	return filepath.Join(ImageDir(), GuestImageName)
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

// AddMCPServer records a Streamable HTTP MCP server and sets connectivity.mode.
// mode must be "direct" or "agentgateway". Same name upserts. Agentgateway
// with required enforcement keeps exactly one origin.
func (c *File) AddMCPServer(mode string, srv MCPServer) error {
	switch mode {
	case "direct", "agentgateway":
	default:
		return fmt.Errorf("mode must be direct or agentgateway")
	}
	if err := srv.validate(); err != nil {
		return err
	}
	c.Connectivity.Mode = mode
	if mode == "agentgateway" {
		if c.Connectivity.Enforcement == "" {
			c.Connectivity.Enforcement = "required"
		}
		if c.Connectivity.Enforcement == "required" {
			for _, existing := range c.MCPServers {
				if existing.Name != srv.Name {
					return fmt.Errorf("agentgateway required allows exactly one mcp_servers entry (already have %q)", existing.Name)
				}
			}
			c.MCPServers = []MCPServer{srv}
			return c.Validate()
		}
	}
	for i, existing := range c.MCPServers {
		if existing.Name == srv.Name {
			c.MCPServers[i] = srv
			return c.Validate()
		}
	}
	c.MCPServers = append(c.MCPServers, srv)
	return c.Validate()
}

func (c File) Save() error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := EnsureLayout(); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	path := Path()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
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

// Dir is always ~/.abox unless ABOX_HOME is set. First run creates it.
func Dir() string {
	if override := strings.TrimSpace(os.Getenv("ABOX_HOME")); override != "" {
		return override
	}
	home := homeDir()
	if home == "" {
		return filepath.Join(".", "var", "abox")
	}
	return filepath.Join(home, ".abox")
}

func AppSupportDir() string { return Dir() }

func CacheDir() string {
	return filepath.Join(Dir(), "cache")
}

func ImageDir() string {
	modern := filepath.Join(Dir(), "images")
	if exists(filepath.Join(modern, GuestImageName)) {
		return modern
	}
	home := homeDir()
	if home != "" {
		legacy := filepath.Join(home, "Library", "Caches", "ABox", "images")
		if exists(filepath.Join(legacy, GuestImageName)) {
			return legacy
		}
	}
	return modern
}

func SessionRoot() string {
	return filepath.Join(Dir(), "sessions")
}
