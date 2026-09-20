package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

type Session struct {
	ID               string    `json:"id"`
	Capability       string    `json:"capability"`
	Created          time.Time `json:"created"`
	SourceDir        string    `json:"source_dir,omitempty"`
	RepoRoot         string    `json:"repo_root,omitempty"`
	HEAD             string    `json:"head,omitempty"`
	Dir              string    `json:"dir"`
	ManifestSchema   int       `json:"manifest_schema,omitempty"`
	GuestArch        string    `json:"guest_arch,omitempty"`
	ImageID          string    `json:"image_id,omitempty"`
	ImageSHA256      string    `json:"image_sha256,omitempty"`
	GuestProtocol    int       `json:"guest_protocol,omitempty"`
	VMMBackend       string    `json:"vmm_backend,omitempty"`
	HelperPID        int       `json:"helper_pid,omitempty"`
	HelperStartID    string    `json:"helper_start_id,omitempty"`
	HelperExecutable string    `json:"helper_executable,omitempty"`
	DiagnosticProbe  bool      `json:"-"`
	runtimeLock      *os.File
}

func Create(sourceDir string) (*Session, error) {
	id, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	cap, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(config.SessionRoot(), id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Session{
		ID:         id,
		Capability: cap,
		Created:    time.Now().UTC(),
		SourceDir:  sourceDir,
		Dir:        dir,
	}
	if err := s.WriteMeta(); err != nil {
		return nil, err
	}
	return s, nil
}

func Load(id string) (*Session, error) {
	if !validID(id) {
		return nil, fmt.Errorf("invalid session id %q", id)
	}
	dir := filepath.Join(config.SessionRoot(), id)
	data, err := os.ReadFile(filepath.Join(dir, "session.json"))
	if err != nil {
		return nil, fmt.Errorf("session %s: %w", id, err)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("session %s: %w", id, err)
	}
	s.Dir = dir
	s.ID = id
	if _, err := os.Stat(s.RootDisk()); err != nil {
		return nil, fmt.Errorf("session %s: missing root.raw", id)
	}
	return &s, nil
}

func (s *Session) WriteMeta() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.Dir, "session.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(path, 0o600)
}

func (s *Session) RPCSocket() string  { return filepath.Join(s.Dir, "rpc.sock") }
func (s *Session) RootDisk() string   { return filepath.Join(s.Dir, "root.raw") }
func (s *Session) ConfigDisk() string { return filepath.Join(s.Dir, "config.raw") }
func (s *Session) ConsoleLog() string { return filepath.Join(s.Dir, "console.log") }
func (s *Session) GuestConfigJSON() string {
	return filepath.Join(s.Dir, "guest-config.json")
}

func (s *Session) TranscriptPath() string {
	return filepath.Join(s.Dir, "transcript.json")
}

func ReadTranscript(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var lines []string
	if err := json.Unmarshal(data, &lines); err != nil {
		return nil, err
	}
	return lines, nil
}

func WriteTranscript(path string, lines []string) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(lines, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (s *Session) WriteGuestConfig(model config.Model) error {
	cfg := protocol.GuestConfig{
		SessionID:  s.ID,
		Capability: s.Capability,
		VsockPort:  protocol.RPCPort,
		RepoDir:    protocol.GuestRepoDir,
		Model:      model.ToGuest(),
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.GuestConfigJSON(), data, 0o600)
}

// ConfigDiskSize is the fixed size of the sealed read-only config disk.
const ConfigDiskSize = 1 << 20

// WritePaddedConfig writes data to path zero-padded to ConfigDiskSize bytes
// and leaves the file read-only (mode 0400). It is the shared writer for the
// config.raw disk: creation, resume rewrite, and secret scrubbing all keep
// the same layout the guest parses (JSON, then NUL padding).
func WritePaddedConfig(path string, data []byte) error {
	if len(data) > ConfigDiskSize {
		return fmt.Errorf("config disk payload too large: %d", len(data))
	}
	// Resume rewrites config.raw; the previous run left it mode 0400.
	_ = os.Chmod(path, 0o600)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if _, err := f.Write(make([]byte, ConfigDiskSize-len(data))); err != nil {
		return err
	}
	return os.Chmod(path, 0o400)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
