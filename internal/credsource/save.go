package credsource

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/credentials"
)

// KeystoreEnabled is swapped in tests because OS keystores are not isolated by
// HOME-scoped temporary directories.
var KeystoreEnabled = func(ctx context.Context) bool { return currentOSKeystore().Available(ctx) }

type SaveResult struct {
	Source   string
	Keystore bool
	Note     string
}

func SavePreferred(ctx context.Context, envName, value string) (SaveResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if KeystoreEnabled(ctx) {
		if err := SetOSKeystore(ctx, envName, []byte(value)); err != nil {
			if !errors.Is(err, ErrLocked) {
				return SaveResult{}, fmt.Errorf("keystore: %w", err)
			}
			if err := credentials.Save(envName, value); err != nil {
				return SaveResult{}, err
			}
			credentials.SetEnv(envName, value)
			return SaveResult{Source: "env", Note: "warning: OS keystore locked or unavailable; saved plaintext fallback to " + credentials.Path() + " (mode 0600)"}, nil
		}
		if err := credentials.Delete(envName); err != nil {
			return SaveResult{}, fmt.Errorf("keystore saved %s but leftover file entry could not be removed: %w", envName, err)
		}
		credentials.SetEnv(envName, value)
		return SaveResult{Source: "keystore", Keystore: true, Note: "key saved to " + OSKeystoreDescription()}, nil
	}
	if err := credentials.Save(envName, value); err != nil {
		return SaveResult{}, err
	}
	credentials.SetEnv(envName, value)
	return SaveResult{Source: "env", Note: "warning: OS keystore unavailable; saved plaintext fallback to " + credentials.Path() + " (mode 0600)"}, nil
}
