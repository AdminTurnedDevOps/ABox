// Package mcpauth runs host-side MCP OAuth (PKCE). Guest never imports this.
package mcpauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/credentials"
)

type Result struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
}

type Options struct {
	HTTPClient *http.Client
	OpenURL    func(string) error
}

type prmDoc struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

type asDoc struct {
	Issuer                        string   `json:"issuer"`
	AuthorizationEndpoint         string   `json:"authorization_endpoint"`
	TokenEndpoint                 string   `json:"token_endpoint"`
	RegistrationEndpoint          string   `json:"registration_endpoint"`
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`
}

type tokenResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	Error        string `json:"error"`
}

func LoginNamed(ctx context.Context, cfg config.File, name string) error {
	srv, err := serverNamed(cfg, name)
	if err != nil {
		return err
	}
	env := config.TokenEnv(srv)
	if srv.CredentialEnv != "" {
		val := strings.TrimSpace(os.Getenv(srv.CredentialEnv))
		if val == "" {
			return fmt.Errorf("set %s or omit credential_env to use OAuth", srv.CredentialEnv)
		}
		credentials.SetEnv(env, val)
		return credentials.Save(env, val)
	}
	res, err := Login(ctx, srv, Options{})
	if err != nil {
		return err
	}
	if res.AccessToken == "" {
		return nil
	}
	if err := credentials.Save(env, res.AccessToken); err != nil {
		return err
	}
	credentials.SetEnv(env, res.AccessToken)
	if res.RefreshToken != "" {
		_ = credentials.Save(env+"_REFRESH", res.RefreshToken)
	}
	return nil
}

func serverNamed(cfg config.File, name string) (config.MCPServer, error) {
	for _, s := range cfg.MCPServers {
		if s.Name == name {
			return s, nil
		}
	}
	return config.MCPServer{}, fmt.Errorf("unknown mcp server %q", name)
}

func Login(ctx context.Context, srv config.MCPServer, opts Options) (Result, error) {
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	status, hdr, err := probeMCP(ctx, client, srv.URL)
	if err != nil {
		return Result{}, err
	}
	if status >= 200 && status < 300 {
		return Result{}, nil
	}
	if status != http.StatusUnauthorized {
		return Result{}, fmt.Errorf("mcp %s: unexpected status %d (want 200 or 401)", srv.URL, status)
	}
	metaURL := resourceMetadataURL(hdr)
	if metaURL == "" {
		metaURL, err = discoverPRM(ctx, client, srv.URL)
		if err != nil {
			return Result{}, err
		}
	}
	prm, err := fetchPRM(ctx, client, metaURL)
	if err != nil {
		return Result{}, err
	}
	if len(prm.AuthorizationServers) == 0 {
		return Result{}, fmt.Errorf("protected resource metadata has no authorization_servers")
	}
	as, err := fetchAS(ctx, client, prm.AuthorizationServers[0])
	if err != nil {
		return Result{}, err
	}
	if !supportsS256(as.CodeChallengeMethodsSupported) {
		return Result{}, fmt.Errorf("authorization server does not advertise S256 PKCE")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Result{}, err
	}
	defer ln.Close()
	redir := "http://" + ln.Addr().String() + "/callback"
	clientID := srv.ClientID
	if clientID == "" {
		if as.RegistrationEndpoint == "" {
			return Result{}, fmt.Errorf("no client_id configured and authorization server has no registration_endpoint")
		}
		clientID, err = registerClient(ctx, client, as.RegistrationEndpoint, redir)
		if err != nil {
			return Result{}, err
		}
	}
	verifier, challenge, err := pkce()
	if err != nil {
		return Result{}, err
	}
	state, err := randomHex(16)
	if err != nil {
		return Result{}, err
	}
	scope := strings.Join(srv.Scopes, " ")
	if scope == "" {
		scope = strings.Join(prm.ScopesSupported, " ")
	}
	authURL := authorizeURL(as.AuthorizationEndpoint, clientID, redir, challenge, state, resourceParam(srv.URL, prm.Resource), scope)
	open := opts.OpenURL
	if open == nil {
		open = openBrowser
	}
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go serveCallback(ln, state, codeCh, errCh)
	if err := open(authURL); err != nil {
		return Result{}, fmt.Errorf("open browser: %w", err)
	}
	var code string
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case err := <-errCh:
		return Result{}, err
	case code = <-codeCh:
	case <-time.After(5 * time.Minute):
		return Result{}, fmt.Errorf("oauth timed out waiting for browser callback")
	}
	return exchangeCode(ctx, client, as.TokenEndpoint, clientID, redir, code, verifier, resourceParam(srv.URL, prm.Resource))
}

func probeMCP(ctx context.Context, client *http.Client, rawURL string) (int, http.Header, error) {
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"abox","version":"dev"}}}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, resp.Header.Clone(), nil
}

func resourceMetadataURL(h http.Header) string {
	for _, v := range h.Values("WWW-Authenticate") {
		lower := strings.ToLower(v)
		key := "resource_metadata="
		i := strings.Index(lower, key)
		if i < 0 {
			continue
		}
		rest := strings.TrimSpace(v[i+len(key):])
		rest = strings.Trim(rest, `"`)
		if comma := strings.Index(rest, ","); comma >= 0 {
			rest = rest[:comma]
		}
		rest = strings.Trim(rest, `"`)
		return strings.TrimSpace(rest)
	}
	return ""
}

