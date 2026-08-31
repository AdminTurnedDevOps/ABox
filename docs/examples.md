---
layout: default
title: Examples
nav_order: 8
has_children: true
permalink: /examples/
---

# Examples

There is **one** Go SDK: [`github.com/AdminTurnedDevOps/ABox/pkg/abox`]({{ '/api' | relative_url }}).
`Open` a session, then call methods on it — `Turn`, `RunCommand`, `ListFiles`,
cancel via `context`, and so on. You do not add a package per feature.

```bash
go get github.com/AdminTurnedDevOps/ABox@latest
```

```go
import "github.com/AdminTurnedDevOps/ABox/pkg/abox"

sess, err := abox.Open(ctx, abox.Options{})
// sess.Turn(...)
// sess.RunCommand(ctx, "uname -a", 15)
// sess.ListFiles(ctx, ".", 4, 50)
```

{: .note }
`examples/sdk-*` in the repo are **sample programs** (`package main`) that call
those methods. `go run …/examples/sdk-run-command@latest` still downloads the
**same** ABox module (cached after the first run). It is not a second SDK.

Each child page is one of those samples, inlined so you can copy it into your
own `main.go`. `abox-vmm` on `PATH` (the `darwin_arm64` archive on the
[GitHub release](https://github.com/AdminTurnedDevOps/ABox/releases)). Golden
image and provider key: [Quickstart]({{ '/quickstart' | relative_url }}).
Probe methods do not need a key.

| Call | Sample |
| --- | --- |
| `Open` + `Turn` | [Basic turn]({{ '/examples/basic' | relative_url }}) |
| `Resume` | [Resume]({{ '/examples/resume' | relative_url }}) |
| `Turn` + canceled `ctx` | [Cancel]({{ '/examples/cancel' | relative_url }}) |
| `TurnOpts` | [Turn options]({{ '/examples/turn-opts' | relative_url }}) |
| `ListFiles` | [List files]({{ '/examples/list-files' | relative_url }}) |
| `ReadFile` | [Read file]({{ '/examples/read-file' | relative_url }}) |
| `RunCommand` | [Run command]({{ '/examples/run-command' | relative_url }}) |
| `ExportPatch` | [Export patch]({{ '/examples/export-patch' | relative_url }}) |
| `SetModel` | [Set model]({{ '/examples/set-model' | relative_url }}) |
| `Turn` events | [Print events]({{ '/examples/print-events' | relative_url }}) |
| `Capabilities` | [Capabilities]({{ '/examples/capabilities' | relative_url }}) |
| `Options` | [Custom VM]({{ '/examples/custom-vm' | relative_url }}) |
| `ErrGuestTooOld` | [Errors]({{ '/examples/errors' | relative_url }}) |
| `History` | [History]({{ '/examples/history' | relative_url }}) |
| `Turn` twice | [Multi-turn]({{ '/examples/multi-turn' | relative_url }}) |
| `SetMCPTokens` | [MCP tokens]({{ '/examples/mcp-tokens' | relative_url }}) |
