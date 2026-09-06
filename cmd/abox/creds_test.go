package main

import (
	"context"
	"errors"
	"testing"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/credentials"
)

func TestCredsMigrateRemovesSuccessAndRefreshButKeepsFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ABOX_HOME", "")
	envRef := config.CredentialRef{Source: "env", Name: "CUSTOM_SOURCE"}
	cfg := config.Defaults()
	cfg.Models = []config.Model{
		{Name: "custom", Provider: "other", Credential: &envRef},
		{Name: "failed", Provider: "other", CredentialEnv: "FAILED_KEY"},
		{Name: "refresh-model", Provider: "other", CredentialEnv: "MODEL_REFRESH"},
	}
	cfg.MCPServers = []config.MCPServer{
		{Name: "refresh-mcp", URL: "https://mcp.example/api", CredentialEnv: "MCP_CRED_REFRESH"},
		{Name: "normal-mcp", URL: "https://normal.example/api", CredentialEnv: "MCP_TOKEN"},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"CUSTOM_SOURCE":            "custom-value",
		"FAILED_KEY":               "failed-value",
		"MODEL_REFRESH":            "model-value",
		"MCP_CRED_REFRESH":         "mcp-value",
		"MCP_CRED_REFRESH_REFRESH": "legacy-refresh-value",
		"MCP_TOKEN_REFRESH":        "legacy-normal-refresh-value",
	} {
		if err := credentials.Save(name, value); err != nil {
			t.Fatal(err)
		}
	}

	origAvailable := migrationKeychainAvailable
	origSet := migrationSetKeychain
	migrationKeychainAvailable = func() bool { return true }
	called := map[string]bool{}
	migrationSetKeychain = func(_ context.Context, name string, _ []byte) error {
		called[name] = true
		if name == "FAILED_KEY" || name == "MODEL_REFRESH" || name == "MCP_CRED_REFRESH" {
			return errors.New("write failed")
		}
		return nil
	}
	t.Cleanup(func() {
		migrationKeychainAvailable = origAvailable
		migrationSetKeychain = origSet
	})

	if err := credsMigrate(); err != nil {
		t.Fatal(err)
	}
	remaining, err := credentials.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 3 || remaining["FAILED_KEY"] != "failed-value" || remaining["MODEL_REFRESH"] != "model-value" || remaining["MCP_CRED_REFRESH"] != "mcp-value" {
		t.Fatalf("remaining credentials %#v", remaining)
	}
	if called["MCP_CRED_REFRESH_REFRESH"] || called["MCP_TOKEN_REFRESH"] {
		t.Fatal("known legacy refresh token was sent to the keychain")
	}
	if !called["MODEL_REFRESH"] || !called["MCP_CRED_REFRESH"] {
		t.Fatalf("configured refresh-suffixed credentials were dropped: calls %#v", called)
	}
	savedCfg, _, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := savedCfg.Models[0].CredentialReference(); got != (config.CredentialRef{Source: "keychain", Name: "CUSTOM_SOURCE"}) {
		t.Fatalf("custom reference %#v", got)
	}
	if got := savedCfg.Models[1].CredentialReference(); got != (config.CredentialRef{Source: "env", Name: "FAILED_KEY"}) {
		t.Fatalf("failed reference %#v", got)
	}
	if got := savedCfg.Models[2].CredentialReference(); got != (config.CredentialRef{Source: "env", Name: "MODEL_REFRESH"}) {
		t.Fatalf("refresh model reference %#v", got)
	}
	if got := savedCfg.MCPServers[0].CredentialReference(); got != (config.CredentialRef{Source: "env", Name: "MCP_CRED_REFRESH"}) {
		t.Fatalf("refresh mcp reference %#v", got)
	}
}

func TestUpsertCredentialRefsUsesEffectiveEnvReference(t *testing.T) {
	vault := config.CredentialRef{Source: "vault", Name: "secret/abox/vault"}
	azure := config.CredentialRef{Source: "azure", Name: "https://testkv.vault.azure.net/secrets/azure"}
	aws := config.CredentialRef{Source: "aws", Name: "prod/aws"}
	customEnv := config.CredentialRef{Source: "env", Name: "CUSTOM_ENV"}
	customMCPEnv := config.CredentialRef{Source: "env", Name: "CUSTOM_MCP_ENV"}
	cfg := config.File{
		Models: []config.Model{
			{Name: "vault", Provider: "other", Credential: &vault},
			{Name: "azure", Provider: "other", Credential: &azure},
			{Name: "aws", Provider: "other", Credential: &aws},
			{Name: "custom", Provider: "other", Credential: &customEnv},
		},
		MCPServers: []config.MCPServer{{Name: "custom", URL: "https://mcp.example/api", Credential: &customMCPEnv}},
	}

	for i := 0; i < 3; i++ {
		if upsertCredentialRefs(&cfg, cfg.Models[i].EnvName()) {
			t.Fatalf("cloud reference %d was changed", i)
		}
	}
	if cfg.Models[0].CredentialReference() != vault || cfg.Models[1].CredentialReference() != azure || cfg.Models[2].CredentialReference() != aws {
		t.Fatalf("cloud references changed: %#v", cfg.Models)
	}
	if !upsertCredentialRefs(&cfg, "CUSTOM_ENV") {
		t.Fatal("custom env reference was not changed")
	}
	if got := cfg.Models[3].CredentialReference(); got != (config.CredentialRef{Source: "keychain", Name: "CUSTOM_ENV"}) {
		t.Fatalf("custom env reference %#v", got)
	}
	if !upsertCredentialRefs(&cfg, "CUSTOM_MCP_ENV") {
		t.Fatal("custom MCP env reference was not changed")
	}
	if got := cfg.MCPServers[0].CredentialReference(); got != (config.CredentialRef{Source: "keychain", Name: "CUSTOM_MCP_ENV"}) {
		t.Fatalf("custom MCP env reference %#v", got)
	}
}
