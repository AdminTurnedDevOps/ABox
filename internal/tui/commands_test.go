package tui

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/credsource"
	"github.com/AdminTurnedDevOps/ABox/internal/runtime"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

func TestApplyProviderKeyAddsMissingProfileWithCurrentReference(t *testing.T) {
	t.Setenv("ABOX_HOME", t.TempDir())
	original := saveCredential
	t.Cleanup(func() { saveCredential = original })
	saveCredential = func(envName, value string) (credsource.SaveResult, error) {
		if envName != "OPENAI_API_KEY" || value != "secret" {
			t.Fatalf("save %q %q", envName, value)
		}
		return credsource.SaveResult{Source: "keychain", Keychain: true, Note: "test"}, nil
	}

	cfg := config.Defaults()
	cfg.Models = append(cfg.Models[:1:1], cfg.Models[2])
	choice := config.DefaultProviders()[1]
	got, sel, _, err := applyProviderKey(cfg, choice, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if sel.Name != choice.Name || sel.Credential == nil || sel.Credential.Source != "keychain" || sel.Credential.Name != choice.Env {
		t.Fatalf("selected model %+v", sel)
	}
	persisted, _, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	model, ok := persisted.ModelNamed(choice.Name)
	if !ok || model.Credential == nil || model.Credential.Name != choice.Env {
		t.Fatalf("persisted model %+v found=%v; cfg=%+v", model, ok, got.Models)
	}
}

func TestApplyProviderKeyFallbackReplacesExplicitCloudReference(t *testing.T) {
	t.Setenv("ABOX_HOME", t.TempDir())
	original := saveCredential
	t.Cleanup(func() { saveCredential = original })
	saveCredential = func(envName, value string) (credsource.SaveResult, error) {
		return credsource.SaveResult{Source: "env", Note: "fallback"}, nil
	}

	cfg := config.Defaults()
	choice := config.DefaultProviders()[0]
	cfg.Models[0].CredentialEnv = ""
	cfg.Models[0].Credential = &config.CredentialRef{Source: "vault", Name: "secret/abox/xai"}
	got, sel, _, err := applyProviderKey(cfg, choice, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if sel.CredentialEnv != "" || sel.Credential == nil || sel.Credential.Source != "env" || sel.Credential.Name != choice.Env {
		t.Fatalf("selected model retained stale reference: %+v", sel)
	}
	if got.Models[0].Credential == nil || got.Models[0].Credential.Source != "env" {
		t.Fatalf("updated config %+v", got.Models[0])
	}
	persisted, _, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	persistedModel, ok := persisted.ModelNamed(choice.Name)
	if !ok || persistedModel.Credential == nil || persistedModel.Credential.Source != "env" {
		t.Fatalf("persisted model retained stale reference: %+v", persistedModel)
	}
}

func TestApplyMCPKeyFallbackReplacesExplicitNonEnvReference(t *testing.T) {
	t.Setenv("ABOX_HOME", t.TempDir())
	original := saveCredential
	t.Cleanup(func() { saveCredential = original })
	saveCredential = func(envName, value string) (credsource.SaveResult, error) {
		return credsource.SaveResult{Source: "env", Note: "fallback"}, nil
	}

	server := config.MCPServer{
		Name: "github", URL: "https://api.githubcopilot.com/mcp/",
		Credential: &config.CredentialRef{Source: "aws", Name: "abox/github-token"},
	}
	cfg := config.Defaults()
	cfg.MCPServers = []config.MCPServer{server}
	got, env, _, err := applyMCPKey(cfg, server, "secret")
	if err != nil {
		t.Fatal(err)
	}
	ref := got.MCPServers[0].Credential
	if got.MCPServers[0].CredentialEnv != "" || ref == nil || ref.Source != "env" || ref.Name != env {
		t.Fatalf("MCP server retained stale reference: %+v", got.MCPServers[0])
	}
	persisted, _, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ref = persisted.MCPServers[0].Credential
	if ref == nil || ref.Source != "env" || ref.Name != env {
		t.Fatalf("persisted MCP server retained stale reference: %+v", persisted.MCPServers[0])
	}
}

func TestUpdateHostBrokerUsesCurrentModelConfig(t *testing.T) {
	cfg := config.Defaults()
	cfg.Models = []config.Model{{
		Name: "updated", Provider: "openai", Model: "gpt-current", CredentialEnv: "CURRENT_API_KEY",
	}}
	sb := &runtime.Sandbox{}
	m := model{sandbox: sb, resolver: credsource.NewResolver()}
	t.Cleanup(func() { _ = m.resolver.Close() })
	m.updateHostBroker(cfg)

	raw, err := json.Marshal(protocol.ProviderOpenParams{Model: "updated"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, perr := sb.OnGuestCall.Handle(ctx, "provider_open", raw, nil); perr != nil {
		t.Fatalf("updated broker rejected current model: %v", perr)
	}
}
