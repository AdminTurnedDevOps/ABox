---
layout: default
title: Examples
nav_order: 12
has_children: true
permalink: /examples/
---

# Examples

There is **one** Go SDK: [`github.com/AdminTurnedDevOps/ABox/pkg/abox`]({{ '/api' | relative_url }}).
`Open` a session, then call methods on it — `Turn`, `RunCommand`, `ListFiles`,
cancel via `context`, `SetApprover`, and so on. You do not add a package per feature.

```bash
go get github.com/AdminTurnedDevOps/ABox@latest
```

```go
import "github.com/AdminTurnedDevOps/ABox/pkg/abox"

sess, err := abox.Open(ctx, abox.Options{})
// sess.Turn(...)
// sess.SetApprover(...)
// sess.RunCommand(ctx, "uname -a", 15)
// sess.ListFiles(ctx, ".", 4, 50)
```

Each child page has the matching `abox` CLI (or `config.yaml`) and a sample
`main` that calls one SDK method. Copy the Go into your program. `abox-vmm`
on `PATH` (the `darwin_arm64` archive on the
[GitHub release](https://github.com/AdminTurnedDevOps/ABox/releases)). Golden
image and provider key: [Quickstart]({{ '/quickstart' | relative_url }}).
Probe methods do not need a key. A successful `Open` speaks protocol 4.

The CLI is `abox`, `abox --resume`, `abox --model`, `abox --probe-vm`,
`abox exec --prompt`, `abox mcp add` / `mcp login`, and `abox creds migrate`.
There are no extra flags for `ReadFile`, `RunCommand`, `ExportPatch`,
`MaxTurns`, or live `SetMCPTokens` — those stay on the SDK page as
SDK-only. Full command list: [CLI and TUI]({{ '/cli' | relative_url }}).

| Call | Sample | CLI |
| --- | --- | --- |
| `Open` + `Turn` | [Basic turn]({{ '/examples/basic' | relative_url }}) | `abox`, `abox exec --prompt` |
| `Resume` | [Resume]({{ '/examples/resume' | relative_url }}) | `abox --resume` |
| `Turn` + canceled `ctx` | [Cancel]({{ '/examples/cancel' | relative_url }}) | Ctrl+C |
| `TurnOpts` | [Turn options]({{ '/examples/turn-opts' | relative_url }}) | SDK-only (`MaxTurns` / `Timeout`) |
| `ListFiles` | [List files]({{ '/examples/list-files' | relative_url }}) | `abox --probe-vm` |
| `ReadFile` | [Read file]({{ '/examples/read-file' | relative_url }}) | SDK-only |
| `RunCommand` | [Run command]({{ '/examples/run-command' | relative_url }}) | SDK-only |
| `SetApprover` | [Approvals]({{ '/examples/approvals' | relative_url }}) | TUI prompt; `exec` denies |
| `ExportPatch` | [Export patch]({{ '/examples/export-patch' | relative_url }}) | SDK-only |
| `SetModel` | [Set model]({{ '/examples/set-model' | relative_url }}) | `abox --model` |
| `Turn` events | [Print events]({{ '/examples/print-events' | relative_url }}) | `abox exec --prompt` |
| `Capabilities` | [Capabilities]({{ '/examples/capabilities' | relative_url }}) | protocol 4 required |
| `Options` | [Custom VM]({{ '/examples/custom-vm' | relative_url }}) | `~/.abox/config.yaml` |
| `ErrGuestTooOld` | [Errors]({{ '/examples/errors' | relative_url }}) | `abox --resume` of an old disk |
| `History` | [History]({{ '/examples/history' | relative_url }}) | `abox --resume` |
| `Turn` twice | [Multi-turn]({{ '/examples/multi-turn' | relative_url }}) | `abox` (TUI) |
| `SetMCPTokens` | [MCP tokens]({{ '/examples/mcp-tokens' | relative_url }}) | `abox mcp add` / `mcp login` |
