package credsource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/credentials"
)

func newVaultServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// --- Azure Key Vault ---

func newAzureTLSServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	orig := newAzureClient
	newAzureClient = func() *http.Client {
		client := srv.Client()
		transport := client.Transport
		client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			clone := req.Clone(req.Context())
			clone.URL.Scheme = target.Scheme
			clone.URL.Host = target.Host
			return transport.RoundTrip(clone)
		})
		return client
	}
	t.Cleanup(func() { newAzureClient = orig })
	return srv
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func fakeAzAvailable(t *testing.T) {
	t.Helper()
	orig := azAvailable
	azAvailable = func() bool { return true }
	t.Cleanup(func() { azAvailable = orig })
}

func TestAzureResolveServicePrincipal(t *testing.T) {
	var gotForm string
	var gotScope string
	var mu sync.Mutex
	newAzureTLSServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/secrets/anthropic" {
			if r.Header.Get("Authorization") != "Bearer sp-token" {
				t.Errorf("missing bearer")
			}
			if r.URL.Query().Get("api-version") != "7.5" {
				t.Errorf("api-version %q", r.URL.Query().Get("api-version"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"value": "azure-secret",
				"id":    "https://testkv.vault.azure.net/secrets/anthropic/abc123",
			})
			return
		}
		mu.Lock()
		defer mu.Unlock()
		gotForm = r.URL.Path
		_ = r.ParseForm()
		gotScope = r.Form.Get("scope")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "sp-token"})
	})
	t.Setenv("AZURE_CLIENT_ID", "client")
	t.Setenv("AZURE_TENANT_ID", "tenant")
	t.Setenv("AZURE_CLIENT_SECRET", "sp-secret")
	t.Setenv("AZURE_AUTHORITY_HOST", "")

	v, err := testResolver().Resolve(context.Background(), Reference{
		Source: "azure",
		Name:   "https://testkv.vault.azure.net/secrets/anthropic",
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	path := gotForm
	scope := gotScope
	mu.Unlock()
	if path != "/tenant/oauth2/v2.0/token" {
		t.Fatalf("token path %q", path)
	}
	if scope != "https://vault.azure.net/.default" {
		t.Fatalf("scope %q", scope)
	}
	if string(v.Bytes) != "azure-secret" || v.Version != "abc123" {
		t.Fatalf("got %q version %q", v.Bytes, v.Version)
	}
}

func TestAzureResolveAzCLIFallback(t *testing.T) {
	fakeAzAvailable(t)
	t.Setenv("AZURE_CLIENT_ID", "")
	t.Setenv("AZURE_TENANT_ID", "")
	t.Setenv("AZURE_CLIENT_SECRET", "")
	orig := runAz
	runAz = func(_ context.Context, args []string) (string, error) {
		if len(args) != 6 || args[0] != "account" || args[1] != "get-access-token" || args[2] != "--resource" || args[3] != "https://vault.azure.net" || args[4] != "--output" || args[5] != "json" {
			return "", fmt.Errorf("unexpected az args %v", args)
		}
		return `{"accessToken":"cli-token"}`, nil
	}
	t.Cleanup(func() { runAz = orig })

	newAzureTLSServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cli-token" {
			t.Errorf("missing cli bearer")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"value": "cli-secret",
			"id":    "https://testkv.vault.azure.net/secrets/anthropic/v2",
		})
	})
	v, err := testResolver().Resolve(context.Background(), Reference{
		Source:  "azure",
		Name:    "https://testkv.vault.azure.net/secrets/anthropic",
		Version: "v2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(v.Bytes) != "cli-secret" {
		t.Fatalf("got %q", v.Bytes)
	}
	if v.Version != "v2" {
		t.Fatalf("version %q", v.Version)
	}
}

