package config

// ProviderProfile is one built-in model picker row and default config.Models entry.
type ProviderProfile struct {
	Label    string
	Name     string
	Provider string
	Model    string
	Env      string
	BaseURL  string
}

func DefaultProviders() []ProviderProfile {
	return []ProviderProfile{
		{Label: "Grok (xAI)", Name: "grok-default", Provider: "xai", Model: "grok-4", Env: "XAI_API_KEY", BaseURL: "https://api.x.ai/v1"},
		{Label: "OpenAI", Name: "openai-default", Provider: "openai", Model: "gpt-4.1", Env: "OPENAI_API_KEY", BaseURL: "https://api.openai.com/v1"},
		{Label: "Anthropic", Name: "claude-default", Provider: "anthropic", Model: "claude-sonnet-4-20250514", Env: "ANTHROPIC_API_KEY", BaseURL: "https://api.anthropic.com"},
	}
}

func (p ProviderProfile) ModelConfig() Model {
	return Model{
		Name:          p.Name,
		Provider:      p.Provider,
		Model:         p.Model,
		CredentialEnv: p.Env,
		BaseURL:       p.BaseURL,
	}
}

func ProviderCredentialEnvs() []string {
	ps := DefaultProviders()
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Env
	}
	return out
}

func defaultModels() []Model {
	ps := DefaultProviders()
	out := make([]Model, len(ps))
	for i, p := range ps {
		out[i] = p.ModelConfig()
	}
	return out
}
