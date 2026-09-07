package credsource

import (
	"context"
	"fmt"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
)

// ResolveSelected returns only the selected model credential for legacy
// protocol-2 guests. MCP credentials are always resolved by the host broker.
func ResolveSelected(ctx context.Context, r *Resolver, cfg config.File, model config.Model) (map[string]string, error) {
	_ = cfg
	out := map[string]string{}
	ref := model.CredentialReference()
	val, err := r.Resolve(ctx, FromConfig(ref))
	if err != nil {
		val.Zero()
		return out, fmt.Errorf("credential for model %q (%s %s): %w", model.Name, ref.Source, ref.Name, err)
	}
	out[model.EnvName()] = string(val.Bytes)
	val.Zero()
	return out, nil
}
