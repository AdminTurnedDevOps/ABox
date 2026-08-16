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
