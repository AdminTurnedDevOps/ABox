---
layout: default
title: Read file
parent: Examples
nav_order: 6
permalink: /examples/read-file/
---

# Read file

`Session.ReadFile` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

Reads a guest path. Default `README.md`; pass another path as `os.Args[1]` (`go run . go.mod`).

## CLI

The CLI has no `ReadFile` probe. `--probe-vm` lists files. Supervisor
`ReadFile` is SDK-only.

Asking the TUI or `abox exec` to "read README.md" is a model **turn**, not
this RPC.

```bash
abox --probe-vm
```

## SDK

```go
{% include examples/sdk-read-file.go %}
```