func TestAzureNotFound(t *testing.T) {
	fakeAzAvailable(t)
	t.Setenv("AZURE_CLIENT_ID", "")
	t.Setenv("AZURE_TENANT_ID", "")
	t.Setenv("AZURE_CLIENT_SECRET", "")
	orig := runAz
	runAz = func(_ context.Context, _ []string) (string, error) {
		return `{"accessToken":"t"}`, nil
	}
	t.Cleanup(func() { runAz = orig })
	newAzureTLSServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	_, err := testResolver().Resolve(context.Background(), Reference{Source: "azure", Name: "https://testkv.vault.azure.net/secrets/anthropic"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestAzureForbidden(t *testing.T) {
	fakeAzAvailable(t)
	t.Setenv("AZURE_CLIENT_ID", "")
	t.Setenv("AZURE_TENANT_ID", "")
	t.Setenv("AZURE_CLIENT_SECRET", "")
	orig := runAz
	runAz = func(_ context.Context, _ []string) (string, error) {
		return `{"accessToken":"t"}`, nil
	}
	t.Cleanup(func() { runAz = orig })
	newAzureTLSServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	_, err := testResolver().Resolve(context.Background(), Reference{Source: "azure", Name: "https://testkv.vault.azure.net/secrets/anthropic"})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("got %v", err)
	}
}

func TestAzureRejectsNonURI(t *testing.T) {
	_, err := testResolver().Resolve(context.Background(), Reference{Source: "azure", Name: "not-a-uri"})
	if err == nil || !strings.Contains(err.Error(), "https secret URI") {
		t.Fatalf("got %v", err)
	}
}

func TestAzureRejectsAttackerHostBeforeFetchingToken(t *testing.T) {
	orig := runAz
	runAz = func(_ context.Context, _ []string) (string, error) {
		t.Fatal("attacker URI reached token acquisition")
		return "", nil
	}
	t.Cleanup(func() { runAz = orig })
	t.Setenv("AZURE_CLIENT_ID", "")
	t.Setenv("AZURE_TENANT_ID", "")
	t.Setenv("AZURE_CLIENT_SECRET", "")
	_, err := testResolver().Resolve(context.Background(), Reference{
		Source: "azure",
		Name:   "https://testkv.vault.azure.net.attacker.example/secrets/x",
	})
	if err == nil || !strings.Contains(err.Error(), "not a supported Azure Key Vault endpoint") {
		t.Fatalf("got %v", err)
	}
}

func TestAzureRejectsMismatchedAuthorityBeforeSendingClientSecret(t *testing.T) {
	t.Setenv("AZURE_CLIENT_ID", "client")
	t.Setenv("AZURE_TENANT_ID", "tenant")
	t.Setenv("AZURE_CLIENT_SECRET", "client-secret")
	t.Setenv("AZURE_AUTHORITY_HOST", "https://attacker.example")
	_, _, _, cloud, err := config.ParseAzureSecretReference("https://testkv.vault.azure.net/secrets/x", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := azureToken(context.Background(), cloud); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("got %v", err)
	}
}

func TestAzureSovereignCLIResources(t *testing.T) {
	fakeAzAvailable(t)
	t.Setenv("AZURE_CLIENT_ID", "")
	t.Setenv("AZURE_TENANT_ID", "")
	t.Setenv("AZURE_CLIENT_SECRET", "")
	for _, tc := range []struct {
		name      string
		host      string
		authority string
		resource  string
	}{
		{name: "usgov", host: "testkv.vault.usgovcloudapi.net", authority: "https://login.microsoftonline.us", resource: "https://vault.usgovcloudapi.net"},
		{name: "china", host: "testkv.vault.azure.cn", authority: "https://login.chinacloudapi.cn", resource: "https://vault.azure.cn"},
		{name: "germany", host: "testkv.vault.microsoftazure.de", authority: "https://login.microsoftonline.de", resource: "https://vault.microsoftazure.de"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, cloud, err := config.ParseAzureSecretReference("https://"+tc.host+"/secrets/x", "")
			if err != nil {
				t.Fatal(err)
			}
			if cloud.AuthorityHost != tc.authority {
				t.Fatalf("authority %q", cloud.AuthorityHost)
			}
			orig := runAz
			runAz = func(_ context.Context, args []string) (string, error) {
				if len(args) != 6 || args[3] != tc.resource || args[4] != "--output" || args[5] != "json" {
					t.Fatalf("unexpected az args %v", args)
				}
				return `{"accessToken":"cli-token"}`, nil
			}
			t.Cleanup(func() { runAz = orig })
			if _, err := azureToken(context.Background(), cloud); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAzureNoCredentials(t *testing.T) {
	t.Setenv("AZURE_CLIENT_ID", "")
	t.Setenv("AZURE_TENANT_ID", "")
	t.Setenv("AZURE_CLIENT_SECRET", "")
	origAvail := azAvailable
	azAvailable = func() bool { return false }
	t.Cleanup(func() { azAvailable = origAvail })
	_, err := testResolver().Resolve(context.Background(), Reference{Source: "azure", Name: "https://testkv.vault.azure.net/secrets/x"})
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("got %v", err)
	}
}

// --- AWS Secrets Manager ---

func newAWSServer(t *testing.T, status int, body string, check func(r *http.Request, sawAuth string)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if r.Header.Get("X-Amz-Target") != "secretsmanager.GetSecretValue" {
			t.Errorf("target %q", r.Header.Get("X-Amz-Target"))
		}
		if r.Header.Get("Content-Type") != "application/x-amz-json-1.1" {
			t.Errorf("content type %q", r.Header.Get("Content-Type"))
		}
		if check != nil {
			check(r, auth)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("AWS_ENDPOINT_URL", srv.URL)
	return srv
}

func TestAWSResolveSigV4(t *testing.T) {
	var body map[string]string
	var mu sync.Mutex
	newAWSServer(t, 200, `{"SecretString":"aws-secret"}`, func(r *http.Request, auth string) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=AKID-TEST/") {
			t.Errorf("authorization %q", auth)
		}
		if !strings.Contains(auth, "SignedHeaders=content-type;host;x-amz-date;x-amz-target") {
			t.Errorf("signed headers missing: %q", auth)
		}
		if strings.Contains(auth, "aws-secret-key") {
			t.Errorf("secret key leaked into Authorization")
		}
	})
	t.Setenv("AWS_ACCESS_KEY_ID", "AKID-TEST")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-secret-key")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_REGION", "us-east-1")

	v, err := testResolver().Resolve(context.Background(), Reference{Source: "aws", Name: "prod/anthropic"})
	if err != nil {
		t.Fatal(err)
	}
	if string(v.Bytes) != "aws-secret" {
		t.Fatalf("got %q", v.Bytes)
	}
	mu.Lock()
	defer mu.Unlock()
	if body["SecretId"] != "prod/anthropic" {
		t.Fatalf("body %v", body)
	}
}

func TestAWSResolveSessionTokenAndField(t *testing.T) {
	var sawAuth string
	var mu sync.Mutex
	newAWSServer(t, 200, `{"SecretString":"{\"api_key\":\"nested\",\"other\":\"x\"}"}`, func(r *http.Request, auth string) {
		mu.Lock()
		defer mu.Unlock()
		sawAuth = auth
	})
	t.Setenv("AWS_ACCESS_KEY_ID", "AKID-TEST")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-secret-key")
	t.Setenv("AWS_SESSION_TOKEN", "session-tok")
	t.Setenv("AWS_REGION", "eu-west-1")

	v, err := testResolver().Resolve(context.Background(), Reference{Source: "aws", Name: "multikey", Field: "api_key"})
	if err != nil {
		t.Fatal(err)
	}
	if string(v.Bytes) != "nested" {
		t.Fatalf("got %q", v.Bytes)
	}
	mu.Lock()
	auth := sawAuth
	mu.Unlock()
	if !strings.Contains(auth, "x-amz-security-token") {
		t.Fatalf("session token not signed: %q", auth)
	}
	if !strings.Contains(auth, "eu-west-1") {
		t.Fatalf("region not in scope: %q", auth)
	}
}

func TestAWSNumericFieldPreservesPrecision(t *testing.T) {
	const large = "9007199254740993123456789"
	newAWSServer(t, 200, `{"SecretString":"{\"large\":`+large+`}"}`, nil)
	t.Setenv("AWS_ACCESS_KEY_ID", "AKID-TEST")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-secret-key")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_REGION", "us-east-1")

	v, err := testResolver().Resolve(context.Background(), Reference{Source: "aws", Name: "numeric", Field: "large"})
	if err != nil {
		t.Fatal(err)
	}
	if string(v.Bytes) != large {
		t.Fatalf("got %q", v.Bytes)
	}
}

func TestAWSNotFound(t *testing.T) {
	newAWSServer(t, 400, `{"__type":"com.amazonaws.secretsmanager#ResourceNotFoundException"}`, nil)
	t.Setenv("AWS_ACCESS_KEY_ID", "AKID-TEST")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "k")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_REGION", "us-east-1")
	_, err := testResolver().Resolve(context.Background(), Reference{Source: "aws", Name: "missing"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestAWSAccessDenied(t *testing.T) {
	newAWSServer(t, 400, `{"__type":"AccessDeniedException","message":"no"}`, nil)
	t.Setenv("AWS_ACCESS_KEY_ID", "AKID-TEST")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "k")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_REGION", "us-east-1")
	_, err := testResolver().Resolve(context.Background(), Reference{Source: "aws", Name: "nope"})
	if err == nil || !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("got %v", err)
	}
}

func TestAWSMissingCredentials(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	_, err := testResolver().Resolve(context.Background(), Reference{Source: "aws", Name: "x"})
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("got %v", err)
	}
}

