package credsource

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AdminTurnedDevOps/ABox/internal/credentials"
)

func testResolver() *Resolver {
	return NewResolver()
}

type fakeExitError int

func (e fakeExitError) Error() string { return fmt.Sprintf("exit status %d", int(e)) }
func (e fakeExitError) ExitCode() int { return int(e) }

func TestValueStringRedacts(t *testing.T) {
	v := Value{Bytes: []byte("super-secret")}
	if got := v.String(); got != "credsource.Value(redacted)" {
		t.Fatalf("String()=%q", got)
	}
	if got := strings.TrimSpace(strings.ReplaceAll(strings.ToLower("credsource.Value(redacted)"), " ", "")); strings.Contains(got, "super-secret") {
		t.Fatal("String leaked secret")
	}
	if v.Bytes == nil {
		t.Fatal("String must not consume the value")
	}
}

func TestValueZeroOverwrites(t *testing.T) {
	v := Value{Bytes: []byte("super-secret")}
	v.Zero()
	if v.Bytes != nil {
		t.Fatalf("Bytes not cleared: %q", string(v.Bytes))
	}
}

func TestResolverUnknownSource(t *testing.T) {
	_, err := testResolver().Resolve(context.Background(), Reference{Source: "nope", Name: "X"})
	if err == nil || !strings.Contains(err.Error(), `unknown credential source "nope"`) {
		t.Fatalf("got %v", err)
	}
}

func TestResolverEmptyName(t *testing.T) {
	_, err := testResolver().Resolve(context.Background(), Reference{Source: "env", Name: "  "})
	if err == nil || !strings.Contains(err.Error(), "empty credential name") {
		t.Fatalf("got %v", err)
	}
}

func TestEnvSourceProcessEnvWins(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ABOX_TEST_ENV_KEY", "from-process")
	if err := credentials.Save("ABOX_TEST_ENV_KEY", "from-file"); err != nil {
		t.Fatal(err)
	}
	v, err := testResolver().Resolve(context.Background(), Reference{Source: "env", Name: "ABOX_TEST_ENV_KEY"})
	if err != nil {
		t.Fatal(err)
	}
	if string(v.Bytes) != "from-process" {
		t.Fatalf("got %q", v.Bytes)
	}
}

func TestEnvSourceFallsBackToFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ABOX_TEST_ENV_KEY2", "")
	if err := credentials.Save("ABOX_TEST_ENV_KEY2", "from-file"); err != nil {
		t.Fatal(err)
	}
	v, err := testResolver().Resolve(context.Background(), Reference{Source: "env", Name: "ABOX_TEST_ENV_KEY2"})
	if err != nil {
		t.Fatal(err)
	}
	if string(v.Bytes) != "from-file" {
		t.Fatalf("got %q", v.Bytes)
	}
}

