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
	Version = 4 // host provider/MCP brokers and model-command approval

	MaxFrameBytes   = 1 << 20
	MaxArchiveChunk = 256 << 10
	MaxHistoryBytes = 256 << 10
	RPCPort         = 1024
	GuestRepoDir    = "/work/repo"

	MaxProviderChunk    = 256 << 10 // provider_send chunk size
	MaxProviderRequest  = 4 << 20   // reassembled provider_send budget
	MaxProviderToolArgs = 512 << 10 // per tool-args bound; larger -> error event
	MaxProviderEvent    = 512 << 10 // encoded provider_event params budget
	MaxProviderMessages = 4096      // messages in one provider request
	MaxProviderTools    = 256       // tool schemas in one provider request
	MaxProviderEvents   = 1 << 20   // events in one provider stream
	MaxProviderStreams  = 2         // concurrent provider streams per session
	MaxGuestCalls       = 8         // concurrent guest-initiated host RPCs

	MaxModelCommandBytes = 16 << 10
	MaxMCPTools          = MaxProviderTools - 5
	MaxMCPSchemaBytes    = 64 << 10
	MaxMCPArgsBytes      = 512 << 10
	MaxMCPResultBytes    = 512 << 10
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
	SessionID  string        `json:"session_id"`
	Capability string        `json:"capability"`
	ImageID    string        `json:"image_id"`
	Protocol   int           `json:"protocol"`
	GuestReady bool          `json:"guest_ready"`
	History    []HistoryLine `json:"history,omitempty"`
}

// AgentEvent is a live or stored agent output line.
type AgentEvent struct {
	Kind       string     `json:"kind"`
	Text       string     `json:"text,omitempty"`
	Tool       string     `json:"tool,omitempty"`
	Status     string     `json:"status,omitempty"`
	Err        string     `json:"err,omitempty"`
	ToolID     string     `json:"tool_id,omitempty"`
	ToolArgs   string     `json:"tool_args,omitempty"`
	Usage      *UsageInfo `json:"usage,omitempty"`
	StopReason string     `json:"stop_reason,omitempty"`
}

type UsageInfo struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
}

// HistoryLine is persisted AgentEvent JSON (hello, get_context, transcripts).
type HistoryLine = AgentEvent

type GetContextResult struct {
	History []HistoryLine `json:"history"`
}

type HelloResult struct {
	Accepted bool   `json:"accepted"`
	Message  string `json:"message,omitempty"`
	Protocol int    `json:"protocol,omitempty"` // 0 if omitted; old hosts hang without this
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

type ApprovalDecision string

const (
	ApprovalDeny      ApprovalDecision = "deny"
	ApprovalAllowOnce ApprovalDecision = "allow_once"
)

type RunCommandApprovalParams struct {
	TurnID     string `json:"turn_id"`
	ToolID     string `json:"tool_id,omitempty"`
	Command    string `json:"command"`
	WorkDir    string `json:"workdir,omitempty"`
	TimeoutSec int    `json:"timeout_sec,omitempty"`
}

type RunCommandApprovalResult struct {
	Decision ApprovalDecision `json:"decision"`
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
	Secrets    map[string]string `json:"secrets,omitempty"` // deprecated; kept so old images still parse
	MCPServers []GuestMCPServer  `json:"mcp_servers,omitempty"`
}

type GuestMCPServer struct {
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	TokenEnv  string   `json:"token_env,omitempty"`
	Allowlist []string `json:"tool_allowlist,omitempty"`
}

type GuestModel struct {
	Name          string `json:"name"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	CredentialEnv string `json:"credential_env"`
	BaseURL       string `json:"base_url,omitempty"`
}

type UserTurnParams struct {
	Text       string `json:"text"`
	MaxTurns   int    `json:"max_turns,omitempty"`
	TimeoutSec int    `json:"timeout_sec,omitempty"`
	RichEvents bool   `json:"rich_events,omitempty"`
}

type CancelTurnParams struct {
	ID string `json:"id"`
}

type SetModelParams struct {
	Model   GuestModel        `json:"model"`
	Secrets map[string]string `json:"secrets,omitempty"`
}

type SetMCPTokensParams struct {
	Secrets map[string]string `json:"secrets"`
}

type MCPTool struct {
	Server      string         `json:"server"`
	Name        string         `json:"name"`
	Prefixed    string         `json:"prefixed"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

type MCPListParams struct{}

type MCPListResult struct {
	Tools []MCPTool `json:"tools"`
}

type MCPCallParams struct {
	CallID    string          `json:"call_id"`
	Server    string          `json:"server"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type MCPCallResult struct {
	Text      string `json:"text,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

type MCPCancelParams struct {
	CallID string `json:"call_id"`
}

// Model is a configured alias, never a URL, header, or credential name.
type ProviderOpenParams struct {
	Model string `json:"model"`
	Rich  bool   `json:"rich,omitempty"`
}

type ProviderOpenResult struct {
	StreamID string `json:"stream_id"`
}

type ProviderSendParams struct {
	StreamID string `json:"stream_id"`
	Data     []byte `json:"data"`
	Last     bool   `json:"last,omitempty"` // host starts the provider call after Last
}

type ProviderRequest struct {
	Messages []ProviderMessage    `json:"messages"`
	Tools    []ProviderToolSchema `json:"tools,omitempty"`
}

type ProviderCancelParams struct {
	StreamID string `json:"stream_id"`
}

type ProviderEventParams struct {
	StreamID   string     `json:"stream_id"`
	Type       string     `json:"type"`
	Text       string     `json:"text,omitempty"`
	ToolID     string     `json:"tool_id,omitempty"`
	ToolName   string     `json:"tool_name,omitempty"`
	ToolArgs   string     `json:"tool_args,omitempty"`
	Usage      *UsageInfo `json:"usage,omitempty"`
	StopReason string     `json:"stop_reason,omitempty"`
	Err        string     `json:"err,omitempty"`
}

type ProviderMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content,omitempty"`
	ToolID     string `json:"tool_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolArgs   string `json:"tool_args,omitempty"`
	ToolResult string `json:"tool_result,omitempty"`
}

type ProviderToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
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
	if err := writeFull(w, hdr[:]); err != nil {
		return err
	}
	return writeFull(w, body)
}

func writeFull(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if n > 0 {
			p = p[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
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

// TrimHistory keeps the newest lines whose JSON size fits in maxBytes.
func TrimHistory(h []HistoryLine, maxBytes int) []HistoryLine {
	if maxBytes < 2 {
		return nil
	}
	out := make([]HistoryLine, 0)
	size := 2 // JSON array brackets.
	for i := len(h) - 1; i >= 0; i-- {
		b, err := json.Marshal(h[i])
		if err != nil {
			continue
		}
		itemSize := len(b)
		if len(out) > 0 {
			itemSize++ // Comma separator.
		}
		if size+itemSize > maxBytes {
			continue
		}
		out = append(out, h[i])
		size += itemSize
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
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

// GuestMethodMinVersion returns the minimum negotiated protocol for a
// guest-initiated host RPC. Unknown methods are rejected by the runtime.
func GuestMethodMinVersion(method string) (int, bool) {
	switch method {
	case "provider_open", "provider_send", "provider_cancel":
		return 3, true
	case "mcp_list", "mcp_call", "mcp_cancel", "request_run_command_approval":
		return 4, true
	default:
		return 0, false
	}
}

func GuestMethodIsCancellation(method string) bool {
	return method == "provider_cancel" || method == "mcp_cancel"
}

const DefaultRPCTimeout = 60 * time.Second