func TestAWSMissingRegion(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKID")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "k")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	_, err := testResolver().Resolve(context.Background(), Reference{Source: "aws", Name: "x"})
	if err == nil || !strings.Contains(err.Error(), "AWS_REGION") {
		t.Fatalf("got %v", err)
	}
}

// --- ResolveSelected ---

type mapSource struct {
	mu    sync.Mutex
	vals  map[string]string
	errs  map[string]error
	calls []string
}

func (m *mapSource) Resolve(_ context.Context, ref Reference) (Value, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, ref.Source+"/"+ref.Name)
	if err := m.errs[ref.Source+"/"+ref.Name]; err != nil {
		return Value{}, err
	}
	if v, ok := m.vals[ref.Source+"/"+ref.Name]; ok {
		return Value{Bytes: []byte(v)}, nil
	}
	return Value{}, ErrNotFound
}

func (m *mapSource) Close() error { return nil }

func TestResolveSelectedOnlySelectedModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := credentials.Save("XAI_API_KEY", "xk"); err != nil {
		t.Fatal(err)
	}
	// OPENAI_API_KEY is present too; it must NOT be resolved or returned.
	if err := credentials.Save("OPENAI_API_KEY", "ok"); err != nil {
		t.Fatal(err)
	}

	r := testResolver()
	cfg := config.Defaults()
	cfg.Connectivity.Mode = "direct"
	cfg.MCPServers = []config.MCPServer{
		{Name: "gh", URL: "https://api.githubcopilot.com/mcp/", CredentialEnv: "ABOX_MCP_GH_TOKEN"},
	}
	model := config.Model{Name: "grok-default", Provider: "xai", CredentialEnv: "XAI_API_KEY"}
	t.Setenv("ABOX_MCP_GH_TOKEN", "mtok")

	got, err := ResolveSelected(context.Background(), r, cfg, model)
	if err != nil {
		t.Fatal(err)
	}
	if got["XAI_API_KEY"] != "xk" {
		t.Fatalf("model key missing: %#v", got)
	}
	if got["ABOX_MCP_GH_TOKEN"] != "mtok" {
		t.Fatalf("mcp token missing: %#v", got)
	}
	if _, ok := got["OPENAI_API_KEY"]; ok {
		t.Fatalf("unselected model key leaked: %#v", got)
	}
}

