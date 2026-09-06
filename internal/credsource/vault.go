package credsource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type vaultSource struct{}

const vaultRequestTimeout = 15 * time.Second

func (vaultSource) Resolve(ctx context.Context, ref Reference) (Value, error) {
	addr := strings.TrimRight(strings.TrimSpace(os.Getenv("VAULT_ADDR")), "/")
	if addr == "" {
		return Value{}, fmt.Errorf("vault source requires VAULT_ADDR")
	}
	token := strings.TrimSpace(os.Getenv("VAULT_TOKEN"))
	if token == "" {
		if home, err := os.UserHomeDir(); err == nil {
			if b, err := os.ReadFile(filepath.Join(home, ".vault-token")); err == nil {
				token = strings.TrimSpace(string(b))
			}
		}
	}
	if token == "" {
		return Value{}, fmt.Errorf("vault source requires VAULT_TOKEN or ~/.vault-token")
	}
	mount, rest, found := strings.Cut(strings.Trim(ref.Name, "/"), "/")
	if !found || mount == "" || rest == "" {
		return Value{}, fmt.Errorf("vault reference %q must be a KV v2 path like secret/abox/name", ref.Name)
	}
	url := fmt.Sprintf("%s/v1/%s/data/%s", addr, mount, rest)
	if ref.Version != "" {
		url += "?version=" + strings.TrimPrefix(ref.Version, "?")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Value{}, err
	}
	req.Header.Set("X-Vault-Token", token)
	if ns := strings.TrimSpace(os.Getenv("VAULT_NAMESPACE")); ns != "" {
		req.Header.Set("X-Vault-Namespace", ns)
	}
	client := &http.Client{Timeout: vaultRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return Value{}, fmt.Errorf("vault request %s: %w", mount+"/"+rest, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Value{}, fmt.Errorf("%w: vault %s", ErrNotFound, ref.Name)
	case resp.StatusCode == http.StatusForbidden:
		return Value{}, fmt.Errorf("vault %s: permission denied (check the token's policy)", ref.Name)
	case resp.StatusCode >= 300:
		return Value{}, fmt.Errorf("vault %s: %s %s", ref.Name, resp.Status, vaultErrMessage(body))
	}
	var parsed struct {
		Data struct {
			Data     map[string]any `json:"data"`
			Metadata struct {
				Version json.Number `json:"version"`
			} `json:"metadata"`
		} `json:"data"`
	}
	if err := decodeJSONUseNumber(body, &parsed); err != nil {
		return Value{}, fmt.Errorf("vault %s: malformed response", ref.Name)
	}
	field := ref.Field
	if field == "" {
		field = "value"
	}
	raw, ok := parsed.Data.Data[field]
	if !ok {
		return Value{}, fmt.Errorf("%w: vault %s has no field %q", ErrNotFound, ref.Name, field)
	}
	return Value{Bytes: vaultFieldBytes(raw), Version: parsed.Data.Metadata.Version.String()}, nil
}

func decodeJSONUseNumber(data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func (vaultSource) Close() error { return nil }

func vaultFieldBytes(raw any) []byte {
	switch v := raw.(type) {
	case string:
		return []byte(v)
	case json.Number:
		return []byte(v.String())
	default:
		b, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		return b
	}
}

func vaultErrMessage(body []byte) string {
	var parsed struct {
		Errors []string `json:"errors"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && len(parsed.Errors) > 0 {
		return strings.Join(parsed.Errors, "; ")
	}
	return strings.TrimSpace(string(body))
}
