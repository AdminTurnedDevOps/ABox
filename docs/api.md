---
layout: default
title: API
nav_order: 8
permalink: /api/
---

# API
{: .no_toc }

Package: `github.com/AdminTurnedDevOps/ABox/pkg/abox`

This is the only public Go API. [Examples]({{ '/examples' | relative_url }})
are sample `main` programs that call these methods — not extra packages.

1. TOC
{:toc}

## Open / Resume / Close

```go
func Open(ctx context.Context, opts Options) (*Session, error)
func Resume(ctx context.Context, sessionID string, opts Options) (*Session, error)
func (s *Session) Close() error
```

`Resume` requires a non-empty session id. Session selection is independent of
`opts.RepoPath`, the current directory, and host Git state.

Both require a protocol-4 guest. Older disks return `ErrGuestTooOld`.
`Open` also scrubs leftover plaintext secrets out of `~/.abox/sessions`
before boot.

Always `defer sess.Close()`. `Close` stops the VM and the host broker.

## Options

| Field | Type | Default |
| --- | --- | --- |
| `RepoPath` | `string` | cwd; exact source directory snapshotted by `Open` |
| `Model` | `string` | first profile in `config.yaml` |
| `Image` | `string` | config / `~/.abox/images/abox-guest.raw` |
| `VMMPath` | `string` | config or `abox-vmm` on `PATH` |
| `VCPU`, `RAMMiB` | `int` | `0` = config resolved (1 / 768) |
| `BootTimeout` | `time.Duration` | 45s |

Home directory is `~/.abox` unless `ABOX_HOME` is set.

## Session

| Method | Notes |
| --- | --- |
| `ID() string` | Session directory name |
| `Capabilities() Capabilities` | Protocol plus feature flags |
| `History() []protocol.HistoryLine` | From guest hello |
| `Turn(ctx, prompt, onEvent) (*TurnResult, error)` | Rich events on |
| `TurnOpts(ctx, prompt, opts, onEvent)` | `MaxTurns`, `Timeout`, `RichEvents` |
| `SetModel(ctx, profile)` | Name in `config.yaml`; host broker follows |
| `SetMCPTokens(ctx, map[string]string)` | Host MCP broker override (not guest env) |
| `SetApprover(Approver) error` | Model-authored `run_command`; nil = deny |
| `ExportPatch(ctx) (patch, summary string, err error)` | Guest `git diff` vs baseline |
| `ListFiles(ctx, path, depth, limit)` | Same as `--probe-vm` |
| `ReadFile(ctx, path, maxBytes)` | `ReadFileResult` |
| `RunCommand(ctx, command, timeoutSec)` | Supervisor RPC; **not** the approval gate |

`RunCommand` is a host-initiated builtin. Model `run_command` during `Turn`
goes through [approvals]({{ '/approvals' | relative_url }}).

## Capabilities

```go
type Capabilities struct {
    Protocol    int
    Cancel      bool  // protocol >= 2
    RichEvents  bool  // protocol >= 2
    TurnOptions bool  // protocol >= 2
    Approvals   bool  // protocol >= 4
    MCPBroker   bool  // protocol >= 4
}
```

A session returned by `Open` / `Resume` has protocol 4 and every flag true.

## Approvals

```go
type ApprovalDecision uint8 // ApprovalDeny, ApprovalAllowOnce

type ApprovalRequest struct {
    Tool       string // "run_command"
    ToolID     string
    Command    string
    WorkDir    string
    TimeoutSec int
}

type Approver interface {
    Approve(context.Context, ApprovalRequest) (ApprovalDecision, error)
}

func (s *Session) SetApprover(approver Approver) error
```

`ApproverFunc` adapts a function. Anything other than `ApprovalAllowOnce` is
treated as deny. No approver (the default, including `abox exec`) is deny.
Cannot change the approver during a turn.

See [Approvals]({{ '/approvals' | relative_url }}) and the
[approvals example]({{ '/examples/approvals' | relative_url }}).

## TurnOpts / TurnResult

```go
type TurnOpts struct {
    MaxTurns   int
    Timeout    time.Duration
    RichEvents bool
}

type TurnResult struct {
    Usage      *protocol.UsageInfo
    StopReason string
    Canceled   bool
}
```

On protocol 2+, `Turn` sets `RichEvents` so usage can populate. xAI may leave
`Usage` nil. Default max turns inside the guest is 16 when `MaxTurns` is 0.

## Event

Alias of `protocol.AgentEvent`:

| Field | When |
| --- | --- |
| `Kind` | `text`, `tool`, `result`, `error`, `done` |
| `Text` | Token stream or tool snippet |
| `Tool`, `Status`, `Err` | Tool finished |
| `ToolID`, `ToolArgs` | Rich events |
| `Usage`, `StopReason` | Kind `result` |

Approval prompts are **not** events. They are a host RPC during the turn.

## Errors

| Error | Meaning |
| --- | --- |
| `ErrGuestTooOld` | Guest below protocol 4 (or a v2-only call against a v1 sandbox) |
| `"guest image missing … (run: make image)"` | No golden `.raw` |
| `"abox-vmm not found"` | Not on `PATH` |
| `"no model profile"` | Unknown `Options.Model` |
| `"cannot set approver while a turn…"` | `SetApprover` during an in-flight turn |
| `"offline mode: provider access is disabled"` | `connectivity.mode: offline` |
| frame `code: canceled` | Turn aborted via `ctx` |

Wraps with `fmt.Errorf("…: %w", err)` like the rest of the repo.
