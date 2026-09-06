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
	"github.com/AdminTurnedDevOps/ABox/internal/credsource"
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

var savePreferred = credsource.SavePreferred

var (
	oauthURLValidator = validatePublicOAuthURL
	lookupOAuthHost   = net.DefaultResolver.LookupIPAddr
	blockedOAuthNets  = []*net.IPNet{
		mustCIDR("0.0.0.0/8"),
		mustCIDR("100.64.0.0/10"),
		mustCIDR("192.0.0.0/24"),
		mustCIDR("192.0.2.0/24"),
		mustCIDR("198.18.0.0/15"),
		mustCIDR("198.51.100.0/24"),
		mustCIDR("203.0.113.0/24"),
		mustCIDR("240.0.0.0/4"),
		mustCIDR("2001:db8::/32"),
	}
)

func mustCIDR(raw string) *net.IPNet {
	_, network, err := net.ParseCIDR(raw)
	if err != nil {
		panic(err)
	}
	return network
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
		res, err := savePreferred(ctx, env, val)
		if err != nil {
			return err
		}
		if err := persistCredentialReference(cfg, srv.Name, env, res.Source); err != nil {
			return err
		}
		fmt.Printf("mcp %s token saved (%s)\n", srv.Name, res.Note)
		return nil
	}
	res, err := Login(ctx, srv, Options{})
	if err != nil {
		return err
	}
	if res.AccessToken == "" {
		return nil
	}
	saved, err := savePreferred(ctx, env, res.AccessToken)
	if err != nil {
		return err
	}
	if err := persistCredentialReference(cfg, srv.Name, env, saved.Source); err != nil {
		return err
	}
	fmt.Printf("mcp %s token saved (%s)\n", srv.Name, saved.Note)
	return nil
}

func persistCredentialReference(cfg config.File, serverName, credentialName, source string) error {
	if source != "env" && source != "keychain" {
		return fmt.Errorf("mcp %s token saved to unknown credential source %q", serverName, source)
	}
	for i := range cfg.MCPServers {
		if cfg.MCPServers[i].Name != serverName {
			continue
		}
		cfg.MCPServers[i].CredentialEnv = ""
		cfg.MCPServers[i].Credential = &config.CredentialRef{Source: source, Name: credentialName}
		if err := cfg.Save(); err != nil {
			return fmt.Errorf("save mcp %s credential reference: %w", serverName, err)
		}
		return nil
	}
	return fmt.Errorf("unknown mcp server %q", serverName)
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
	client = withoutRedirects(client)
	mcpURL, err := oauthURLValidator(ctx, srv.URL)
	if err != nil {
		return Result{}, fmt.Errorf("mcp URL: %w", err)
	}
	status, hdr, err := probeMCP(ctx, client, mcpURL.String())
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
		metaURL, err = discoverPRM(ctx, client, mcpURL.String())
		if err != nil {
			return Result{}, err
		}
	}
	metadataURL, err := oauthURLValidator(ctx, metaURL)
	if err != nil {
		return Result{}, fmt.Errorf("protected resource metadata URL: %w", err)
	}
	if !sameOrigin(mcpURL, metadataURL) {
		return Result{}, fmt.Errorf("protected resource metadata URL must use the configured MCP origin %s", oauthOrigin(mcpURL))
	}
	prm, err := fetchPRM(ctx, client, metadataURL.String())
	if err != nil {
		return Result{}, err
	}
	resource, err := validateProtectedResourceMetadata(ctx, mcpURL, prm)
	if err != nil {
		return Result{}, err
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
	authURL := authorizeURL(as.AuthorizationEndpoint, clientID, redir, challenge, state, resource, scope)
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
	return exchangeCode(ctx, client, as.TokenEndpoint, clientID, redir, code, verifier, resource)
}

func withoutRedirects(client *http.Client) *http.Client {
	clone := *client
	clone.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &clone
}

