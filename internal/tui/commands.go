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
	{Name: "/help", Help: "List slash commands"},
}

type providerChoice struct {
	Label    string
	Provider string
	Profile  string
	Env      string
}

func providerChoices() []providerChoice {
	return []providerChoice{
		{Label: "Grok (xAI)", Provider: "xai", Profile: "grok-default", Env: "XAI_API_KEY"},
		{Label: "OpenAI", Provider: "openai", Profile: "openai-default", Env: "OPENAI_API_KEY"},
		{Label: "Anthropic", Provider: "anthropic", Profile: "claude-default", Env: "ANTHROPIC_API_KEY"},
	}
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

func applyProviderKey(cfg config.File, choice providerChoice, key string) (config.Model, error) {
	if err := credentials.Save(choice.Env, key); err != nil {
		return config.Model{}, err
	}
	credentials.SetEnv(choice.Env, key)
	if m, ok := cfg.ModelNamed(choice.Profile); ok {
		return m, nil
	}
	return config.Model{
		Name:          choice.Profile,
		Provider:      choice.Provider,
		Model:         choice.Profile,
		CredentialEnv: choice.Env,
	}, nil
}
