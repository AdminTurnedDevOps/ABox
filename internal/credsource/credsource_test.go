package credsource

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

type staticSource struct{ value []byte }

func (s staticSource) Resolve(context.Context, Reference) (Value, error) {
	return Value{Bytes: append([]byte(nil), s.value...)}, nil
}
func (staticSource) Close() error { return nil }

func TestResolverAcceptsKeystoreAliases(t *testing.T) {
	r := &Resolver{sources: map[string]Source{}}
	r.Register("keystore", staticSource{value: []byte("value")})
	for _, source := range []string{"keystore", "keychain", "secretservice"} {
		v, err := r.Resolve(context.Background(), Reference{Source: source, Name: "SAFE_NAME"})
		if err != nil || string(v.Bytes) != "value" {
			t.Fatalf("%s: value=%q err=%v", source, v.Bytes, err)
		}
	}
}

func TestOSKeystoreDispatch(t *testing.T) {
	if _, ok := selectedOSKeystore("darwin").(keychainStore); !ok {
		t.Fatal("darwin did not select the macOS keychain")
	}
	if _, ok := selectedOSKeystore("linux").(secretServiceStore); !ok {
		t.Fatal("linux did not select Secret Service")
	}
	if _, ok := selectedOSKeystore("windows").(unavailableStore); !ok {
		t.Fatal("unsupported platform did not select unavailable store")
	}
}

func installKeystoreFakes(t *testing.T, run func(context.Context, string, []string, []byte) ([]byte, []byte, error)) {
	t.Helper()
	origLookPath := keystoreLookPath
	origRun := runKeystoreCommand
	keystoreLookPath = func(name string) (string, error) { return "/fake/" + name, nil }
	runKeystoreCommand = run
	t.Cleanup(func() {
		keystoreLookPath = origLookPath
		runKeystoreCommand = origRun
	})
}

func successfulSecretServiceCommand(_ context.Context, path string, args []string, _ []byte) ([]byte, []byte, error) {
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "StartServiceByName"):
		return []byte("(uint32 2,)"), nil, nil
	case strings.Contains(joined, "NameHasOwner"):
		return []byte("(true,)"), nil, nil
	case strings.Contains(joined, "SearchItems"):
		return []byte("([objectpath '/org/freedesktop/secrets/collection/login/1'], @ao [])"), nil, nil
	case path == "/fake/secret-tool":
		return nil, nil, nil
	default:
		return nil, nil, fmt.Errorf("unexpected command %s %v", path, args)
	}
}