func TestResolveSelectedMissingModelError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ABOX_TEST_NEVER_KEY", "")
	r := testResolver()
	cfg := config.Defaults()
	model := config.Model{Name: "m", Provider: "x", CredentialEnv: "ABOX_TEST_NEVER_KEY"}
	_, err := ResolveSelected(context.Background(), r, cfg, model)
	if err == nil || !strings.Contains(err.Error(), "ABOX_TEST_NEVER_KEY") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveSelectedReturnsMCPTokenWithModelError(t *testing.T) {
	r := NewResolver()
	source := &mapSource{vals: map[string]string{"env/MCP_TOKEN": "mcp-value"}}
	r.Register("env", source)
	cfg := config.Defaults()
	cfg.Connectivity.Mode = "direct"
	cfg.MCPServers = []config.MCPServer{{Name: "svc", URL: "https://mcp.example/api", CredentialEnv: "MCP_TOKEN"}}
	model := config.Model{Name: "missing", Provider: "other", CredentialEnv: "MODEL_TOKEN"}
	got, err := ResolveSelected(context.Background(), r, cfg, model)
	if err == nil || !strings.Contains(err.Error(), "MODEL_TOKEN") {
		t.Fatalf("got error %v", err)
	}
	if got["MCP_TOKEN"] != "mcp-value" {
		t.Fatalf("partial secrets %#v", got)
	}
}

