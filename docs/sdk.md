---
layout: page
title: Go SDK
permalink: /sdk/
---

Public package: [`github.com/AdminTurnedDevOps/ABox/pkg/abox`](https://pkg.go.dev/github.com/AdminTurnedDevOps/ABox/pkg/abox).

The CLI already orchestrates `config` → session → repo snapshot →
`runtime.Prepare` / `Start` → `UserTurn`. The SDK is that path as a library.
`cmd/abox` is unchanged; it does not import `pkg/abox`.

## Requirements

- Apple Silicon Mac (Hypervisor.framework)
- Homebrew `libkrun` and `libkrunfw` (guest **kernel** is libkrunfw, not the disk)
- Golden image: `make image` (Docker packer today)
- Provider key in `~/.abox` (`/provider` or `credentials.env`)

Missing image fails with the same message as the CLI: `run: make image`.

## Install

```bash
go get github.com/AdminTurnedDevOps/ABox/pkg/abox
```

Module path matches the repo. `abox-vmm` must be on `PATH` (or set `Options.VMMPath`).

## Quickstart

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "os/signal"

    "github.com/AdminTurnedDevOps/ABox/pkg/abox"
)

func main() {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
    defer stop()

    sess, err := abox.Open(ctx, abox.Options{})
    if err != nil {
        log.Fatal(err)
    }
    defer sess.Close()

    _, err = sess.Turn(ctx, "List the repo files", func(ev abox.Event) {
        if ev.Kind == "text" {
            fmt.Print(ev.Text)
        }
    })
    if err != nil {
        log.Fatal(err)
    }
}
```

Full sample: [`examples/sdk-basic`](https://github.com/AdminTurnedDevOps/ABox/tree/main/examples/sdk-basic).

## Open and Resume

| Func | Behavior |
| --- | --- |
| `Open(ctx, Options)` | New session: snapshot the repo, clone the golden disk, boot, transfer HEAD as tar |
| `Resume(ctx, sessionID, Options)` | Boot existing `root.raw`. Empty `sessionID` = latest session for that repo |

`Close()` stops the VM (`Sandbox.Stop`). Always defer it.

### `Options`

| Field | Default |
| --- | --- |
| `RepoPath` | cwd |
| `Model` | first profile in `config.yaml` (`config.ModelNamed("")`) |
| `Image` | `cfg.Runtime.Image` or `~/.abox/images/abox-guest.raw` |
| `VMMPath` | `cfg.Runtime.VMMPath` or `abox-vmm` on `PATH` |
| `VCPU`, `RAMMiB` | `0` = config resolved resources (1 / 768 if unset) |
| `BootTimeout` | 45s |

## Turn

`Turn` streams `Event` callbacks (same JSON as CLI `agent_event` frames). On a
protocol 2 guest it also requests rich events so `TurnResult` can carry usage.

```go
res, err := sess.Turn(ctx, prompt, func(ev abox.Event) { ... })
```

Cancel the context to abort a protocol 2 turn (`cancel_turn` RPC). An in-flight
`run_command` is killed. List/read/search finish on their own (bounded).

`TurnOpts` for limits (protocol 2 only):

```go
sess.TurnOpts(ctx, prompt, abox.TurnOpts{
    MaxTurns:   8,
    Timeout:    2 * time.Minute,
    RichEvents: true,
}, onEvent)
```

`TurnResult`: `Usage` (best-effort token counts), `StopReason`, `Canceled`.
xAI may leave `Usage` nil (`stream_options` is omitted for that provider).

### Event kinds

| `Kind` | Meaning |
| --- | --- |
| `text` | Model token stream |
| `tool` | Builtin or MCP tool finished (`Tool`, `Status`, `Text` / `Err`) |
| `result` | End-of-turn metadata when rich events are on (`Usage`, `StopReason`) |
| `error` | Agent or provider error |
| `done` | Turn completed |

On protocol 2 with rich events, `tool` may also set `ToolID` and `ToolArgs`.

## Other session methods

| Method | RPC |
| --- | --- |
| `ID()` | Session directory name |
| `History()` | Lines from guest hello |
| `Capabilities()` | Guest protocol + cancel / rich / turn-options flags |
| `SetModel(ctx, profile)` | `set_model` (name in `config.yaml`) |
| `SetMCPTokens(ctx, secrets)` | `set_mcp_tokens` |
| `ExportPatch(ctx)` | Guest `git diff` vs baseline |
| `ListFiles` / `ReadFile` / `RunCommand` | Same builtins as `--probe-vm` |

## Protocol v1 vs v2

Host/guest RPC version is **not** the GitHub release tag. Current guest sends
`protocol.Version = 2` in hello.

`Resume` (and CLI `--resume`) boots the **session** `root.raw`, cloned when that
session was created. `make image-update` only patches the **golden** image.

| Guest | What works |
| --- | --- |
| v1 (disk from before the v2 guest rebuild) | Plain `Turn` (no extra options). Cancel and `TurnOpts` return `ErrGuestTooOld`. |
| v2 (`make build && make image-update`, then a **new** session) | Rich events, usage, `TurnOpts`, mid-turn cancel |

`Capabilities()` is the check. Fresh installs after a current `make image` never
see v1.

## Isolation

The SDK boots the same microVM as the CLI. Do not describe this as verified
isolation. Claims stay Planned until the hardware tests in `PLAN.md` pass.
