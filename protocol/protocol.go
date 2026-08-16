// Package protocol is the versioned host/guest RPC contract.
package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

const (
	Version         = 1
	MaxFrameBytes   = 1 << 20
	MaxArchiveChunk = 256 << 10
	RPCPort         = 1024
)

// Frame is a length-prefixed JSON message.
type Frame struct {
	V      int             `json:"v"`
	ID     string          `json:"id"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

type HelloParams struct {
	SessionID  string `json:"session_id"`
	Capability string `json:"capability"`
	ImageID    string `json:"image_id"`
	Protocol   int    `json:"protocol"`
	GuestReady bool   `json:"guest_ready"`
}

type HelloResult struct {
	Accepted bool   `json:"accepted"`
	Message  string `json:"message,omitempty"`
}

type ListFilesParams struct {
	Path  string `json:"path"`
	Depth int    `json:"depth"`
	Limit int    `json:"limit"`
}

type ListFilesResult struct {
	Paths []string `json:"paths"`
}

type ReadFileParams struct {
	Path      string `json:"path"`
	MaxBytes  int    `json:"max_bytes"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

type ReadFileResult struct {
	Content string `json:"content"`
	Binary  bool   `json:"binary"`
	Trunc   bool   `json:"truncated"`
}

type SearchParams struct {
	Query   string `json:"query"`
	Path    string `json:"path"`
	IsRegex bool   `json:"is_regex"`
	Limit   int    `json:"limit"`
}

type SearchResult struct {
	Matches []string `json:"matches"`
}

type ApplyPatchParams struct {
	Patch string `json:"patch"`
}

type ApplyPatchResult struct {
	OK     bool   `json:"ok"`
	Output string `json:"output"`
}

type RunCommandParams struct {
	Command string `json:"command"`
	WorkDir string `json:"workdir,omitempty"`
	Timeout int    `json:"timeout_sec"`
}

type RunCommandResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Duration string `json:"duration"`
	Trunc    bool   `json:"truncated"`
}

type ArchiveChunkParams struct {
	Offset int64  `json:"offset"`
	Last   bool   `json:"last"`
	Data   []byte `json:"data"`
}

type ArchiveChunkResult struct {
	Written int64 `json:"written"`
}

type ExportPatchResult struct {
	Patch   string `json:"patch"`
	Summary string `json:"summary"`
}

type QuiesceResult struct {
	Frozen bool `json:"frozen"`
}

type SetTimeParams struct {
	UnixMicro int64 `json:"unix_micro"`
}

type GuestConfig struct {
	SessionID  string            `json:"session_id"`
	Capability string            `json:"capability"`
	VsockPort  uint32            `json:"vsock_port"`
	RepoDir    string            `json:"repo_dir"`
	Model      GuestModel        `json:"model"`
	Secrets    map[string]string `json:"secrets,omitempty"`
}

type GuestModel struct {
	Name          string `json:"name"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	CredentialEnv string `json:"credential_env"`
	BaseURL       string `json:"base_url,omitempty"`
}

type UserTurnParams struct {
	Text string `json:"text"`
}

type AgentEvent struct {
	Kind   string `json:"kind"`
	Text   string `json:"text,omitempty"`
	Tool   string `json:"tool,omitempty"`
	Status string `json:"status,omitempty"`
	Err    string `json:"err,omitempty"`
}

type SetModelParams struct {
	Model   GuestModel        `json:"model"`
	Secrets map[string]string `json:"secrets,omitempty"`
}

func WriteFrame(w io.Writer, f Frame) error {
	if f.V == 0 {
		f.V = Version
	}
	body, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if len(body) > MaxFrameBytes {
		return fmt.Errorf("frame too large: %d", len(body))
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(body)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

func ReadFrame(r io.Reader) (Frame, error) {
	return ReadFrameLimit(r, MaxFrameBytes)
}

func ReadFrameLimit(r io.Reader, limit int) (Frame, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Frame{}, err
	}
	n := int(binary.BigEndian.Uint32(hdr[:]))
	if n <= 0 || n > limit {
		return Frame{}, fmt.Errorf("invalid frame size %d", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return Frame{}, err
	}
	var f Frame
	if err := json.Unmarshal(body, &f); err != nil {
		return Frame{}, err
	}
	return f, nil
}

func EncodeParams(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func DecodeParams[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 {
		return v, fmt.Errorf("empty params")
	}
	err := json.Unmarshal(raw, &v)
	return v, err
}

const DefaultRPCTimeout = 60 * time.Second