func TestSecretServiceGetIsByteExact(t *testing.T) {
	want := []byte(" value with spaces\nand newline\n")
	installKeystoreFakes(t, func(ctx context.Context, path string, args []string, stdin []byte) ([]byte, []byte, error) {
		if path == "/fake/secret-tool" {
			if got := strings.Join(args, " "); got != "lookup service abox account SAFE_NAME" {
				t.Fatalf("lookup args %q", got)
			}
			return append([]byte(nil), want...), nil, nil
		}
		return successfulSecretServiceCommand(ctx, path, args, stdin)
	})
	got, err := (secretServiceStore{}).Get(context.Background(), "SAFE_NAME")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSecretServiceSetUsesOnlyByteExactStdin(t *testing.T) {
	secret := []byte("plain-key\n")
	installKeystoreFakes(t, func(ctx context.Context, path string, args []string, stdin []byte) ([]byte, []byte, error) {
		if path == "/fake/secret-tool" {
			for _, arg := range args {
				if strings.Contains(arg, "plain-key") {
					t.Fatalf("secret leaked in argv: %v", args)
				}
			}
			if got := strings.Join(args, " "); got != "store --label abox: SAFE_NAME service abox account SAFE_NAME" {
				t.Fatalf("store args %q", got)
			}
			if !bytes.Equal(stdin, secret) {
				t.Fatalf("stdin %q want %q", stdin, secret)
			}
			return nil, nil, nil
		}
		return successfulSecretServiceCommand(ctx, path, args, stdin)
	})
	if err := (secretServiceStore{}).Set(context.Background(), "SAFE_NAME", secret); err != nil {
		t.Fatal(err)
	}
}

func TestSecretServiceMissingItemIsNotFound(t *testing.T) {
	installKeystoreFakes(t, func(ctx context.Context, path string, args []string, stdin []byte) ([]byte, []byte, error) {
		if strings.Contains(strings.Join(args, " "), "SearchItems") {
			return []byte("(@ao [], @ao [])"), nil, nil
		}
		if path == "/fake/secret-tool" {
			t.Fatal("secret-tool called for confirmed missing item")
		}
		return successfulSecretServiceCommand(ctx, path, args, stdin)
	})
	_, err := (secretServiceStore{}).Get(context.Background(), "MISSING")
	if !errors.Is(err, ErrNotFound) || errors.Is(err, ErrLocked) {
		t.Fatalf("got %v", err)
	}
}

func TestSecretServiceLockedItemIsLocked(t *testing.T) {
	installKeystoreFakes(t, func(ctx context.Context, path string, args []string, stdin []byte) ([]byte, []byte, error) {
		if path == "/fake/secret-tool" {
			return nil, []byte("prompt dismissed"), fakeExitError(1)
		}
		return successfulSecretServiceCommand(ctx, path, args, stdin)
	})
	_, err := (secretServiceStore{}).Get(context.Background(), "SAFE_NAME")
	if !errors.Is(err, ErrLocked) || errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestSecretServiceUnavailableWithoutProvider(t *testing.T) {
	installKeystoreFakes(t, func(_ context.Context, _ string, args []string, _ []byte) ([]byte, []byte, error) {
		if strings.Contains(strings.Join(args, " "), "NameHasOwner") {
			return []byte("(false,)"), nil, nil
		}
		return nil, []byte("not activatable"), fakeExitError(1)
	})
	if err := secretServiceAvailable(context.Background()); !errors.Is(err, ErrLocked) {
		t.Fatalf("got %v", err)
	}
}

func TestSecretServiceMissingSessionBusIsLocked(t *testing.T) {
	installKeystoreFakes(t, func(_ context.Context, _ string, args []string, _ []byte) ([]byte, []byte, error) {
		if strings.Contains(strings.Join(args, " "), "NameHasOwner") {
			return nil, []byte("Cannot autolaunch D-Bus without X11 $DISPLAY"), fakeExitError(1)
		}
		return nil, []byte("session bus unavailable"), fakeExitError(1)
	})
	if err := secretServiceAvailable(context.Background()); !errors.Is(err, ErrLocked) {
		t.Fatalf("got %v", err)
	}
}

func TestSecretServiceMissingGDBusIsUnavailable(t *testing.T) {
	origLookPath := keystoreLookPath
	keystoreLookPath = func(name string) (string, error) {
		if name == "gdbus" {
			return "", os.ErrNotExist
		}
		return "/fake/" + name, nil
	}
	t.Cleanup(func() { keystoreLookPath = origLookPath })
	if err := secretServiceAvailable(context.Background()); !errors.Is(err, ErrLocked) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got %v", err)
	}
}

func TestSecretServiceTimeoutIsLockedAndRunnerReturns(t *testing.T) {
	reaped := make(chan struct{})
	installKeystoreFakes(t, func(ctx context.Context, _ string, _ []string, _ []byte) ([]byte, []byte, error) {
		<-ctx.Done()
		close(reaped)
		return nil, nil, ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := (secretServiceStore{}).Get(ctx, "SAFE_NAME")
	if !errors.Is(err, ErrLocked) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	select {
	case <-reaped:
	default:
		t.Fatal("command runner did not return on timeout")
	}
}

func TestSecretServiceProviderLossDuringSaveIsLocked(t *testing.T) {
	installKeystoreFakes(t, func(ctx context.Context, path string, args []string, stdin []byte) ([]byte, []byte, error) {
		if path == "/fake/secret-tool" {
			return nil, []byte("service vanished"), fakeExitError(1)
		}
		return successfulSecretServiceCommand(ctx, path, args, stdin)
	})
	if err := (secretServiceStore{}).Set(context.Background(), "SAFE_NAME", []byte("value")); !errors.Is(err, ErrLocked) {
		t.Fatalf("got %v", err)
	}
}

func TestMacOSKeychainBehaviorRetained(t *testing.T) {
	var setInput []byte
	installKeystoreFakes(t, func(_ context.Context, path string, args []string, stdin []byte) ([]byte, []byte, error) {
		if path != "/usr/bin/security" {
			t.Fatalf("path %q", path)
		}
		if len(args) == 1 && args[0] == "-i" {
			setInput = append([]byte(nil), stdin...)
			return nil, nil, nil
		}
		return []byte("keychain-value\n"), nil, nil
	})
	value, err := (keychainStore{}).Get(context.Background(), "SAFE_NAME")
	if err != nil || string(value) != "keychain-value" {
		t.Fatalf("value=%q err=%v", value, err)
	}
	if err := (keychainStore{}).Set(context.Background(), "SAFE_NAME", []byte("plain-key")); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(setInput, []byte("plain-key")) || !bytes.Contains(setInput, []byte(hex.EncodeToString([]byte("plain-key")))) {
		t.Fatalf("security input %q", setInput)
	}
}

func TestMacOSKeychainErrorMappingsRetained(t *testing.T) {
	installKeystoreFakes(t, func(_ context.Context, _ string, args []string, _ []byte) ([]byte, []byte, error) {
		if len(args) > 0 && args[0] == "find-generic-password" {
			if slicesContain(args, "MISSING") {
				return nil, []byte("could not be found"), fakeExitError(44)
			}
			return nil, []byte("User interaction is not allowed"), fakeExitError(1)
		}
		return nil, nil, nil
	})
	if _, err := (keychainStore{}).Get(context.Background(), "MISSING"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := (keychainStore{}).Get(context.Background(), "LOCKED"); !errors.Is(err, ErrLocked) {
		t.Fatalf("locked: %v", err)
	}
}

func slicesContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestKeystoreRejectsUnsafeAccountBeforeCommand(t *testing.T) {
	called := false
	installKeystoreFakes(t, func(context.Context, string, []string, []byte) ([]byte, []byte, error) {
		called = true
		return nil, nil, nil
	})
	err := SetOSKeystore(context.Background(), "SAFE_NAME\naccount", []byte("value"))
	if err == nil || !strings.Contains(err.Error(), "invalid keystore account name") || called {
		t.Fatalf("err=%v called=%v", err, called)
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

func TestSavePreferredWarnsOnUnavailableFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	origEnabled := KeystoreEnabled
	KeystoreEnabled = func(context.Context) bool { return false }
	t.Cleanup(func() { KeystoreEnabled = origEnabled })
	result, err := SavePreferred(context.Background(), "SAFE_NAME", "value")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "env" || result.Keystore || !strings.Contains(result.Note, "warning:") || !strings.Contains(result.Note, "0600") {
		t.Fatalf("result %#v", result)
	}
	info, err := os.Stat(credentials.Path())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credentials mode=%v", info.Mode())
	}
}

type lockedStore struct{}

func (lockedStore) Available(context.Context) bool { return true }
func (lockedStore) Get(context.Context, string) ([]byte, error) {
	return nil, ErrLocked
}
func (lockedStore) Set(context.Context, string, []byte) error { return ErrLocked }
func (lockedStore) Delete(context.Context, string) error      { return ErrLocked }
func (lockedStore) Description() string                       { return "locked test store" }

func TestSavePreferredWarnsOnLockedFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	origEnabled := KeystoreEnabled
	origStore := currentOSKeystore
	KeystoreEnabled = func(context.Context) bool { return true }
	currentOSKeystore = func() osKeystore { return lockedStore{} }
	t.Cleanup(func() {
		KeystoreEnabled = origEnabled
		currentOSKeystore = origStore
	})
	result, err := SavePreferred(context.Background(), "SAFE_NAME", "value")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "env" || !strings.Contains(result.Note, "locked or unavailable") {
		t.Fatalf("result %#v", result)
	}
}

func TestSavePreferredReportsCanonicalKeystoreAndRemovesFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := credentials.Save("SAFE_NAME", "old-value"); err != nil {
		t.Fatal(err)
	}
	origEnabled := KeystoreEnabled
	origStore := currentOSKeystore
	KeystoreEnabled = func(context.Context) bool { return true }
	currentOSKeystore = func() osKeystore { return secretServiceStore{} }
	t.Cleanup(func() {
		KeystoreEnabled = origEnabled
		currentOSKeystore = origStore
	})
	installKeystoreFakes(t, successfulSecretServiceCommand)
	result, err := SavePreferred(context.Background(), "SAFE_NAME", "new-value")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "keystore" || !result.Keystore {
		t.Fatalf("result %#v", result)
	}
	got, err := credentials.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["SAFE_NAME"]; ok {
		t.Fatalf("leftover file entry %#v", got)
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
