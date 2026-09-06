// Package credsource resolves host-held credentials. Guest packages must not
// import it. Zeroing Values is best-effort: Go's GC copies memory.
package credsource

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
)

type Reference struct {
	Source  string
	Name    string
	Field   string
	Version string
}

type Value struct {
	Bytes     []byte
	Version   string
	ExpiresAt time.Time
	LeaseID   string
}

func (v *Value) Zero() {
	if v == nil || v.Bytes == nil {
		return
	}
	for i := range v.Bytes {
		v.Bytes[i] = 0
	}
	v.Bytes = nil
}

func (v Value) String() string { return "credsource.Value(redacted)" }

func (v Value) GoString() string { return "credsource.Value(redacted)" }

// Source implementations must never include secret values in errors.
type Source interface {
	Resolve(ctx context.Context, ref Reference) (Value, error)
	Close() error
}

var (
	ErrNotFound = errors.New("credential not found")
	ErrLocked   = errors.New("credential store locked or unavailable")
)

type Resolver struct {
	mu      sync.Mutex
	sources map[string]Source
}

func NewResolver() *Resolver {
	r := &Resolver{sources: map[string]Source{}}
	r.Register("env", envSource{})
	r.Register("keychain", keychainSource{})
	r.Register("vault", vaultSource{})
	r.Register("azure", azureSource{})
	r.Register("aws", awsSource{})
	return r
}

func (r *Resolver) Register(name string, s Source) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sources[name] = s
}

func (r *Resolver) Resolve(ctx context.Context, ref Reference) (Value, error) {
	r.mu.Lock()
	s, ok := r.sources[ref.Source]
	r.mu.Unlock()
	if !ok {
		return Value{}, fmt.Errorf("unknown credential source %q for %q", ref.Source, ref.Name)
	}
	if strings.TrimSpace(ref.Name) == "" {
		return Value{}, fmt.Errorf("empty credential name for source %q", ref.Source)
	}
	return s.Resolve(ctx, ref)
}

func (r *Resolver) Close() error {
	r.mu.Lock()
	sources := make([]Source, 0, len(r.sources))
	for _, s := range r.sources {
		sources = append(sources, s)
	}
	r.mu.Unlock()
	var first error
	for _, s := range sources {
		if err := s.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func Present(ctx context.Context, r *Resolver, ref Reference) bool {
	v, err := r.Resolve(ctx, ref)
	if err != nil {
		return false
	}
	v.Zero()
	return true
}

func FromConfig(c config.CredentialRef) Reference {
	return Reference{Source: c.Source, Name: c.Name, Field: c.Field, Version: c.Version}
}
