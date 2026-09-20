package credsource

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
)

const (
	KeychainService        = "abox"
	keystoreCommandTimeout = 10 * time.Second
	secretServiceName      = "org.freedesktop.secrets"
)

type osKeystore interface {
	Available(context.Context) bool
	Get(context.Context, string) ([]byte, error)
	Set(context.Context, string, []byte) error
	Delete(context.Context, string) error
	Description() string
}

type keychainStore struct{}
type secretServiceStore struct{}
type unavailableStore struct{ goos string }

var (
	keystoreLookPath   = trustedKeystoreTool
	keystoreStat       = os.Stat
	currentOSKeystore  = func() osKeystore { return selectedOSKeystore(runtime.GOOS) }
	runKeystoreCommand = func(ctx context.Context, path string, args []string, stdin []byte) (stdout, stderr []byte, err error) {
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.Env = keystoreEnvironment()
		cmd.Stdin = bytes.NewReader(stdin)
		var outBuf, errBuf bytes.Buffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
		cmd.WaitDelay = time.Second
		err = cmd.Run()
		return outBuf.Bytes(), errBuf.Bytes(), err
	}
)

func trustedKeystoreTool(name string) (string, error) {
	var path string
	switch name {
	case "secret-tool":
		path = "/usr/bin/secret-tool"
	case "gdbus":
		path = "/usr/bin/gdbus"
	default:
		return "", fmt.Errorf("unrecognized keystore helper %q", name)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("keystore helper is not a regular executable: %s", path)
	}
	return path, nil
}

func keystoreEnvironment() []string {
	env := []string{"PATH=/usr/bin:/bin"}
	for _, name := range []string{
		"HOME", "DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR", "DISPLAY", "WAYLAND_DISPLAY", "LANG", "LC_ALL",
	} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}

type keystoreSource struct{}

func (keystoreSource) Resolve(ctx context.Context, ref Reference) (Value, error) {
	if !config.ValidEnvName(ref.Name) {
		return Value{}, fmt.Errorf("invalid keystore account name %q", ref.Name)
	}
	value, err := currentOSKeystore().Get(ctx, ref.Name)
	if err != nil {
		return Value{}, err
	}
	return Value{Bytes: value}, nil
}

func (keystoreSource) Close() error { return nil }

func selectedOSKeystore(goos string) osKeystore {
	switch goos {
	case "darwin":
		return keychainStore{}
	case "linux":
		return secretServiceStore{}
	default:
		return unavailableStore{goos: goos}
	}
}

func OSKeystoreAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), keystoreCommandTimeout)
	defer cancel()
	return currentOSKeystore().Available(ctx)
}

func OSKeystoreDescription() string {
	return currentOSKeystore().Description()
}

func SetOSKeystore(ctx context.Context, name string, value []byte) error {
	if !config.ValidEnvName(name) {
		return fmt.Errorf("invalid keystore account name %q", name)
	}
	return currentOSKeystore().Set(ctx, name, value)
}

func DeleteOSKeystore(ctx context.Context, name string) error {
	if !config.ValidEnvName(name) {
		return fmt.Errorf("invalid keystore account name %q", name)
	}
	return currentOSKeystore().Delete(ctx, name)
}

func (keychainStore) Available(_ context.Context) bool {
	info, err := keystoreStat("/usr/bin/security")
	return err == nil && securityToolAvailable("darwin", info.Mode())
}

func securityToolAvailable(goos string, mode os.FileMode) bool {
	return goos == "darwin" && mode.IsRegular() && mode.Perm()&0o111 != 0
}

func (keychainStore) Get(ctx context.Context, name string) ([]byte, error) {
	if !config.ValidEnvName(name) {
		return nil, fmt.Errorf("invalid keystore account name %q", name)
	}
	ctx, cancel := boundedKeystoreContext(ctx)
	defer cancel()
	stdout, stderr, err := runKeystoreCommand(ctx, "/usr/bin/security", []string{
		"find-generic-password", "-s", KeychainService, "-a", name, "-w",
	}, nil)
	if err == nil {
		value := strings.TrimSpace(string(stdout))
		if value == "" {
			return nil, fmt.Errorf("%w: keychain %s is empty", ErrNotFound, name)
		}
		return []byte(value), nil
	}
	if exitCode(err) == 44 {
		return nil, fmt.Errorf("%w: keychain %s: %w", ErrNotFound, name, err)
	}
	if bytes.Contains(stderr, []byte("User interaction is not allowed")) {
		return nil, fmt.Errorf("%w: keychain locked while reading %s: %w", ErrLocked, name, err)
	}
	return nil, keychainCommandError(ctx, "read", name, stderr, err)
}

