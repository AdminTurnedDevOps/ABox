---
layout: default
title: API
nav_order: 4
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

`Resume("", opts)` picks the latest session for `opts.RepoPath`.

## Options

| Field | Type | Default |
| --- | --- | --- |
| `RepoPath` | `string` | cwd |
| `Model` | `string` | first profile in `config.yaml` |
| `Image` | `string` | config / `~/.abox/images/abox-guest.raw` |
| `VMMPath` | `string` | config or `abox-vmm` on `PATH` |
| `VCPU`, `RAMMiB` | `int` | `0` = config resolved (1 / 768) |
| `BootTimeout` | `time.Duration` | 45s |

## Session

| Method | Notes |
| --- | --- |
| `ID() string` | Session directory name |
| `Capabilities() Capabilities` | Protocol, Cancel, RichEvents, TurnOptions |
| `History() []protocol.HistoryLine` | From guest hello |
| `Turn(ctx, prompt, onEvent) (*TurnResult, error)` | Rich events on v2 |
| `TurnOpts(ctx, prompt, opts, onEvent)` | `MaxTurns`, `Timeout`, `RichEvents` |
| `SetModel(ctx, profile)` | Name in `config.yaml` |
| `SetMCPTokens(ctx, map[string]string)` | Guest env + MCP reconnect |
| `ExportPatch(ctx) (patch, summary string, err error)` | Guest `git diff` vs baseline |
| `ListFiles(ctx, path, depth, limit)` | Same as `--probe-vm` |
| `ReadFile(ctx, path, maxBytes)` | `ReadFileResult` |
| `RunCommand(ctx, command, timeoutSec)` | `RunCommandResult` |

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

On protocol 2, `Turn` sets `RichEvents` so usage can populate. xAI may leave
`Usage` nil.

v2-only fields (`MaxTurns`, `Timeout`, explicit `RichEvents`) against a v1
guest return `ErrGuestTooOld`.

## Event

Alias of `protocol.AgentEvent`:

| Field | When |
| --- | --- |
| `Kind` | `text`, `tool`, `result`, `error`, `done` |
| `Text` | Token stream or tool snippet |
| `Tool`, `Status`, `Err` | Tool finished |
| `ToolID`, `ToolArgs` | Rich events |
| `Usage`, `StopReason` | Kind `result` |

## Errors

| Error | Meaning |
| --- | --- |
| `ErrGuestTooOld` | Cancel / turn options / forced rich events on a v1 guest |
| `"guest image missing … (run: make image)"` | No golden `.raw` |
| `"abox-vmm not found"` | Not on `PATH` |
| `"no model profile"` | Unknown `Options.Model` |
| frame `code: canceled` | Turn aborted via `ctx` |

Wraps with `fmt.Errorf("…: %w", err)` like the rest of the repo.
