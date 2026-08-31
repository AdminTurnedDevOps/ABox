---
layout: default
title: Examples
nav_order: 8
has_children: true
permalink: /examples/
---

# Examples

Each program is a full `main` under `examples/` in the repo. Run from a git
worktree with `abox-vmm` on `PATH` and a provider key (except probe examples).

```bash
export PATH="/path/to/ABox/bin:$PATH"
go run ./examples/sdk-basic
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
