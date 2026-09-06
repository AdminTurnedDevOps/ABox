package credsource

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
)

// keychainSource shells out to /usr/bin/security (no cgo). Values are ASCII:
// `security find-generic-password -w` hex-encodes anything else.
type keychainSource struct{}

const KeychainService = "abox"

var runSecurity = func(ctx context.Context, args []string, stdin string) (stdout, stderr string, err error) {
	cmd := exec.CommandContext(ctx, "/usr/bin/security", args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

func KeychainAvailable() bool {
	info, err := os.Stat("/usr/bin/security")
	if err != nil {
		return false
	}
	return securityToolAvailable(runtime.GOOS, info.Mode())
}

func securityToolAvailable(goos string, mode os.FileMode) bool {
	return goos == "darwin" && mode.IsRegular() && mode.Perm()&0o111 != 0
}

func (keychainSource) Resolve(ctx context.Context, ref Reference) (Value, error) {
	if !config.ValidEnvName(ref.Name) {
		return Value{}, fmt.Errorf("invalid keychain account name %q", ref.Name)
	}
	stdout, stderr, err := runSecurity(ctx, []string{
		"find-generic-password", "-s", KeychainService, "-a", ref.Name, "-w",
	}, "")
	if err == nil {
		v := strings.TrimSpace(stdout)
		if v == "" {
			return Value{}, fmt.Errorf("%w: keychain %s is empty", ErrNotFound, ref.Name)
		}
		return Value{Bytes: []byte(v)}, nil
	}
	if exitCode(err) == 44 {
		return Value{}, fmt.Errorf("%w: keychain %s: %w", ErrNotFound, ref.Name, err)
	}
	if strings.Contains(stderr, "User interaction is not allowed") {
		return Value{}, fmt.Errorf("%w: keychain locked while reading %s (unlock the login keychain or use credential source env): %w", ErrLocked, ref.Name, err)
	}
	return Value{}, keychainCommandError(ctx, "read", ref.Name, stderr, err)
}

func (keychainSource) Close() error { return nil }

func exitCode(err error) int {
	var coder interface{ ExitCode() int }
	if errors.As(err, &coder) {
		return coder.ExitCode()
	}
	return -1
}

// SetKeychain writes via `security -i` with `-X` hex on stdin so the secret
// never appears in argv. Names must be env-var syntax: -i parses a command language.
func SetKeychain(ctx context.Context, name string, value []byte) error {
	if !config.ValidEnvName(name) {
		return fmt.Errorf("invalid keychain account name %q", name)
	}
	cmdStr := fmt.Sprintf("add-generic-password -U -s %s -a %s -X %s -j \"managed by abox\"\n",
		KeychainService, name, hex.EncodeToString(value))
	_, stderr, err := runSecurity(ctx, []string{"-i"}, cmdStr)
	if err != nil {
		if strings.Contains(stderr, "User interaction is not allowed") {
			return fmt.Errorf("%w: keychain locked while writing %s (unlock the login keychain or use credential source env): %w", ErrLocked, name, err)
		}
		return keychainCommandError(ctx, "write", name, stderr, err)
	}
	return nil
}

func DeleteKeychain(ctx context.Context, name string) error {
	if !config.ValidEnvName(name) {
		return fmt.Errorf("invalid keychain account name %q", name)
	}
	_, stderr, err := runSecurity(ctx, []string{
		"delete-generic-password", "-s", KeychainService, "-a", name,
	}, "")
	if err != nil {
		if exitCode(err) == 44 {
			return fmt.Errorf("%w: keychain %s: %w", ErrNotFound, name, err)
		}
		if strings.Contains(stderr, "User interaction is not allowed") {
			return fmt.Errorf("%w: keychain locked while deleting %s: %w", ErrLocked, name, err)
		}
		return keychainCommandError(ctx, "delete", name, stderr, err)
	}
	return nil
}

func keychainCommandError(ctx context.Context, action, name, stderr string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("keychain %s %s: %w", action, name, ctxErr)
	}
	detail := strings.TrimSpace(stderr)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		if detail != "" {
			return fmt.Errorf("%w: keychain %s %s: %s: %w", ErrLocked, action, name, detail, err)
		}
		return fmt.Errorf("%w: keychain %s %s: %w", ErrLocked, action, name, err)
	}
	if detail != "" {
		return fmt.Errorf("keychain %s %s: %s: %w", action, name, detail, err)
	}
	return fmt.Errorf("keychain %s %s: %w", action, name, err)
}