func (keychainStore) Set(ctx context.Context, name string, value []byte) error {
	if !config.ValidEnvName(name) {
		return fmt.Errorf("invalid keystore account name %q", name)
	}
	ctx, cancel := boundedKeystoreContext(ctx)
	defer cancel()
	command := fmt.Sprintf("add-generic-password -U -s %s -a %s -X %s -j \"managed by abox\"\n",
		KeychainService, name, hex.EncodeToString(value))
	_, stderr, err := runKeystoreCommand(ctx, "/usr/bin/security", []string{"-i"}, []byte(command))
	if err == nil {
		return nil
	}
	if bytes.Contains(stderr, []byte("User interaction is not allowed")) {
		return fmt.Errorf("%w: keychain locked while writing %s: %w", ErrLocked, name, err)
	}
	return keychainCommandError(ctx, "write", name, stderr, err)
}

func (keychainStore) Delete(ctx context.Context, name string) error {
	if !config.ValidEnvName(name) {
		return fmt.Errorf("invalid keystore account name %q", name)
	}
	ctx, cancel := boundedKeystoreContext(ctx)
	defer cancel()
	_, stderr, err := runKeystoreCommand(ctx, "/usr/bin/security", []string{
		"delete-generic-password", "-s", KeychainService, "-a", name,
	}, nil)
	if err == nil {
		return nil
	}
	if exitCode(err) == 44 {
		return fmt.Errorf("%w: keychain %s: %w", ErrNotFound, name, err)
	}
	if bytes.Contains(stderr, []byte("User interaction is not allowed")) {
		return fmt.Errorf("%w: keychain locked while deleting %s: %w", ErrLocked, name, err)
	}
	return keychainCommandError(ctx, "delete", name, stderr, err)
}

func (keychainStore) Description() string { return "macOS Keychain (service abox)" }

func (secretServiceStore) Available(ctx context.Context) bool {
	return secretServiceAvailable(ctx) == nil
}

func (secretServiceStore) Get(ctx context.Context, name string) ([]byte, error) {
	if !config.ValidEnvName(name) {
		return nil, fmt.Errorf("invalid keystore account name %q", name)
	}
	ctx, cancel := boundedKeystoreContext(ctx)
	defer cancel()
	if err := secretServiceAvailable(ctx); err != nil {
		return nil, err
	}
	exists, err := secretServiceItemExists(ctx, name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("%w: Secret Service item %s", ErrNotFound, name)
	}
	tool, err := keystoreLookPath("secret-tool")
	if err != nil {
		return nil, unavailableError("find secret-tool", err)
	}
	stdout, stderr, err := runKeystoreCommand(ctx, tool, []string{
		"lookup", "service", KeychainService, "account", name,
	}, nil)
	if err != nil {
		return nil, secretServiceCommandError(ctx, "read", name, stderr, err)
	}
	return append([]byte(nil), stdout...), nil
}

func (secretServiceStore) Set(ctx context.Context, name string, value []byte) error {
	if !config.ValidEnvName(name) {
		return fmt.Errorf("invalid keystore account name %q", name)
	}
	ctx, cancel := boundedKeystoreContext(ctx)
	defer cancel()
	if err := secretServiceAvailable(ctx); err != nil {
		return err
	}
	tool, err := keystoreLookPath("secret-tool")
	if err != nil {
		return unavailableError("find secret-tool", err)
	}
	_, stderr, err := runKeystoreCommand(ctx, tool, []string{
		"store", "--label", "abox: " + name, "service", KeychainService, "account", name,
	}, value)
	if err != nil {
		return secretServiceCommandError(ctx, "write", name, stderr, err)
	}
	return nil
}

func (secretServiceStore) Delete(ctx context.Context, name string) error {
	if !config.ValidEnvName(name) {
		return fmt.Errorf("invalid keystore account name %q", name)
	}
	ctx, cancel := boundedKeystoreContext(ctx)
	defer cancel()
	if err := secretServiceAvailable(ctx); err != nil {
		return err
	}
	exists, err := secretServiceItemExists(ctx, name)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: Secret Service item %s", ErrNotFound, name)
	}
	tool, err := keystoreLookPath("secret-tool")
	if err != nil {
		return unavailableError("find secret-tool", err)
	}
	_, stderr, err := runKeystoreCommand(ctx, tool, []string{
		"clear", "service", KeychainService, "account", name,
	}, nil)
	if err != nil {
		return secretServiceCommandError(ctx, "delete", name, stderr, err)
	}
	return nil
}

