package tui

import (
	"context"
	"encoding/json"
	"os"
	"strings"
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

func TestApplyCloudCredentialWritesAzureRef(t *testing.T) {
	t.Setenv("ABOX_HOME", t.TempDir())
	cfg := config.Defaults()
	choice := config.DefaultProviders()[2]
	ref := config.CredentialRef{Source: "azure", Name: "https://testkv.vault.azure.net/secrets/anthropic"}
	got, sel, note, err := applyCloudCredential(cfg, choice, ref)
	if err != nil {
		t.Fatal(err)
	}
	if sel.CredentialEnv != "" || sel.Credential == nil || sel.Credential.Source != "azure" || sel.Credential.Name != ref.Name {
		t.Fatalf("selected model %+v", sel)
	}
	if !strings.Contains(note, "az login") {
		t.Fatalf("note %q", note)
	}
	persisted, _, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	model, ok := persisted.ModelNamed(choice.Name)
	if !ok || model.Credential == nil || model.Credential.Source != "azure" || model.Credential.Name != ref.Name {
		t.Fatalf("persisted %+v found=%v cfg=%+v", model, ok, got.Models)
	}
	body, err := os.ReadFile(config.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "sk-") || strings.Contains(string(body), "secret=") {
		t.Fatalf("config contained a secret: %s", body)
	}
}

func TestApplyCloudCredentialRejectsBadAzureURI(t *testing.T) {
	t.Setenv("ABOX_HOME", t.TempDir())
	cfg := config.Defaults()
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	choice := config.DefaultProviders()[2]
	_, _, _, err := applyCloudCredential(cfg, choice, config.CredentialRef{
		Source: "azure", Name: "https://evil.example/secrets/x",
	})
	if err == nil {
		t.Fatal("expected invalid azure URI to fail")
	}
	body, err := os.ReadFile(config.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "evil.example") {
		t.Fatalf("invalid ref was persisted: %s", body)
	}
}

func TestApplyCloudCredentialAddsMissingProfile(t *testing.T) {
	t.Setenv("ABOX_HOME", t.TempDir())
	cfg := config.Defaults()
	cfg.Models = cfg.Models[:1]
	choice := config.DefaultProviders()[1]
	got, sel, _, err := applyCloudCredential(cfg, choice, config.CredentialRef{
		Source: "vault", Name: "secret/abox/openai",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sel.Name != choice.Name || sel.Credential == nil || sel.Credential.Source != "vault" {
		t.Fatalf("selected %+v", sel)
	}
	if _, ok := got.ModelNamed(choice.Name); !ok {
		t.Fatalf("config missing profile: %+v", got.Models)
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
