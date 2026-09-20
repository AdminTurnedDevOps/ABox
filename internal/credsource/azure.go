package credsource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
)

type azureSource struct{}

const (
	azureAPIVersion   = "7.5"
	azureTokenTimeout = 15 * time.Second
)

var runAz = func(ctx context.Context, args []string) (stdout string, err error) {
	path, err := trustedAzureCLI()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = azureCLIEnvironment()
	out, err := cmd.Output()
	return string(out), err
}

var newAzureClient = func() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

var azAvailable = func() bool {
	_, err := trustedAzureCLI()
	return err == nil
}

func trustedAzureCLI() (string, error) {
	candidates := []string{"/usr/bin/az", "/usr/local/bin/az"}
	if runtime.GOOS == "darwin" {
		candidates = []string{"/opt/homebrew/bin/az", "/usr/local/bin/az"}
	}
	for _, candidate := range candidates {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		info, err := os.Stat(resolved)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("Azure CLI not found in a trusted system location")
}

func azureCLIEnvironment() []string {
	env := []string{"PATH=/usr/bin:/bin"}
	for _, name := range []string{
		"HOME", "AZURE_CONFIG_DIR", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
		"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "LANG", "LC_ALL",
	} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}

func (azureSource) Resolve(ctx context.Context, ref Reference) (Value, error) {
	secretURI, name, version, cloud, err := config.ParseAzureSecretReference(ref.Name, ref.Version)
	if err != nil {
		return Value{}, err
	}
	token, err := azureToken(ctx, cloud)
	if err != nil {
		return Value{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, secretURI+"?api-version="+azureAPIVersion, nil)
	if err != nil {
		return Value{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := newAzureClient().Do(req)
	if err != nil {
		return Value{}, fmt.Errorf("azure key vault request for %s: %w", name, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Value{}, fmt.Errorf("%w: azure key vault %s", ErrNotFound, name)
	case resp.StatusCode == http.StatusForbidden:
		return Value{}, fmt.Errorf("azure key vault %s: permission denied (check the key vault access policy or RBAC role)", name)
	case resp.StatusCode >= 300:
		return Value{}, fmt.Errorf("azure key vault %s: %s", name, resp.Status)
	}
	var parsed struct {
		Value string `json:"value"`
		ID    string `json:"id"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Value{}, fmt.Errorf("azure key vault %s: malformed response", name)
	}
	if parsed.Value == "" {
		return Value{}, fmt.Errorf("%w: azure key vault %s is empty", ErrNotFound, name)
	}
	if version == "" {
		if parts := strings.Split(parsed.ID, "/"); len(parts) > 0 {
			version = parts[len(parts)-1]
		}
	}
	return Value{Bytes: []byte(parsed.Value), Version: version}, nil
}

func (azureSource) Close() error { return nil }

func azureToken(ctx context.Context, cloud config.AzureCloud) (string, error) {
	clientID := strings.TrimSpace(os.Getenv("AZURE_CLIENT_ID"))
	tenantID := strings.TrimSpace(os.Getenv("AZURE_TENANT_ID"))
	clientSecret := os.Getenv("AZURE_CLIENT_SECRET")
	if clientID != "" && tenantID != "" && clientSecret != "" {
		authority := cloud.AuthorityHost
		if configured := strings.TrimRight(strings.TrimSpace(os.Getenv("AZURE_AUTHORITY_HOST")), "/"); configured != "" && !strings.EqualFold(configured, authority) {
			return "", fmt.Errorf("AZURE_AUTHORITY_HOST %q does not match Key Vault cloud authority %q", configured, authority)
		}
		tokenCtx, cancel := context.WithTimeout(ctx, azureTokenTimeout)
		defer cancel()
		form := strings.NewReader(strings.Join([]string{
			"grant_type=client_credentials",
			"client_id=" + url.QueryEscape(clientID),
			"client_secret=" + url.QueryEscape(clientSecret),
			"scope=" + url.QueryEscape(cloud.VaultResource+"/.default"),
		}, "&"))
		req, err := http.NewRequestWithContext(tokenCtx, http.MethodPost,
			authority+"/"+tenantID+"/oauth2/v2.0/token", form)
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := newAzureClient().Do(req)
		if err != nil {
			return "", fmt.Errorf("azure token request: %w", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode >= 300 {
			return "", fmt.Errorf("azure token request: %s (check AZURE_CLIENT_ID/AZURE_TENANT_ID/AZURE_CLIENT_SECRET)", resp.Status)
		}
		var parsed struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil || parsed.AccessToken == "" {
			return "", fmt.Errorf("azure token response: malformed")
		}
		return parsed.AccessToken, nil
	}
	if !azAvailable() {
		return "", fmt.Errorf("%w: azure source needs AZURE_CLIENT_ID+AZURE_TENANT_ID+AZURE_CLIENT_SECRET or the Azure CLI (az login)", ErrLocked)
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := runAz(cmdCtx, []string{"account", "get-access-token", "--resource", cloud.VaultResource, "--output", "json"})
	if err != nil {
		return "", fmt.Errorf("az account get-access-token failed (run: az login): %w", err)
	}
	var parsed struct {
		AccessToken string `json:"accessToken"`
		Token       string `json:"token"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return "", fmt.Errorf("az token output: malformed")
	}
	if parsed.AccessToken != "" {
		return parsed.AccessToken, nil
	}
	if parsed.Token != "" {
		return parsed.Token, nil
	}
	return "", fmt.Errorf("az token output: no access token")
}
