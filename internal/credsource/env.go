package credsource

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/AdminTurnedDevOps/ABox/internal/credentials"
)

type envSource struct{}

func (envSource) Resolve(_ context.Context, ref Reference) (Value, error) {
	if v := strings.TrimSpace(os.Getenv(ref.Name)); v != "" {
		return Value{Bytes: []byte(v)}, nil
	}
	cur, err := credentials.Load()
	if err != nil {
		return Value{}, fmt.Errorf("load %s: %w", credentials.Path(), err)
	}
	if v := strings.TrimSpace(cur[ref.Name]); v != "" {
		return Value{Bytes: []byte(v)}, nil
	}
	return Value{}, fmt.Errorf("%w: env %s", ErrNotFound, ref.Name)
}

func (envSource) Close() error { return nil }