func discoverPRM(ctx context.Context, client *http.Client, mcpURL string) (string, error) {
	u, err := url.Parse(mcpURL)
	if err != nil {
		return "", err
	}
	path := strings.TrimSuffix(u.Path, "/")
	candidates := []string{
		u.Scheme + "://" + u.Host + "/.well-known/oauth-protected-resource" + path,
		u.Scheme + "://" + u.Host + "/.well-known/oauth-protected-resource",
	}
	var last error
	for _, c := range candidates {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c, nil)
		if err != nil {
			last = err
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			last = err
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == 200 {
			return c, nil
		}
		last = fmt.Errorf("%s: %s", c, resp.Status)
	}
	if last == nil {
		last = fmt.Errorf("no protected resource metadata")
	}
	return "", last
}

func fetchPRM(ctx context.Context, client *http.Client, raw string) (prmDoc, error) {
	var doc prmDoc
	if err := getJSON(ctx, client, raw, &doc); err != nil {
		return doc, err
	}
	return doc, nil
}

func fetchAS(ctx context.Context, client *http.Client, issuer string) (asDoc, error) {
	issuer = strings.TrimRight(issuer, "/")
	u, err := url.Parse(issuer)
	if err != nil {
		return asDoc{}, err
	}
	var candidates []string
	if u.Path != "" && u.Path != "/" {
		path := strings.TrimPrefix(u.Path, "/")
		candidates = []string{
			u.Scheme + "://" + u.Host + "/.well-known/oauth-authorization-server/" + path,
			u.Scheme + "://" + u.Host + "/.well-known/openid-configuration/" + path,
			issuer + "/.well-known/openid-configuration",
		}
	} else {
		candidates = []string{
			issuer + "/.well-known/oauth-authorization-server",
			issuer + "/.well-known/openid-configuration",
		}
	}
	var last error
	for _, c := range candidates {
		var doc asDoc
		if err := getJSON(ctx, client, c, &doc); err != nil {
			last = err
			continue
		}
		if doc.AuthorizationEndpoint != "" && doc.TokenEndpoint != "" {
			return doc, nil
		}
		last = fmt.Errorf("%s: missing endpoints", c)
	}
	if last == nil {
		last = fmt.Errorf("authorization server metadata not found")
	}
	return asDoc{}, last
}

func supportsS256(methods []string) bool {
	for _, m := range methods {
		if strings.EqualFold(m, "S256") {
			return true
		}
	}
	return false
}

func registerClient(ctx context.Context, client *http.Client, endpoint, redirect string) (string, error) {
	payload, _ := json.Marshal(map[string]any{
		"client_name":                "ABox",
		"redirect_uris":              []string{redirect},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("dcr %s: %s", resp.Status, b)
	}
	var out struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(b, &out); err != nil || out.ClientID == "" {
		return "", fmt.Errorf("dcr: missing client_id")
	}
	return out.ClientID, nil
}

func authorizeURL(endpoint, clientID, redirect, challenge, state, resource, scope string) string {
	u, _ := url.Parse(endpoint)
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirect)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	if resource != "" {
		q.Set("resource", resource)
	}
	if scope != "" {
		q.Set("scope", scope)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func serveCallback(ln net.Listener, state string, codeCh chan<- string, errCh chan<- error) {
	http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("error") != "" {
			http.Error(w, "ABox login failed: "+q.Get("error"), http.StatusBadRequest)
			errCh <- fmt.Errorf("oauth error %s", q.Get("error"))
			return
		}
		if q.Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			errCh <- fmt.Errorf("oauth state mismatch")
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			errCh <- fmt.Errorf("oauth missing code")
			return
		}
		_, _ = io.WriteString(w, "ABox login complete. You can close this window.")
		codeCh <- code
	}))
}

func exchangeCode(ctx context.Context, client *http.Client, tokenURL, clientID, redirect, code, verifier, resource string) (Result, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirect)
	form.Set("client_id", clientID)
	form.Set("code_verifier", verifier)
	if resource != "" {
		form.Set("resource", resource)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tr tokenResp
	if err := json.Unmarshal(b, &tr); err != nil {
		return Result{}, fmt.Errorf("token json: %w", err)
	}
	if resp.StatusCode >= 300 || tr.AccessToken == "" {
		return Result{}, fmt.Errorf("token %s: %s", resp.Status, b)
	}
	return Result{AccessToken: tr.AccessToken, RefreshToken: tr.RefreshToken, TokenType: tr.TokenType}, nil
}

func getJSON(ctx context.Context, client *http.Client, raw string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, b)
	}
	return json.Unmarshal(b, dest)
}

func pkce() (verifier, challenge string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func resourceParam(mcpURL, prmResource string) string {
	if prmResource != "" {
		return strings.TrimRight(prmResource, "/")
	}
	u, err := url.Parse(mcpURL)
	if err != nil {
		return mcpURL
	}
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/")
}

func openBrowser(raw string) error {
	return exec.Command("open", raw).Start()
}
