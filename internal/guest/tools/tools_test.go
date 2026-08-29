package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AdminTurnedDevOps/ABox/protocol"
)

func TestBuiltinSpecsCount(t *testing.T) {
	if n := len(BuiltinSpecs()); n != 5 {
		t.Fatalf("want 5 builtins, got %d", n)
	}
	for _, s := range BuiltinSpecs() {
		if !IsBuiltin(s.Name) {
			t.Fatalf("%q not IsBuiltin", s.Name)
		}
	}
	if IsBuiltin("host_shell") {
		t.Fatal("host_shell must not be builtin")
	}
}

func TestCallBuiltinAndTool(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Repo{Root: dir}
	raw, _ := json.Marshal(protocol.ListFilesParams{Path: ".", Depth: 4, Limit: 50})
	got, err := r.CallBuiltin(ListFiles, raw, 0)
	if err != nil {
		t.Fatal(err)
	}
	res := got.(protocol.ListFilesResult)
	if len(res.Paths) != 1 || res.Paths[0] != "a.txt" {
		t.Fatalf("%#v", res)
	}
	if _, err := r.CallBuiltin(ListFiles, nil, 0); err == nil {
		t.Fatal("empty rpc params should fail")
	}
	out, err := FormatToolResult(r.CallTool(ListFiles, []byte(`{"path":"."}`), 0))
	if err != nil {
		t.Fatal(err)
	}
	if out != `["a.txt"]` {
		t.Fatalf("tool list %q", out)
	}
	text, err := FormatToolResult(r.CallTool(ReadFile, []byte(`{"path":"a.txt"}`), 0))
	if err != nil || text != "hello" {
		t.Fatalf("read %q err=%v", text, err)
	}
	none, err := FormatToolResult(r.CallTool(Search, []byte(`{"query":"zzz"}`), 0))
	if err != nil || none != "no matches" {
		t.Fatalf("search %q err=%v", none, err)
	}
}

func TestRejectTraversal(t *testing.T) {
	dir := t.TempDir()
	r := Repo{Root: dir}
	if _, err := r.Resolve("../etc/passwd"); err == nil {
		t.Fatal("expected rejection")
	}
	if _, err := r.Resolve("/etc/passwd"); err == nil {
		t.Fatal("expected rejection")
	}
}

func TestListAndRead(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644)
	r := Repo{Root: dir}
	paths, err := r.List(".", 4, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "a.txt" {
		t.Fatalf("paths=%v", paths)
	}
	content, bin, _, err := r.Read("a.txt", 100)
	if err != nil || bin || content != "hello" {
		t.Fatalf("read %q bin=%v err=%v", content, bin, err)
	}
}
