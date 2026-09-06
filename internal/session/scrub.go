package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
)

// ScrubSecrets removes the "secrets" key from guest-config.json and
// config.raw. It never deletes sessions.
func ScrubSecrets(root string) (int, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	scrubbed := 0
	var scrubErrs []error
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		n, err := scrubSessionDir(dir)
		if n > 0 {
			scrubbed++
		}
		if err != nil {
			scrubErrs = append(scrubErrs, fmt.Errorf("session %s: %w", e.Name(), err))
		}
	}
	return scrubbed, errors.Join(scrubErrs...)
}

func ScrubSecretsEverywhere() (int, error) {
	n, err := ScrubSecrets(config.SessionRoot())
	legacyDir := config.LegacyAppSupportDir()
	if legacyDir == "" {
		return n, err
	}
	n2, err2 := ScrubSecrets(filepath.Join(legacyDir, "sessions"))
	return n + n2, errors.Join(err, err2)
}

func scrubSessionDir(dir string) (int, error) {
	rewritten := 0
	var scrubErrs []error
	guestConfig := filepath.Join(dir, "guest-config.json")
	if ok, err := scrubJSONFile(guestConfig); err != nil {
		scrubErrs = append(scrubErrs, err)
	} else if ok {
		rewritten++
	}
	configDisk := filepath.Join(dir, "config.raw")
	if ok, err := scrubConfigDisk(configDisk); err != nil {
		scrubErrs = append(scrubErrs, err)
	} else if ok {
		rewritten++
	}
	return rewritten, errors.Join(scrubErrs...)
}

func scrubJSONFile(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	scrubbed, err := scrubbedJSONObject(data)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}
	if scrubbed == nil {
		return false, nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, scrubbed, 0o600); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return false, fmt.Errorf("replace %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return true, fmt.Errorf("chmod %s: %w", path, err)
	}
	return true, nil
}

func scrubConfigDisk(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("open %s: %w", path, err)
	}
	buf, err := io.ReadAll(io.LimitReader(f, ConfigDiskSize+1))
	closeErr := f.Close()
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	if closeErr != nil {
		return false, fmt.Errorf("close %s: %w", path, closeErr)
	}
	if len(buf) > ConfigDiskSize {
		return false, fmt.Errorf("read %s: config disk exceeds %d bytes", path, ConfigDiskSize)
	}
	data := buf
	if i := bytes.IndexByte(data, 0); i >= 0 { // guest JSON ends at first NUL
		data = data[:i]
	}
	scrubbed, err := scrubbedJSONObject(data)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}
	if scrubbed == nil {
		return false, nil
	}
	if err := WritePaddedConfig(path, scrubbed); err != nil {
		return false, fmt.Errorf("rewrite %s: %w", path, err)
	}
	return true, nil
}

func scrubbedJSONObject(data []byte) ([]byte, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, fmt.Errorf("empty JSON document")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("malformed JSON object: %w", err)
	}
	if obj == nil {
		return nil, fmt.Errorf("expected JSON object")
	}
	if _, ok := obj["secrets"]; !ok {
		return nil, nil
	}
	delete(obj, "secrets")
	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("scrub: %w", err)
	}
	return out, nil
}
