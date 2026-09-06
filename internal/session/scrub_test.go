package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
)

func writeLegacySession(t *testing.T, root, id string, withSecrets bool) {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	guestCfg := map[string]any{
		"session_id": id,
		"capability": "cap",
		"model":      map[string]any{"name": "grok"},
	}
	if withSecrets {
		guestCfg["secrets"] = map[string]string{"XAI_API_KEY": "legacy-key"}
	}
	data, err := json.MarshalIndent(guestCfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "guest-config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	// config.raw: JSON + NUL padding to 1 MiB.
	cfgData := data
	if withSecrets {
		var obj map[string]json.RawMessage
		_ = json.Unmarshal(data, &obj)
		obj["secrets"], _ = json.Marshal(map[string]string{"XAI_API_KEY": "legacy-key"})
		cfgData, _ = json.MarshalIndent(obj, "", "  ")
	}
	if err := WritePaddedConfig(filepath.Join(dir, "config.raw"), cfgData); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "root.raw"), []byte("session disk bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestScrubSecretsEverywhereIncludesLegacyAppSupport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	aboxHome := t.TempDir()
	t.Setenv("ABOX_HOME", aboxHome)
	writeLegacySession(t, config.SessionRoot(), "modern", true)
	legacyRoot := filepath.Join(config.LegacyAppSupportDir(), "sessions")
	writeLegacySession(t, legacyRoot, "old", true)
	n, err := ScrubSecretsEverywhere()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("scrubbed %d", n)
	}
	for _, dir := range []string{
		filepath.Join(config.SessionRoot(), "modern"),
		filepath.Join(legacyRoot, "old"),
	} {
		body, err := os.ReadFile(filepath.Join(dir, "guest-config.json"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), `"secrets"`) {
			t.Fatalf("%s still has secrets: %s", dir, body)
		}
	}
}

func TestScrubSecretsRemovesLegacySecrets(t *testing.T) {
	root := t.TempDir()
	writeLegacySession(t, root, "aaa", true)
	writeLegacySession(t, root, "bbb", true)

	n, err := ScrubSecrets(root)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("scrubbed %d sessions, want 2", n)
	}

	for _, id := range []string{"aaa", "bbb"} {
		dir := filepath.Join(root, id)
		data, err := os.ReadFile(filepath.Join(dir, "guest-config.json"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "secrets") || strings.Contains(string(data), "legacy-key") {
			t.Fatalf("guest-config still has secrets: %s", data)
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(data, &obj); err != nil {
			t.Fatalf("scrubbed guest-config is not valid JSON: %v", err)
		}
		if obj["session_id"] == nil || obj["model"] == nil {
			t.Fatalf("scrub dropped unrelated fields: %s", data)
		}
		st, _ := os.Stat(filepath.Join(dir, "guest-config.json"))
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("guest-config perm %o", st.Mode().Perm())
		}

		raw, err := os.ReadFile(filepath.Join(dir, "config.raw"))
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) != ConfigDiskSize {
			t.Fatalf("config.raw size %d", len(raw))
		}
		if strings.Contains(string(raw), "legacy-key") || strings.Contains(string(raw), "secrets") {
			t.Fatalf("config.raw still has secrets")
		}
		st, _ = os.Stat(filepath.Join(dir, "config.raw"))
		if st.Mode().Perm() != 0o400 {
			t.Fatalf("config.raw perm %o", st.Mode().Perm())
		}
		// root.raw untouched.
		disk, err := os.ReadFile(filepath.Join(dir, "root.raw"))
		if err != nil {
			t.Fatal(err)
		}
		if string(disk) != "session disk bytes" {
			t.Fatalf("root.raw was modified: %q", disk)
		}
	}
}

func TestScrubSecretsIdempotent(t *testing.T) {
	root := t.TempDir()
	writeLegacySession(t, root, "aaa", true)
	if n, err := ScrubSecrets(root); err != nil || n != 1 {
		t.Fatalf("first pass: n=%d err=%v", n, err)
	}
	if n, err := ScrubSecrets(root); err != nil || n != 0 {
		t.Fatalf("second pass: n=%d err=%v (want no-op)", n, err)
	}
}

func TestScrubSecretsSkipsCleanSessions(t *testing.T) {
	root := t.TempDir()
	writeLegacySession(t, root, "clean", false)
	if n, err := ScrubSecrets(root); err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestScrubSecretsMissingRoot(t *testing.T) {
	n, err := ScrubSecrets(filepath.Join(t.TempDir(), "nope"))
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestScrubSecretsContinuesAndCombinesSessionErrors(t *testing.T) {
	root := t.TempDir()
	writeLegacySession(t, root, "good", true)

	badDir := filepath.Join(root, "bad-json")
	if err := os.MkdirAll(badDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badDir, "guest-config.json"), []byte(`{"secrets":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WritePaddedConfig(filepath.Join(badDir, "config.raw"), []byte(`not-json`)); err != nil {
		t.Fatal(err)
	}

	unreadableDir := filepath.Join(root, "unreadable")
	if err := os.MkdirAll(filepath.Join(unreadableDir, "guest-config.json"), 0o700); err != nil {
		t.Fatal(err)
	}

	n, err := ScrubSecrets(root)
	if n != 1 {
		t.Fatalf("scrubbed %d sessions, want 1", n)
	}
	if err == nil {
		t.Fatal("expected combined scrub errors")
	}
	for _, want := range []string{"bad-json", "guest-config.json", "config.raw", "unreadable"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
	good, readErr := os.ReadFile(filepath.Join(root, "good", "guest-config.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(good), "legacy-key") {
		t.Fatalf("good session was not scrubbed: %s", good)
	}
}

func TestScrubbedJSONObjectRejectsMalformedJSON(t *testing.T) {
	for _, data := range [][]byte{nil, []byte(`{"clean":`), []byte(`null`), []byte(`[]`)} {
		if scrubbed, err := scrubbedJSONObject(data); err == nil || scrubbed != nil {
			t.Fatalf("data %q: scrubbed=%q err=%v", data, scrubbed, err)
		}
	}
}

func TestScrubbedJSONObjectPreservesUnknownRawValues(t *testing.T) {
	input := []byte(`{"unknown":{"large":9007199254740993123456789,"future":[true,{"x":"y"}]},"secrets":{"TOKEN":"secret"},"tail":"kept"}`)
	out, err := scrubbedJSONObject(input)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	if _, exists := obj["secrets"]; exists {
		t.Fatalf("secrets retained: %s", out)
	}
	var unknown map[string]json.RawMessage
	if err := json.Unmarshal(obj["unknown"], &unknown); err != nil {
		t.Fatal(err)
	}
	if string(unknown["large"]) != "9007199254740993123456789" || string(obj["tail"]) != `"kept"` {
		t.Fatalf("unknown values changed: %s", out)
	}
}