func validatePublicOAuthURL(ctx context.Context, raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, fmt.Errorf("must be an https URL without userinfo or fragment")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.HasSuffix(host, ".") {
		return nil, fmt.Errorf("has an invalid host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if !publicOAuthIP(ip) {
			return nil, fmt.Errorf("host %q is not a public address", host)
		}
		return u, nil
	}
	if !strings.Contains(host, ".") || host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".home.arpa") {
		return nil, fmt.Errorf("host %q is not a public DNS name", host)
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	addrs, err := lookupOAuthHost(lookupCtx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve host %q: %w", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("host %q has no addresses", host)
	}
	for _, addr := range addrs {
		if !publicOAuthIP(addr.IP) {
			return nil, fmt.Errorf("host %q resolves to non-public address %s", host, addr.IP)
		}
	}
	return u, nil
}

func publicOAuthIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, network := range blockedOAuthNets {
		if network.Contains(ip) {
			return false
		}
	}
	return true
}

func oauthOrigin(u *url.URL) string {
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	defaultPort := "443"
	if scheme == "http" {
		defaultPort = "80"
	}
	if port != "" && port != defaultPort {
		return scheme + "://" + net.JoinHostPort(host, port)
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && effectivePort(a) == effectivePort(b)
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}

func validateProtectedResourceMetadata(ctx context.Context, mcpURL *url.URL, prm prmDoc) (string, error) {
	if strings.TrimSpace(prm.Resource) == "" {
		return "", fmt.Errorf("protected resource metadata has no resource identifier")
	}
	resourceURL, err := oauthURLValidator(ctx, prm.Resource)
	if err != nil {
		return "", fmt.Errorf("protected resource metadata resource: %w", err)
	}
	if !sameOrigin(mcpURL, resourceURL) {
		return "", fmt.Errorf("protected resource metadata resource must use the configured MCP origin %s", oauthOrigin(mcpURL))
	}
	if len(prm.AuthorizationServers) == 0 {
		return "", fmt.Errorf("protected resource metadata has no authorization_servers")
	}
	for _, raw := range prm.AuthorizationServers {
		issuer, err := oauthURLValidator(ctx, raw)
		if err != nil {
			return "", fmt.Errorf("authorization server %q: %w", raw, err)
		}
		if issuer.RawQuery != "" {
			return "", fmt.Errorf("authorization server issuer must not contain a query")
		}
	}
	return strings.TrimRight(resourceURL.String(), "/"), nil
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
	u, err := oauthURLValidator(ctx, issuer)
	if err != nil {
		return asDoc{}, fmt.Errorf("authorization server issuer: %w", err)
	}
	if u.RawQuery != "" {
		return asDoc{}, fmt.Errorf("authorization server issuer must not contain a query")
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
		if err := validateAuthorizationServerMetadata(ctx, u, doc); err == nil {
			return doc, nil
		} else {
			last = fmt.Errorf("%s: %w", c, err)
			continue
		}
	}
	if last == nil {
		last = fmt.Errorf("authorization server metadata not found")
	}
	return asDoc{}, last
}

func validateAuthorizationServerMetadata(ctx context.Context, issuer *url.URL, doc asDoc) error {
	if strings.TrimRight(doc.Issuer, "/") != strings.TrimRight(issuer.String(), "/") {
		return fmt.Errorf("authorization server metadata issuer %q does not match %q", doc.Issuer, issuer.String())
	}
	for _, endpoint := range []struct {
		name     string
		raw      string
		required bool
	}{
		{name: "authorization_endpoint", raw: doc.AuthorizationEndpoint, required: true},
		{name: "token_endpoint", raw: doc.TokenEndpoint, required: true},
		{name: "registration_endpoint", raw: doc.RegistrationEndpoint},
	} {
		if endpoint.raw == "" {
			if endpoint.required {
				return fmt.Errorf("authorization server metadata has no %s", endpoint.name)
			}
			continue
		}
		u, err := oauthURLValidator(ctx, endpoint.raw)
		if err != nil {
			return fmt.Errorf("%s: %w", endpoint.name, err)
		}
		if !sameOrigin(issuer, u) {
			return fmt.Errorf("%s must use authorization server origin %s", endpoint.name, oauthOrigin(issuer))
		}
	}
	return nil
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
	if resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("token endpoint: %s", resp.Status)
	}
	var tr tokenResp
	if err := json.Unmarshal(b, &tr); err != nil {
		return Result{}, fmt.Errorf("token json: %w", err)
	}
	if tr.AccessToken == "" {
		return Result{}, fmt.Errorf("token endpoint response has no access token")
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

func openBrowser(raw string) error {
	return exec.Command("open", raw).Start()
}