func TestEnvSourceNotFound(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ABOX_TEST_ENV_MISSING", "")
	_, err := testResolver().Resolve(context.Background(), Reference{Source: "env", Name: "ABOX_TEST_ENV_MISSING"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestKeychainResolveReadsPasswordOnly(t *testing.T) {
	var argv []string
	orig := runSecurity
	runSecurity = func(_ context.Context, args []string, stdin string) (string, string, error) {
		argv = args
		return "kchain-value\n", "", nil
	}
	t.Cleanup(func() { runSecurity = orig })

	v, err := testResolver().Resolve(context.Background(), Reference{Source: "keychain", Name: "ANTHROPIC_API_KEY"})
	if err != nil {
		t.Fatal(err)
	}
	if string(v.Bytes) != "kchain-value" {
		t.Fatalf("got %q", v.Bytes)
	}
	want := []string{"find-generic-password", "-s", "abox", "-a", "ANTHROPIC_API_KEY", "-w"}
	if len(argv) != len(want) {
		t.Fatalf("argv=%v", argv)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv=%v", argv)
		}
	}
	for _, a := range argv {
		if strings.Contains(a, "kchain-value") {
			t.Fatalf("secret value leaked into argv: %v", argv)
		}
	}
}

func TestKeychainResolveNotFoundExit44(t *testing.T) {
	orig := runSecurity
	runSecurity = func(_ context.Context, args []string, _ string) (string, string, error) {
		return "", "could not be found", fakeExitError(44)
	}
	t.Cleanup(func() { runSecurity = orig })

	_, err := testResolver().Resolve(context.Background(), Reference{Source: "keychain", Name: "NOPE"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestKeychainResolveLocked(t *testing.T) {
	orig := runSecurity
	runSecurity = func(_ context.Context, _ []string, _ string) (string, string, error) {
		return "", "security: SecKeychainSearchCopyNext(): User interaction is not allowed.", fakeExitError(1)
	}
	t.Cleanup(func() { runSecurity = orig })

	_, err := testResolver().Resolve(context.Background(), Reference{Source: "keychain", Name: "X"})
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("got %v", err)
	}
}

func TestKeychainSetUsesStdinHexNoArgvLeak(t *testing.T) {
	var argv, stdin, stderr []string
	orig := runSecurity
	runSecurity = func(_ context.Context, args []string, in string) (string, string, error) {
		argv = args
		stdin = append(stdin, in)
		return "", "", nil
	}
	t.Cleanup(func() { runSecurity = orig })

	if err := SetKeychain(context.Background(), "ANTHROPIC_API_KEY", []byte("plain-key")); err != nil {
		t.Fatal(err)
	}
	if len(argv) != 1 || argv[0] != "-i" {
		t.Fatalf("argv=%v, want only [\"-i\"]", argv)
	}
	if len(stdin) != 1 {
		t.Fatalf("stdin writes: %d", len(stdin))
	}
	cmd := stdin[0]
	if strings.Contains(cmd, "plain-key") {
		t.Fatal("plaintext secret in security stdin command")
	}
	if !strings.Contains(cmd, "add-generic-password -U -s abox -a ANTHROPIC_API_KEY -X") {
		t.Fatalf("stdin command: %q", cmd)
	}
	_ = stderr
}

func TestKeychainRejectsCommandInputAccountName(t *testing.T) {
	called := false
	orig := runSecurity
	runSecurity = func(_ context.Context, _ []string, _ string) (string, string, error) {
		called = true
		return "", "", nil
	}
	t.Cleanup(func() { runSecurity = orig })

	err := SetKeychain(context.Background(), "SAFE_NAME\n delete-generic-password", []byte("value"))
	if err == nil || !strings.Contains(err.Error(), "invalid keychain account name") {
		t.Fatalf("got %v", err)
	}
	if called {
		t.Fatal("security invoked for invalid account name")
	}
}

func TestKeychainCommandErrorWrapsCause(t *testing.T) {
	cause := errors.New("security failed")
	orig := runSecurity
	runSecurity = func(_ context.Context, _ []string, _ string) (string, string, error) {
		return "", "diagnostic", cause
	}
	t.Cleanup(func() { runSecurity = orig })

	err := SetKeychain(context.Background(), "SAFE_NAME", []byte("value"))
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "diagnostic") {
		t.Fatalf("got %v", err)
	}
}

func TestKeychainMissingToolIsUnavailableAndWrapsCause(t *testing.T) {
	cause := &os.PathError{Op: "fork/exec", Path: "/usr/bin/security", Err: os.ErrNotExist}
	orig := runSecurity
	runSecurity = func(_ context.Context, _ []string, _ string) (string, string, error) {
		return "", "", cause
	}
	t.Cleanup(func() { runSecurity = orig })

	_, err := testResolver().Resolve(context.Background(), Reference{Source: "keychain", Name: "SAFE_NAME"})
	if !errors.Is(err, ErrLocked) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got %v", err)
	}
}

func TestSecurityToolAvailable(t *testing.T) {
	if !securityToolAvailable("darwin", 0o755) {
		t.Fatal("executable regular file should be available on darwin")
	}
	if securityToolAvailable("linux", 0o755) || securityToolAvailable("darwin", os.ModeDir|0o755) || securityToolAvailable("darwin", 0o644) {
		t.Fatal("unsupported OS, directory, or non-executable file reported available")
	}
}

func TestSavePreferredReportsKeychainSource(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	origEnabled := KeychainEnabled
	origSecurity := runSecurity
	KeychainEnabled = func() bool { return true }
	runSecurity = func(_ context.Context, args []string, _ string) (string, string, error) {
		if len(args) != 1 || args[0] != "-i" {
			t.Fatalf("args %v", args)
		}
		return "", "", nil
	}
	t.Cleanup(func() {
		KeychainEnabled = origEnabled
		runSecurity = origSecurity
	})

	result, err := SavePreferred(context.Background(), "SAFE_NAME", "value")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "keychain" || !result.Keychain {
		t.Fatalf("result %#v", result)
	}
}

func TestSavePreferredRemovesLeftoverFileEntry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := credentials.Save("SAFE_NAME", "old-file-value"); err != nil {
		t.Fatal(err)
	}
	origEnabled := KeychainEnabled
	origSecurity := runSecurity
	KeychainEnabled = func() bool { return true }
	runSecurity = func(_ context.Context, args []string, _ string) (string, string, error) {
		return "", "", nil
	}
	t.Cleanup(func() {
		KeychainEnabled = origEnabled
		runSecurity = origSecurity
	})

	if _, err := SavePreferred(context.Background(), "SAFE_NAME", "new-keychain-value"); err != nil {
		t.Fatal(err)
	}
	got, err := credentials.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["SAFE_NAME"]; ok {
		t.Fatalf("leftover file entry %#v", got)
	}
}

func TestKeychainDelete(t *testing.T) {
	var argv []string
	orig := runSecurity
	runSecurity = func(_ context.Context, args []string, _ string) (string, string, error) {
		argv = args
		return "", "", nil
	}
	t.Cleanup(func() { runSecurity = orig })

	if err := DeleteKeychain(context.Background(), "ANTHROPIC_API_KEY"); err != nil {
		t.Fatal(err)
	}
	want := []string{"delete-generic-password", "-s", "abox", "-a", "ANTHROPIC_API_KEY"}
	if len(argv) != len(want) {
		t.Fatalf("argv=%v", argv)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv=%v", argv)
		}
	}
}

