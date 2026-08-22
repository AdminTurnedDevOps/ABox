package mcpauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
)

func TestLoginUnauthenticated(t *testing.T) {
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
	var authorizeURL string
	mux := http.NewServeMux()
	as := httptest.NewServer(nil)
	mcp := httptest.NewServer(nil)

	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              mcp.URL,
			"authorization_servers": []string{as.URL},
			"scopes_supported":      []string{"mcp"},
		})
	})
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
	mcpMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+as.URL+`/.well-known/oauth-protected-resource"`)
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
