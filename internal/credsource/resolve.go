package credsource

import (
	"context"
	"errors"
	"fmt"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
)

// ResolveSelected returns the selected model credential plus enabled MCP
// tokens. A missing model credential is an error; a missing MCP token is skipped.
func ResolveSelected(ctx context.Context, r *Resolver, cfg config.File, model config.Model) (map[string]string, error) {
	out := map[string]string{}
	var resolveErrs []error
	ref := model.CredentialReference()
	val, err := r.Resolve(ctx, FromConfig(ref))
	if err != nil {
		val.Zero()
		resolveErrs = append(resolveErrs, fmt.Errorf("credential for model %q (%s %s): %w", model.Name, ref.Source, ref.Name, err))
	} else {
		out[model.EnvName()] = string(val.Bytes)
		val.Zero()
	}

	servers, err := cfg.ResolvedMCPServers()
	if err != nil {
		resolveErrs = append(resolveErrs, fmt.Errorf("resolve mcp servers: %w", err))
		return out, errors.Join(resolveErrs...)
	}
	for _, srv := range servers {
		sref := srv.CredentialReference()
		tok, err := r.Resolve(ctx, FromConfig(sref))
		if err != nil {
			tok.Zero()
			if !errors.Is(err, ErrNotFound) {
				resolveErrs = append(resolveErrs, fmt.Errorf("credential for mcp server %q (%s %s): %w", srv.Name, sref.Source, sref.Name, err))
			}
			continue
		}
		out[config.TokenEnv(srv)] = string(tok.Bytes)
		tok.Zero()
	}
	return out, errors.Join(resolveErrs...)
}
