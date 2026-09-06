package mcpauth

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/credentials"
	"github.com/AdminTurnedDevOps/ABox/internal/credsource"
)

func allowLocalOAuthURLs(t *testing.T) {
	t.Helper()
	orig := oauthURLValidator
	oauthURLValidator = func(_ context.Context, raw string) (*url.URL, error) {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, err
		}
		if u.Host == "" {
			return nil, url.InvalidHostError(raw)
		}
		return u, nil
	}
	t.Cleanup(func() { oauthURLValidator = orig })
}

func TestLoginUnauthenticated(t *testing.T) {
	allowLocalOAuthURLs(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer ts.Close()
	res, err := Login(context.Background(), config.MCPServer{Name: "open", URL: ts.URL}, Options{HTTPClient: ts.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if res.AccessToken != "" {
		t.Fatalf("expected no token, got %q", res.AccessToken)
	}
}

func TestLoginOAuthCodeExchange(t *testing.T) {
	allowLocalOAuthURLs(t)
	var authorizeURL string
	mux := http.NewServeMux()
	as := httptest.NewServer(nil)
	mcp := httptest.NewServer(nil)

	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                           as.URL,
			"authorization_endpoint":           as.URL + "/authorize",
			"token_endpoint":                   as.URL + "/token",
			"registration_endpoint":            as.URL + "/register",
			"code_challenge_methods_supported": []string{"S256"},
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "redirect_uris") {
			http.Error(w, "bad dcr", 400)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"client_id":                  "test-client",
			"token_endpoint_auth_method": "none",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("code") != "good-code" || r.Form.Get("code_verifier") == "" {
			http.Error(w, "bad token", 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-xyz",
			"refresh_token": "refresh-xyz",
			"token_type":    "Bearer",
		})
	})
	as.Config.Handler = mux

	mcpMux := http.NewServeMux()
	mcpMux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              mcp.URL,
			"authorization_servers": []string{as.URL},
			"scopes_supported":      []string{"mcp"},
		})
	})
	mcpMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+mcp.URL+`/.well-known/oauth-protected-resource"`)
		http.Error(w, "auth", http.StatusUnauthorized)
	})
	mcp.Config.Handler = mcpMux

	res, err := Login(context.Background(), config.MCPServer{Name: "svc", URL: mcp.URL}, Options{
		HTTPClient: http.DefaultClient,
		OpenURL: func(raw string) error {
			authorizeURL = raw
			u, err := url.Parse(raw)
			if err != nil {
				return err
			}
			redir := u.Query().Get("redirect_uri")
			state := u.Query().Get("state")
			go func() {
				cb := redir + "?code=good-code&state=" + url.QueryEscape(state)
				_, _ = http.Get(cb)
			}()
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.AccessToken != "access-xyz" || res.RefreshToken != "refresh-xyz" {
		t.Fatalf("got %#v authorize=%s", res, authorizeURL)
	}
}

func TestValidatePublicOAuthURLRejectsPrivateTargets(t *testing.T) {
	for _, raw := range []string{
		"http://public.example/oauth",
		"https://localhost/oauth",
		"https://127.0.0.1/oauth",
		"https://10.0.0.1/oauth",
		"https://169.254.169.254/oauth",
		"https://[::1]/oauth",
		"https://[fe80::1]/oauth",
	} {
		if _, err := validatePublicOAuthURL(context.Background(), raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestValidatePublicOAuthURLRejectsDNSResolvingPrivate(t *testing.T) {
	orig := lookupOAuthHost
	lookupOAuthHost = func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host != "public.example" {
			t.Fatalf("host %q", host)
		}
		return []net.IPAddr{{IP: net.ParseIP("192.168.1.10")}}, nil
	}
	t.Cleanup(func() { lookupOAuthHost = orig })
	if _, err := validatePublicOAuthURL(context.Background(), "https://public.example/oauth"); err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("got %v", err)
	}
}

func TestLoginRejectsCrossOriginProtectedResourceMetadata(t *testing.T) {
	allowLocalOAuthURLs(t)
	metadataRequests := 0
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metadataRequests++
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer attacker.Close()
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+attacker.URL+`/metadata"`)
		http.Error(w, "auth", http.StatusUnauthorized)
	}))
	defer mcp.Close()

	_, err := Login(context.Background(), config.MCPServer{Name: "svc", URL: mcp.URL}, Options{HTTPClient: http.DefaultClient})
	if err == nil || !strings.Contains(err.Error(), "configured MCP origin") {
		t.Fatalf("got %v", err)
	}
	if metadataRequests != 0 {
		t.Fatal("cross-origin metadata endpoint was requested")
	}
}

