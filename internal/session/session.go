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
	ID         string    `json:"id"`
	Capability string    `json:"capability"`
	Created    time.Time `json:"created"`
	RepoRoot   string    `json:"repo_root"`
	HEAD       string    `json:"head"`
	Dir        string    `json:"dir"`
}

func Create(repoRoot, head string) (*Session, error) {
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
		RepoRoot:   repoRoot,
		HEAD:       head,
		Dir:        dir,
	}
	if err := s.WriteMeta(); err != nil {
		return nil, err
	}
	return s, nil
}

func Load(id string) (*Session, error) {
	if id == "" {
		return nil, fmt.Errorf("empty session id")
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

// LatestForRepo returns the newest session whose RepoRoot matches any of roots
// and that still has a root.raw disk.
func LatestForRepo(roots ...string) (*Session, error) {
	want := map[string]struct{}{}
	for _, r := range roots {
		if r == "" {
			continue
		}
		abs, err := filepath.Abs(r)
		if err != nil {
			abs = filepath.Clean(r)
		}
		want[abs] = struct{}{}
		want[filepath.Clean(r)] = struct{}{}
	}
	if len(want) == 0 {
		return nil, fmt.Errorf("no repository root to match")
	}
	entries, err := os.ReadDir(config.SessionRoot())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no sessions to resume")
		}
		return nil, err
	}
	var best *Session
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s, err := Load(e.Name())
		if err != nil {
			continue
		}
		root := s.RepoRoot
		if abs, err := filepath.Abs(root); err == nil {
			root = abs
		}
		if _, ok := want[root]; !ok {
			if _, ok := want[filepath.Clean(s.RepoRoot)]; !ok {
				continue
			}
		}
		if best == nil || s.Created.After(best.Created) {
			best = s
		}
	}
	if best == nil {
		return nil, fmt.Errorf("no session to resume for this repository")
	}
	return best, nil
}

func (s *Session) WriteMeta() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.Dir, "session.json"), data, 0o600)
}

func (s *Session) RPCSocket() string  { return filepath.Join(s.Dir, "rpc.sock") }
func (s *Session) RootDisk() string   { return filepath.Join(s.Dir, "root.raw") }
func (s *Session) ConfigDisk() string { return filepath.Join(s.Dir, "config.raw") }
func (s *Session) ConsoleLog() string { return filepath.Join(s.Dir, "console.log") }
func (s *Session) GuestConfigJSON() string {
	return filepath.Join(s.Dir, "guest-config.json")
}

func (s *Session) WriteGuestConfig(model config.Model, secrets map[string]string, servers []config.MCPServer) error {
	var gs []protocol.GuestMCPServer
	for _, srv := range servers {
		gs = append(gs, protocol.GuestMCPServer{
			Name:      srv.Name,
			URL:       srv.URL,
			TokenEnv:  config.TokenEnv(srv),
			Allowlist: srv.ToolAllowlist,
		})
	}
	cfg := protocol.GuestConfig{
		SessionID:  s.ID,
		Capability: s.Capability,
		VsockPort:  protocol.RPCPort,
		RepoDir:    "/work/repo",
		Model: protocol.GuestModel{
			Name:          model.Name,
			Provider:      model.Provider,
			Model:         model.Model,
			CredentialEnv: model.CredentialEnv,
			BaseURL:       model.BaseURL,
		},
		Secrets:    secrets,
		MCPServers: gs,
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.GuestConfigJSON(), data, 0o600)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return hex.EncodeToString(b), nil
}