func TestVaultResolvePathAndToken(t *testing.T) {
	srv := newVaultServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/secret/data/abox/anthropic" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("X-Vault-Token") != "vault-token" {
			t.Errorf("token header missing")
		}
		if r.URL.Query().Get("version") != "" {
			t.Errorf("unexpected version %q", r.URL.Query().Get("version"))
		}
		_, _ = w.Write([]byte(`{"data":{"data":{"value":"vv"},"metadata":{"version":7}}}`))
	})
	t.Setenv("VAULT_ADDR", srv.URL)
	t.Setenv("VAULT_TOKEN", "vault-token")
	v, err := testResolver().Resolve(context.Background(), Reference{Source: "vault", Name: "secret/abox/anthropic"})
	if err != nil {
		t.Fatal(err)
	}
	if string(v.Bytes) != "vv" || v.Version != "7" {
		t.Fatalf("got %q version %q", v.Bytes, v.Version)
	}
}

func TestVaultResolveVersionAndField(t *testing.T) {
	srv := newVaultServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("version") != "3" {
			t.Errorf("version %q", r.URL.Query().Get("version"))
		}
		_, _ = w.Write([]byte(`{"data":{"data":{"api_key":"fieldval","other":"x"},"metadata":{"version":3}}}`))
	})
	t.Setenv("VAULT_ADDR", srv.URL)
	t.Setenv("VAULT_TOKEN", "vault-token")
	v, err := testResolver().Resolve(context.Background(), Reference{Source: "vault", Name: "secret/abox/anthropic", Field: "api_key", Version: "3"})
	if err != nil {
		t.Fatal(err)
	}
	if string(v.Bytes) != "fieldval" {
		t.Fatalf("got %q", v.Bytes)
	}
}

func TestVaultNumericFieldPreservesPrecision(t *testing.T) {
	const large = "9007199254740993123456789"
	srv := newVaultServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"data":{"large":` + large + `},"metadata":{"version":9}}}`))
	})
	t.Setenv("VAULT_ADDR", srv.URL)
	t.Setenv("VAULT_TOKEN", "vault-token")
	v, err := testResolver().Resolve(context.Background(), Reference{Source: "vault", Name: "secret/abox/number", Field: "large"})
	if err != nil {
		t.Fatal(err)
	}
	if string(v.Bytes) != large {
		t.Fatalf("got %q", v.Bytes)
	}
}

func TestVaultTokenFileFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("VAULT_TOKEN", "")
	if err := os.WriteFile(filepath.Join(home, ".vault-token"), []byte("file-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := newVaultServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "file-token" {
			t.Errorf("token from file missing")
		}
		_, _ = w.Write([]byte(`{"data":{"data":{"value":"ok"},"metadata":{"version":1}}}`))
	})
	t.Setenv("VAULT_ADDR", srv.URL)
	v, err := testResolver().Resolve(context.Background(), Reference{Source: "vault", Name: "secret/abox/anthropic"})
	if err != nil {
		t.Fatal(err)
	}
	if string(v.Bytes) != "ok" {
		t.Fatalf("got %q", v.Bytes)
	}
}

func TestVaultNotFound(t *testing.T) {
	srv := newVaultServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	t.Setenv("VAULT_ADDR", srv.URL)
	t.Setenv("VAULT_TOKEN", "vault-token")
	_, err := testResolver().Resolve(context.Background(), Reference{Source: "vault", Name: "secret/abox/anthropic"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestVaultMissingAddr(t *testing.T) {
	t.Setenv("VAULT_ADDR", "")
	_, err := testResolver().Resolve(context.Background(), Reference{Source: "vault", Name: "secret/abox/anthropic"})
	if err == nil || !strings.Contains(err.Error(), "VAULT_ADDR") {
		t.Fatalf("got %v", err)
	}
}
