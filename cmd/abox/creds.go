package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/credentials"
	"github.com/AdminTurnedDevOps/ABox/internal/credsource"
)

var (
	migrationKeychainAvailable = credsource.KeychainAvailable
	migrationSetKeychain       = credsource.SetKeychain
)

func runCreds(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: abox creds migrate  (move credentials.env entries to the macOS keychain)")
	}
	switch args[0] {
	case "migrate":
		return credsMigrate()
	default:
		return fmt.Errorf("unknown creds command %q (try: abox creds migrate)", args[0])
	}
}

func credsMigrate() error {
	if !migrationKeychainAvailable() {
		return fmt.Errorf("macOS keychain unavailable (this command needs /usr/bin/security on darwin); the env credential source keeps working")
	}
	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
	creds, err := credentials.Load()
	if err != nil {
		return err
	}
	if len(creds) == 0 {
		fmt.Println("no credentials to migrate")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	remaining := make(map[string]string, len(creds))
	for name, value := range creds {
		remaining[name] = value
	}
	var migrated, failed, dropped int
	configChanged := false
	legacyRefresh := legacyMCPRefreshEntries(cfg)
	for _, name := range sortedCredNames(creds) {
		if _, drop := legacyRefresh[name]; drop {
			fmt.Printf("dropping %s (refresh tokens are no longer stored; re-login when the access token expires)\n", name)
			delete(remaining, name)
			dropped++
			continue
		}
		if err := migrationSetKeychain(ctx, name, []byte(creds[name])); err != nil {
			fmt.Printf("skipped %s: %v (entry stays in the file; set it manually or re-login)\n", name, err)
			failed++
			continue
		}
		delete(remaining, name)
		configChanged = upsertCredentialRefs(&cfg, name) || configChanged
		migrated++
	}
	if configChanged {
		if err := cfg.Save(); err != nil {
			return fmt.Errorf("keychain writes succeeded but config update failed: %w", err)
		}
	}
	if err := rewriteCredentialFile(remaining); err != nil {
		return err
	}
	if failed > 0 {
		fmt.Printf("migrated %d, dropped %d refresh token(s), skipped %d (only skipped entries remain; fix them and run abox creds migrate again)\n", migrated, dropped, failed)
		return nil
	}
	fmt.Printf("migrated %d credential(s) to the macOS keychain (service %s), dropped %d refresh token(s)\n",
		migrated, credsource.KeychainService, dropped)
	fmt.Printf("rewrote %s (kept, mode 0600)\n", credentials.Path())
	return nil
}

func legacyMCPRefreshEntries(cfg config.File) map[string]struct{} {
	drops := make(map[string]struct{}, len(cfg.MCPServers))
	protected := make(map[string]struct{}, len(cfg.Models)+len(cfg.MCPServers))
	for _, model := range cfg.Models {
		protected[model.CredentialReference().Name] = struct{}{}
	}
	for _, server := range cfg.MCPServers {
		ref := server.CredentialReference()
		protected[ref.Name] = struct{}{}
		drops[config.TokenEnv(server)+"_REFRESH"] = struct{}{}
		if ref.Source == "env" {
			drops[ref.Name+"_REFRESH"] = struct{}{}
		}
	}
	for name := range protected {
		delete(drops, name)
	}
	return drops
}

func upsertCredentialRefs(cfg *config.File, name string) bool {
	changed := false
	for i := range cfg.Models {
		ref := cfg.Models[i].CredentialReference()
		if ref.Source == "env" && ref.Name == name {
			cfg.Models[i].CredentialEnv = ""
			cfg.Models[i].Credential = &config.CredentialRef{Source: "keychain", Name: name}
			changed = true
		}
	}
	for i := range cfg.MCPServers {
		ref := cfg.MCPServers[i].CredentialReference()
		if ref.Source == "env" && ref.Name == name {
			cfg.MCPServers[i].CredentialEnv = ""
			cfg.MCPServers[i].Credential = &config.CredentialRef{Source: "keychain", Name: name}
			changed = true
		}
	}
	return changed
}

func rewriteCredentialFile(creds map[string]string) error {
	var body strings.Builder
	body.WriteString("# ABox credentials. Mode 0600. Do not commit.\n")
	if len(creds) == 0 {
		body.WriteString("# Credentials migrated to the macOS keychain; env fallback remains supported.\n")
	} else {
		for _, name := range sortedCredNames(creds) {
			body.WriteString(name)
			body.WriteByte('=')
			body.WriteString(creds[name])
			body.WriteByte('\n')
		}
	}
	path := credentials.Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("rewrite credentials: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body.String()), 0o600); err != nil {
		return fmt.Errorf("rewrite credentials: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rewrite credentials: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("rewrite credentials: %w", err)
	}
	return nil
}

func sortedCredNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
