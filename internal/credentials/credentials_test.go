package credentials

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := Save("XAI_API_KEY", "test-key-not-real"); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got["XAI_API_KEY"] != "test-key-not-real" {
		t.Fatalf("got %#v", got)
	}
	st, err := os.Stat(Path())
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm %o", st.Mode().Perm())
	}
	_ = filepath.Dir(dir)
}

func TestSavePreservesMCPTokens(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := Save("XAI_API_KEY", "llm-key"); err != nil {
		t.Fatal(err)
	}
	if err := Save("ABOX_MCP_GITHUB_TOKEN", "mcp-key"); err != nil {
		t.Fatal(err)
	}
	if err := Save("XAI_API_KEY", "llm-key-2"); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got["ABOX_MCP_GITHUB_TOKEN"] != "mcp-key" {
		t.Fatalf("mcp token dropped: %#v", got)
	}
	if got["XAI_API_KEY"] != "llm-key-2" {
		t.Fatalf("llm key: %#v", got)
	}
}

func TestDeleteRemovesOneKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := Save("XAI_API_KEY", "keep-me"); err != nil {
		t.Fatal(err)
	}
	if err := Save("ABOX_MCP_GITHUB_TOKEN", "drop-me"); err != nil {
		t.Fatal(err)
	}
	if err := Delete("ABOX_MCP_GITHUB_TOKEN"); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got["XAI_API_KEY"] != "keep-me" {
		t.Fatalf("kept %#v", got)
	}
	if _, ok := got["ABOX_MCP_GITHUB_TOKEN"]; ok {
		t.Fatalf("deleted key still present: %#v", got)
	}
}

func TestDeleteMissingIsNoop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := Delete("XAI_API_KEY"); err != nil {
		t.Fatal(err)
	}
	if err := Save("XAI_API_KEY", "keep-me"); err != nil {
		t.Fatal(err)
	}
	if err := Delete("OPENAI_API_KEY"); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got["XAI_API_KEY"] != "keep-me" {
		t.Fatalf("got %#v", got)
	}
}