func TestProtectedResourceMetadataRejectsResourceMismatch(t *testing.T) {
	allowLocalOAuthURLs(t)
	mcpURL, _ := url.Parse("http://mcp.example:8443/mcp")
	_, err := validateProtectedResourceMetadata(context.Background(), mcpURL, prmDoc{
		Resource:             "http://attacker.example/resource",
		AuthorizationServers: []string{"http://auth.example"},
	})
	if err == nil || !strings.Contains(err.Error(), "configured MCP origin") {
		t.Fatalf("got %v", err)
	}
}

func TestAuthorizationMetadataRejectsAttackerTokenEndpoint(t *testing.T) {
	allowLocalOAuthURLs(t)
	issuer, _ := url.Parse("http://auth.example:8443")
	err := validateAuthorizationServerMetadata(context.Background(), issuer, asDoc{
		Issuer:                issuer.String(),
		AuthorizationEndpoint: issuer.String() + "/authorize",
		TokenEndpoint:         "http://attacker.example/token",
	})
	if err == nil || !strings.Contains(err.Error(), "authorization server origin") {
		t.Fatalf("got %v", err)
	}
}

func TestTokenExchangeDoesNotFollowRedirect(t *testing.T) {
	attackerRequests := 0
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attackerRequests++
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "attacker-token"})
	}))
	defer attacker.Close()
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, attacker.URL+"/token", http.StatusTemporaryRedirect)
	}))
	defer tokenServer.Close()

	_, err := exchangeCode(context.Background(), withoutRedirects(http.DefaultClient), tokenServer.URL, "client", "http://127.0.0.1/callback", "code", "verifier", "https://mcp.example")
	if err == nil {
		t.Fatal("expected redirect response to fail")
	}
	if attackerRequests != 0 {
		t.Fatal("token request followed attacker redirect")
	}
}

func TestLoginNamedPersistsNoRefreshToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ABOX_HOME", "")
	// Never touch the real macOS keychain from tests: force the file-store
	// fallback path of the keychain-preferred writer.
	origKC := credsource.KeychainEnabled
	credsource.KeychainEnabled = func() bool { return false }
	t.Cleanup(func() { credsource.KeychainEnabled = origKC })

	cfg := config.Defaults()
	cfg.MCPServers = []config.MCPServer{{
		Name: "gh", URL: "https://api.githubcopilot.com/mcp/",
		CredentialEnv: "ABOX_MCP_GH_PAT",
	}}
	t.Setenv("ABOX_MCP_GH_PAT", "pat-value")
	if err := LoginNamed(context.Background(), cfg, "gh"); err != nil {
		t.Fatal(err)
	}
	creds, err := credentials.Load()
	if err != nil {
		t.Fatal(err)
	}
	if creds["ABOX_MCP_GH_PAT"] != "pat-value" {
		t.Fatalf("token not saved: %#v", creds)
	}
	for name := range creds {
		if strings.HasSuffix(name, "_REFRESH") {
			t.Fatalf("refresh token persisted: %s", name)
		}
	}
	savedCfg, _, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ref := savedCfg.MCPServers[0].CredentialReference()
	if ref.Source != "env" || ref.Name != "ABOX_MCP_GH_PAT" {
		t.Fatalf("credential reference %#v", ref)
	}
}

func TestLoginNamedPersistsKeychainReference(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ABOX_HOME", "")
	t.Setenv("CUSTOM_MCP_TOKEN", "pat-value")
	origSave := savePreferred
	savePreferred = func(_ context.Context, name, value string) (credsource.SaveResult, error) {
		if name != "CUSTOM_MCP_TOKEN" || value != "pat-value" {
			t.Fatalf("save %q=%q", name, value)
		}
		return credsource.SaveResult{Source: "keychain", Keychain: true, Note: "keychain"}, nil
	}
	t.Cleanup(func() { savePreferred = origSave })

	cfg := config.Defaults()
	cfg.MCPServers = []config.MCPServer{{
		Name:          "gh",
		URL:           "https://api.githubcopilot.com/mcp/",
		CredentialEnv: "CUSTOM_MCP_TOKEN",
	}}
	if err := LoginNamed(context.Background(), cfg, "gh"); err != nil {
		t.Fatal(err)
	}
	savedCfg, _, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	server := savedCfg.MCPServers[0]
	if server.CredentialEnv != "" || server.Credential == nil || *server.Credential != (config.CredentialRef{Source: "keychain", Name: "CUSTOM_MCP_TOKEN"}) {
		t.Fatalf("saved server %#v", server)
	}
}
