package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type File struct {
	Models       []Model      `yaml:"models"`
	Connectivity Connectivity `yaml:"connectivity"`
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
	Endpoint    string `yaml:"endpoint,omitempty"`
	Enforcement string `yaml:"enforcement,omitempty"`
}

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
	cfg := Defaults()
	path := Path()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, path, nil
		}
		return cfg, path, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, path, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, path, err
	}
	return cfg, path, nil
}

func (c File) Validate() error {
	switch c.Connectivity.Mode {
	case "", "offline", "direct", "agentgateway":
	default:
		return fmt.Errorf("unknown connectivity mode %q", c.Connectivity.Mode)
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
	return filepath.Join(AppSupportDir(), "config.yaml")
}

func AppSupportDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "var", "abox")
	}
	return filepath.Join(home, "Library", "Application Support", "ABox")
}

func CacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "var", "cache")
	}
	return filepath.Join(home, "Library", "Caches", "ABox")
}

func ImageDir() string {
	return filepath.Join(CacheDir(), "images")
}

func SessionRoot() string {
	return filepath.Join(AppSupportDir(), "sessions")
}
