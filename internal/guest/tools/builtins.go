package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AdminTurnedDevOps/ABox/protocol"
)

const (
	ListFiles  = "list_files"
	ReadFile   = "read_file"
	Search     = "search"
	ApplyPatch = "apply_patch"
	RunCommand = "run_command"

	AgentListDepth   = 6
	AgentListLimit   = 500
	AgentReadMax     = 64 << 10
	AgentSearchPath  = "."
	AgentSearchLimit = 40
)

type Spec struct {
	Name        string
	Description string
	Parameters  map[string]any
}

func BuiltinSpecs() []Spec {
	obj := func(props map[string]any) map[string]any {
		return map[string]any{"type": "object", "properties": props}
	}
	return []Spec{
		{Name: ListFiles, Description: "List paths in the guest repository", Parameters: obj(map[string]any{
			"path": map[string]any{"type": "string"},
		})},
		{Name: ReadFile, Description: "Read a file in the guest repository", Parameters: obj(map[string]any{
			"path": map[string]any{"type": "string"},
		})},
		{Name: Search, Description: "Search the guest repository", Parameters: obj(map[string]any{
			"query": map[string]any{"type": "string"},
		})},
		{Name: ApplyPatch, Description: "Apply a unified diff in the guest repository", Parameters: obj(map[string]any{
			"patch": map[string]any{"type": "string"},
		})},
		{Name: RunCommand, Description: "Run a shell command in the guest", Parameters: obj(map[string]any{
			"command": map[string]any{"type": "string"},
		})},
	}
}

func IsBuiltin(name string) bool {
	for _, s := range BuiltinSpecs() {
		if s.Name == name {
			return true
		}
	}
	return false
}

// CallBuiltin runs a host-guest RPC builtin. Empty params are an error.
func (r Repo) CallBuiltin(name string, raw json.RawMessage, timeout time.Duration) (any, error) {
	return r.call(name, raw, timeout, false)
}

// CallTool runs a model-facing builtin. Empty or invalid JSON uses zero params plus agent defaults.
func (r Repo) CallTool(name string, raw json.RawMessage, timeout time.Duration) (any, error) {
	return r.call(name, raw, timeout, true)
}

func (r Repo) call(name string, raw json.RawMessage, timeout time.Duration, agent bool) (any, error) {
	switch name {
	case ListFiles:
		p, err := unmarshalParams[protocol.ListFilesParams](raw, agent)
		if err != nil {
			return nil, err
		}
		if agent {
			if p.Depth == 0 {
				p.Depth = AgentListDepth
			}
			if p.Limit == 0 {
				p.Limit = AgentListLimit
			}
		}
		paths, err := r.List(p.Path, p.Depth, p.Limit)
		if err != nil {
			return nil, err
		}
		return protocol.ListFilesResult{Paths: paths}, nil
	case ReadFile:
		p, err := unmarshalParams[protocol.ReadFileParams](raw, agent)
		if err != nil {
			return nil, err
		}
		if agent && p.MaxBytes == 0 {
			p.MaxBytes = AgentReadMax
		}
		content, bin, trunc, err := r.Read(p.Path, p.MaxBytes)
		if err != nil {
			return nil, err
		}
		return protocol.ReadFileResult{Content: content, Binary: bin, Trunc: trunc}, nil
	case Search:
		p, err := unmarshalParams[protocol.SearchParams](raw, agent)
		if err != nil {
			return nil, err
		}
		if agent {
			if p.Path == "" {
				p.Path = AgentSearchPath
			}
			if p.Limit == 0 {
				p.Limit = AgentSearchLimit
			}
		}
		matches, err := r.Search(p.Query, p.Path, p.Limit)
		if err != nil {
			return nil, err
		}
		return protocol.SearchResult{Matches: matches}, nil
	case ApplyPatch:
		p, err := unmarshalParams[protocol.ApplyPatchParams](raw, agent)
		if err != nil {
			return nil, err
		}
		output, err := r.ApplyPatch(p.Patch)
		return protocol.ApplyPatchResult{OK: err == nil, Output: output}, err
	case RunCommand:
		p, err := unmarshalParams[protocol.RunCommandParams](raw, agent)
		if err != nil {
			return nil, err
		}
		to := timeout
		if to <= 0 && p.Timeout > 0 {
			to = time.Duration(p.Timeout) * time.Second
		}
		exit, stdout, stderr, dur, trunc, err := r.Run(p.Command, p.WorkDir, to, DefaultMaxOutput)
		return protocol.RunCommandResult{
			ExitCode: exit, Stdout: stdout, Stderr: stderr, Duration: dur.String(), Trunc: trunc,
		}, err
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

func FormatToolResult(result any, err error) (string, error) {
	switch v := result.(type) {
	case protocol.ListFilesResult:
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(v.Paths)
		return string(b), nil
	case protocol.ReadFileResult:
		if err != nil {
			return "", err
		}
		if v.Binary {
			return "[binary file]", nil
		}
		return v.Content, nil
	case protocol.SearchResult:
		if err != nil {
			return "", err
		}
		if len(v.Matches) == 0 {
			return "no matches", nil
		}
		b, _ := json.Marshal(v.Matches)
		return string(b), nil
	case protocol.ApplyPatchResult:
		return v.Output, err
	case protocol.RunCommandResult:
		if err != nil && v.ExitCode == -1 {
			return "", err
		}
		return fmt.Sprintf("exit=%d\n%s\n%s", v.ExitCode, v.Stdout, v.Stderr), nil
	default:
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("unknown tool result %T", result)
	}
}

func unmarshalParams[T any](raw json.RawMessage, lenient bool) (T, error) {
	var v T
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		if lenient {
			return v, nil
		}
		return v, fmt.Errorf("empty params")
	}
	err := json.Unmarshal(raw, &v)
	if err != nil && lenient {
		var zero T
		return zero, nil
	}
	return v, err
}
