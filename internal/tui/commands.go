package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/credsource"
)

type slashCmd struct {
	Name string
	Help string
}

var slashCommands = []slashCmd{
	{Name: "/provider", Help: "Connect Grok, OpenAI, or Anthropic and set an API key"},
	{Name: "/mcp", Help: "List MCP servers and paste a Bearer token (OAuth: abox mcp login)"},
	{Name: "/help", Help: "List slash commands"},
}

func providerChoices() []config.ProviderProfile {
	return config.DefaultProviders()
}

func filterSlash(q string) []slashCmd {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" || q == "/" {
		return slashCommands
	}
	var out []slashCmd
	for _, c := range slashCommands {
		if strings.HasPrefix(c.Name, q) {
			out = append(out, c)
		}
	}
	return out
}

func mcpServers(cfg config.File) []config.MCPServer {
	servers, err := cfg.ResolvedMCPServers()
	if err != nil {
		return nil
	}
	return servers
}

var saveCredential = func(envName, value string) (credsource.SaveResult, error) {
	return credsource.SavePreferred(context.Background(), envName, value)
}

func applyMCPKey(cfg config.File, server config.MCPServer, key string) (config.File, string, string, error) {
	env := config.TokenEnv(server)
	res, err := saveCredential(env, key)
	if err != nil {
		return cfg, "", "", err
	}
	found := false
	for i, s := range cfg.MCPServers {
		if s.Name == server.Name {
			found = true
			cfg.MCPServers[i].CredentialEnv = ""
			cfg.MCPServers[i].Credential = &config.CredentialRef{Source: res.Source, Name: env}
			if err := cfg.Save(); err != nil {
				return cfg, env, res.Note, fmt.Errorf("config update: %w", err)
			}
			break
		}
	}
	if !found {
		return cfg, env, res.Note, fmt.Errorf("config update did not retain MCP server %q", server.Name)
	}
	return cfg, env, res.Note, nil
}

func applyProviderKey(cfg config.File, choice config.ProviderProfile, key string) (config.File, config.Model, string, error) {
	res, err := saveCredential(choice.Env, key)
	if err != nil {
		return cfg, config.Model{}, "", err
	}
	found := false
	changed := false
	for i, m := range cfg.Models {
		if m.Name == choice.Name {
			found = true
			cfg.Models[i].CredentialEnv = ""
			cfg.Models[i].Credential = &config.CredentialRef{Source: res.Source, Name: choice.Env}
			changed = true
		}
	}
	if !found {
		model := choice.ModelConfig()
		model.CredentialEnv = ""
		model.Credential = &config.CredentialRef{Source: res.Source, Name: choice.Env}
		cfg.Models = append(cfg.Models, model)
		changed = true
	}
	if changed {
		if err := cfg.Save(); err != nil {
			return cfg, config.Model{}, res.Note, fmt.Errorf("config update: %w", err)
		}
	}
	sel, ok := cfg.ModelNamed(choice.Name)
	if !ok {
		return cfg, config.Model{}, res.Note, fmt.Errorf("config update did not retain model profile %q", choice.Name)
	}
	return cfg, sel, res.Note, nil
}
