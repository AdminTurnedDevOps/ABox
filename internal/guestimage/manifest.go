package guestimage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const Schema = 1

var ErrManifestMissing = errors.New("guest image manifest missing")

type Manifest struct {
	Schema   int    `json:"schema"`
	Arch     string `json:"arch"`
	ImageID  string `json:"image_id"`
	Protocol int    `json:"protocol"`
	SHA256   string `json:"sha256"`
}

type Image struct {
	Path     string
	Manifest Manifest
}

func Load(path string) (Image, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return Image{}, err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return Image{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return Image{}, err
	}
	if !info.Mode().IsRegular() {
		return Image{}, fmt.Errorf("guest image is not a regular file: %s", resolved)
	}

	manifestPath := resolved + ".manifest.json"
	f, err := openManifest(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Image{}, fmt.Errorf("%w: %s", ErrManifestMissing, manifestPath)
		}
		return Image{}, fmt.Errorf("read guest image manifest: %w", err)
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return Image{}, fmt.Errorf("stat guest image manifest: %w", err)
	}
	if info.Size() > 64<<10 {
		return Image{}, fmt.Errorf("guest image manifest is too large")
	}
	if !info.Mode().IsRegular() {
		return Image{}, fmt.Errorf("guest image manifest is not a regular file")
	}
	dec := json.NewDecoder(io.LimitReader(f, 64<<10))
	dec.DisallowUnknownFields()
	var manifest Manifest
	if err := dec.Decode(&manifest); err != nil {
		return Image{}, fmt.Errorf("decode guest image manifest: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return Image{}, fmt.Errorf("decode guest image manifest: trailing data")
	}
	if err := manifest.Validate(); err != nil {
		return Image{}, err
	}
	return Image{Path: resolved, Manifest: manifest}, nil
}

func (m Manifest) Validate() error {
	if m.Schema != Schema {
		return fmt.Errorf("unsupported guest image manifest schema %d", m.Schema)
	}
	if m.Arch != "amd64" && m.Arch != "arm64" {
		return fmt.Errorf("unsupported guest image architecture %q", m.Arch)
	}
	if strings.TrimSpace(m.ImageID) == "" {
		return fmt.Errorf("guest image manifest has an empty image_id")
	}
	if m.Protocol <= 0 {
		return fmt.Errorf("guest image manifest has invalid protocol %d", m.Protocol)
	}
	digest, err := hex.DecodeString(m.SHA256)
	if err != nil || len(digest) != sha256.Size || strings.ToLower(m.SHA256) != m.SHA256 {
		return fmt.Errorf("guest image manifest has invalid sha256")
	}
	return nil
}

func Digest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
