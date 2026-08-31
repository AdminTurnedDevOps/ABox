---
layout: default
title: Examples
nav_order: 8
has_children: true
permalink: /examples/
---

# Examples

Each page is a full `main`. Run from any directory — you do not need an ABox
checkout.

`abox-vmm` must be on `PATH` (the `darwin_arm64` archive on the
[GitHub release](https://github.com/AdminTurnedDevOps/ABox/releases)). Golden
image and provider key: [Quickstart]({{ '/quickstart' | relative_url }}).
Probe examples (`ListFiles`, `ReadFile`, `RunCommand`) do not need a key.

```bash
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-basic@latest
```

Or copy the Go from a child page into `main.go` in your own module:

```bash
go get github.com/AdminTurnedDevOps/ABox@latest
go run .
```

| Example | What it shows |
| --- | --- |
| [Basic turn]({{ '/examples/basic' | relative_url }}) | `Open` + `Turn` + SIGINT |
| [Resume]({{ '/examples/resume' | relative_url }}) | `Resume("")` latest disk |
| [Cancel]({{ '/examples/cancel' | relative_url }}) | `ctx` cancel mid-turn |
| [Turn options]({{ '/examples/turn-opts' | relative_url }}) | `MaxTurns`, `Timeout` |
| [List files]({{ '/examples/list-files' | relative_url }}) | Probe without a model |
| [Read file]({{ '/examples/read-file' | relative_url }}) | `ReadFile` |
| [Run command]({{ '/examples/run-command' | relative_url }}) | Guest `run_command` |
| [Export patch]({{ '/examples/export-patch' | relative_url }}) | Guest diff vs baseline |
| [Set model]({{ '/examples/set-model' | relative_url }}) | Switch profile after boot |
| [Print events]({{ '/examples/print-events' | relative_url }}) | Every `Event.Kind` |
| [Capabilities]({{ '/examples/capabilities' | relative_url }}) | Protocol v1 vs v2 |
| [Custom VM]({{ '/examples/custom-vm' | relative_url }}) | vCPU, RAM, image, VMM path |
| [Errors]({{ '/examples/errors' | relative_url }}) | Missing image / `ErrGuestTooOld` |
| [History]({{ '/examples/history' | relative_url }}) | `History()` after hello |
| [Multi-turn]({{ '/examples/multi-turn' | relative_url }}) | Two turns, same VM |
| [MCP tokens]({{ '/examples/mcp-tokens' | relative_url }}) | `SetMCPTokens` |