func (secretServiceStore) Description() string { return "Secret Service (service abox)" }

func secretServiceAvailable(ctx context.Context) error {
	if _, err := keystoreLookPath("secret-tool"); err != nil {
		return unavailableError("find secret-tool", err)
	}
	gdbus, err := keystoreLookPath("gdbus")
	if err != nil {
		return unavailableError("find gdbus", err)
	}
	startArgs := []string{
		"call", "--session", "--dest", "org.freedesktop.DBus", "--object-path", "/org/freedesktop/DBus",
		"--method", "org.freedesktop.DBus.StartServiceByName", secretServiceName, "0",
	}
	_, startStderr, startErr := runKeystoreCommand(ctx, gdbus, startArgs, nil)
	if ctx.Err() != nil {
		return secretServiceCommandError(ctx, "activate provider", "", startStderr, startErr)
	}
	ownerArgs := []string{
		"call", "--session", "--dest", "org.freedesktop.DBus", "--object-path", "/org/freedesktop/DBus",
		"--method", "org.freedesktop.DBus.NameHasOwner", secretServiceName,
	}
	stdout, ownerStderr, ownerErr := runKeystoreCommand(ctx, gdbus, ownerArgs, nil)
	if ownerErr != nil {
		return secretServiceCommandError(ctx, "check provider", "", ownerStderr, ownerErr)
	}
	if !strings.Contains(string(stdout), "true") {
		if startErr != nil {
			return secretServiceCommandError(ctx, "activate provider", "", startStderr, startErr)
		}
		return fmt.Errorf("%w: Secret Service has no provider", ErrLocked)
	}
	return nil
}

func secretServiceItemExists(ctx context.Context, name string) (bool, error) {
	gdbus, err := keystoreLookPath("gdbus")
	if err != nil {
		return false, unavailableError("find gdbus", err)
	}
	attributes := fmt.Sprintf("{'service': <'%s'>, 'account': <'%s'>}", KeychainService, name)
	stdout, stderr, err := runKeystoreCommand(ctx, gdbus, []string{
		"call", "--session", "--dest", secretServiceName, "--object-path", "/org/freedesktop/secrets",
		"--method", "org.freedesktop.Secret.Service.SearchItems", attributes,
	}, nil)
	if err != nil {
		return false, secretServiceCommandError(ctx, "search", name, stderr, err)
	}
	return bytes.Contains(stdout, []byte("objectpath '/org/freedesktop/secrets/")), nil
}

func (unavailableStore) Available(context.Context) bool { return false }

func (s unavailableStore) Get(_ context.Context, name string) ([]byte, error) {
	return nil, fmt.Errorf("%w: OS keystore is unsupported on %s while reading %s", ErrLocked, s.goos, name)
}

func (s unavailableStore) Set(_ context.Context, name string, _ []byte) error {
	return fmt.Errorf("%w: OS keystore is unsupported on %s while writing %s", ErrLocked, s.goos, name)
}

func (s unavailableStore) Delete(_ context.Context, name string) error {
	return fmt.Errorf("%w: OS keystore is unsupported on %s while deleting %s", ErrLocked, s.goos, name)
}

func (s unavailableStore) Description() string { return "OS keystore" }

func boundedKeystoreContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, keystoreCommandTimeout)
}

func exitCode(err error) int {
	var coder interface{ ExitCode() int }
	if errors.As(err, &coder) {
		return coder.ExitCode()
	}
	return -1
}

func keychainCommandError(ctx context.Context, action, name string, stderr []byte, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%w: keychain %s %s: %w", ErrLocked, action, name, ctxErr)
	}
	detail := strings.TrimSpace(string(stderr))
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		return unavailableError("keychain "+action+" "+name, err)
	}
	if detail != "" {
		return fmt.Errorf("keychain %s %s: %s: %w", action, name, detail, err)
	}
	return fmt.Errorf("keychain %s %s: %w", action, name, err)
}

func secretServiceCommandError(ctx context.Context, action, name string, stderr []byte, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%w: Secret Service %s %s: %w", ErrLocked, action, name, ctxErr)
	}
	detail := strings.TrimSpace(string(stderr))
	if detail != "" {
		return fmt.Errorf("%w: Secret Service %s %s: %s: %w", ErrLocked, action, name, detail, err)
	}
	return fmt.Errorf("%w: Secret Service %s %s: %w", ErrLocked, action, name, err)
}

func unavailableError(action string, err error) error {
	return fmt.Errorf("%w: OS keystore unavailable (%s): %w", ErrLocked, action, err)
}
