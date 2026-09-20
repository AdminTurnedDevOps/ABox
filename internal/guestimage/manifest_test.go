package guestimage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadResolvesPointerAndValidatesManifest(t *testing.T) {
	dir := t.TempDir()
	image := filepath.Join(dir, "generation.raw")
	if err := os.WriteFile(image, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest, err := Digest(image)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(Manifest{Schema: Schema, Arch: "amd64", ImageID: "id", Protocol: 4, SHA256: digest})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(image+".manifest.json", data, 0o600); err != nil {
		t.Fatal(err)
	}
	pointer := filepath.Join(dir, "current.raw")
	if err := os.Symlink(filepath.Base(image), pointer); err != nil {
		t.Fatal(err)
	}
	got, err := Load(pointer)
	if err != nil {
		t.Fatal(err)
	}
	expectedPath, err := filepath.EvalSymlinks(image)
	if err != nil {
		t.Fatal(err)
	}
	expectedPath, err = filepath.Abs(expectedPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != expectedPath || got.Manifest.SHA256 != digest {
		t.Fatalf("got %#v", got)
	}
}

func TestManifestValidation(t *testing.T) {
	validDigest := strings.Repeat("a", 64)
	for _, manifest := range []Manifest{
		{Schema: 2, Arch: "amd64", ImageID: "id", Protocol: 4, SHA256: validDigest},
		{Schema: 1, Arch: "386", ImageID: "id", Protocol: 4, SHA256: validDigest},
		{Schema: 1, Arch: "amd64", Protocol: 4, SHA256: validDigest},
		{Schema: 1, Arch: "amd64", ImageID: "id", Protocol: 0, SHA256: validDigest},
		{Schema: 1, Arch: "amd64", ImageID: "id", Protocol: 4, SHA256: "bad"},
	} {
		if err := manifest.Validate(); err == nil {
			t.Fatalf("accepted %#v", manifest)
		}
	}
}