func TestResolveSelectedReportsMCPBackendErrorAndContinues(t *testing.T) {
	backendErr := errors.New("backend unavailable")
	r := NewResolver()
	source := &mapSource{
		vals: map[string]string{"env/MODEL_TOKEN": "model", "env/GOOD_TOKEN": "good"},
		errs: map[string]error{"env/BAD_TOKEN": backendErr},
	}
	r.Register("env", source)
	cfg := config.Defaults()
	cfg.MCPServers = []config.MCPServer{
		{Name: "bad", URL: "https://bad.example/mcp", CredentialEnv: "BAD_TOKEN"},
		{Name: "good", URL: "https://good.example/mcp", CredentialEnv: "GOOD_TOKEN"},
	}
	model := config.Model{Name: "selected", Provider: "other", CredentialEnv: "MODEL_TOKEN"}
	got, err := ResolveSelected(context.Background(), r, cfg, model)
	if !errors.Is(err, backendErr) || !strings.Contains(err.Error(), `mcp server "bad"`) {
		t.Fatalf("got error %v", err)
	}
	if got["MODEL_TOKEN"] != "model" || got["GOOD_TOKEN"] != "good" {
		t.Fatalf("partial secrets %#v", got)
	}
}

func TestResolveSelectedMissingMCPTokenSkipped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := credentials.Save("XAI_API_KEY", "xk"); err != nil {
		t.Fatal(err)
	}
	r := testResolver()
	cfg := config.Defaults()
	cfg.Connectivity.Mode = "direct"
	cfg.MCPServers = []config.MCPServer{
		{Name: "gh", URL: "https://api.githubcopilot.com/mcp/", CredentialEnv: "ABOX_MCP_MISSING_TOKEN"},
	}
	t.Setenv("ABOX_MCP_MISSING_TOKEN", "")
	model := config.Model{Name: "grok-default", Provider: "xai", CredentialEnv: "XAI_API_KEY"}
	got, err := ResolveSelected(context.Background(), r, cfg, model)
	if err != nil {
		t.Fatal(err)
	}
	if got["XAI_API_KEY"] != "xk" {
		t.Fatalf("%#v", got)
	}
	if _, ok := got["ABOX_MCP_MISSING_TOKEN"]; ok {
		t.Fatalf("missing token not skipped: %#v", got)
	}
}

func TestResolveSelectedOfflineSkipsMCP(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := credentials.Save("XAI_API_KEY", "xk"); err != nil {
		t.Fatal(err)
	}
	r := testResolver()
	cfg := config.Defaults()
	cfg.Connectivity.Mode = "offline"
	cfg.MCPServers = []config.MCPServer{
		{Name: "gh", URL: "https://api.githubcopilot.com/mcp/", CredentialEnv: "ABOX_MCP_GH_TOKEN"},
	}
	model := config.Model{Name: "grok-default", Provider: "xai", CredentialEnv: "XAI_API_KEY"}
	got, err := ResolveSelected(context.Background(), r, cfg, model)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["ABOX_MCP_GH_TOKEN"]; ok {
		t.Fatalf("offline resolved an mcp token: %#v", got)
	}
}

func TestCredentialsFileMode(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := credentials.Save("XAI_API_KEY", "k"); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(credentials.Path())
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm %o", st.Mode().Perm())
	}
	if !strings.HasSuffix(filepath.ToSlash(credentials.Path()), "/.abox/credentials.env") {
		t.Fatalf("path %q", credentials.Path())
	}
}
