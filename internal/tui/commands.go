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
	{Name: "/credential", Help: "Point a model at Vault, Azure Key Vault, or AWS Secrets Manager"},
	{Name: "/mcp", Help: "List MCP servers and paste a Bearer token (OAuth: abox mcp login)"},
	{Name: "/help", Help: "List slash commands"},
}

type cloudCredSource struct {
	Source      string
	Label       string
	Placeholder string
	Prompt      string
	Note        string
}

func cloudCredentialChoices() []cloudCredSource {
	return []cloudCredSource{
		{Source: "vault", Label: "HashiCorp Vault", Placeholder: "secret/abox/anthropic", Prompt: "path> ", Note: "host needs VAULT_ADDR and VAULT_TOKEN (or ~/.vault-token)"},
		{Source: "azure", Label: "Azure Key Vault", Placeholder: "https://myvault.vault.azure.net/secrets/name", Prompt: "uri> ", Note: "host needs AZURE_CLIENT_ID/TENANT_ID/SECRET or az login"},
		{Source: "aws", Label: "AWS Secrets Manager", Placeholder: "abox/anthropic", Prompt: "id> ", Note: "host uses AWS_* env or ~/.aws/credentials"},
	}
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

func applyCloudCredential(cfg config.File, choice config.ProviderProfile, ref config.CredentialRef) (config.File, config.Model, string, error) {
	ref.Name = strings.TrimSpace(ref.Name)
	next := cfg
	next.Models = append([]config.Model(nil), cfg.Models...)
	found := false
	for i, m := range next.Models {
		if m.Name == choice.Name {
			next.Models[i].CredentialEnv = ""
			cred := ref
			next.Models[i].Credential = &cred
			found = true
		}
	}
	if !found {
		model := choice.ModelConfig()
		model.CredentialEnv = ""
		cred := ref
		model.Credential = &cred
		next.Models = append(next.Models, model)
	}
	if err := next.Save(); err != nil {
		return cfg, config.Model{}, "", err
	}
	sel, ok := next.ModelNamed(choice.Name)
	if !ok {
		return cfg, config.Model{}, "", fmt.Errorf("config update did not retain model profile %q", choice.Name)
	}
	note := ref.Source + " " + ref.Name
	for _, src := range cloudCredentialChoices() {
		if src.Source == ref.Source {
			note = src.Note
			break
		}
	}
	return next, sel, note, nil
}
