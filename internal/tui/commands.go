package tui

import (
	"strings"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/credentials"
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

func applyMCPKey(server config.MCPServer, key string) (string, error) {
	env := config.TokenEnv(server)
	if err := credentials.Save(env, key); err != nil {
		return "", err
	}
	credentials.SetEnv(env, key)
	return env, nil
}

func applyProviderKey(cfg config.File, choice config.ProviderProfile, key string) (config.Model, error) {
	if err := credentials.Save(choice.Env, key); err != nil {
		return config.Model{}, err
	}
	credentials.SetEnv(choice.Env, key)
	if m, ok := cfg.ModelNamed(choice.Name); ok {
		return m, nil
	}
	return choice.ModelConfig(), nil
}
