package credsource

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/credentials"
)

// KeychainEnabled is swapped to false in tests: the macOS keychain is
// machine-wide, so HOME-scoped temp dirs do not isolate it.
var KeychainEnabled = KeychainAvailable

type SaveResult struct {
	Source   string
	Keychain bool
	Note     string
}

func SavePreferred(ctx context.Context, envName, value string) (SaveResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if KeychainEnabled() {
		if err := SetKeychain(ctx, envName, []byte(value)); err != nil {
			if !errors.Is(err, ErrLocked) {
				return SaveResult{}, fmt.Errorf("keychain: %w", err)
			}
			if err := credentials.Save(envName, value); err != nil {
				return SaveResult{}, err
			}
			credentials.SetEnv(envName, value)
			return SaveResult{Source: "env", Note: "keychain locked; saved to " + credentials.Path()}, nil
		}
		if err := credentials.Delete(envName); err != nil {
			return SaveResult{}, fmt.Errorf("keychain saved %s but leftover file entry could not be removed: %w", envName, err)
		}
		credentials.SetEnv(envName, value)
		return SaveResult{Source: "keychain", Keychain: true, Note: "key saved to macOS keychain (service abox)"}, nil
	}
	if err := credentials.Save(envName, value); err != nil {
		return SaveResult{}, err
	}
	credentials.SetEnv(envName, value)
	return SaveResult{Source: "env", Note: "key saved to " + credentials.Path()}, nil
}
