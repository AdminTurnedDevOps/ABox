---
layout: default
title: List files
parent: Examples
nav_order: 5
permalink: /examples/list-files/
---

# List files

`Session.ListFiles` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

No model call. Same RPC as `abox --probe-vm` (path `.`, depth 4, limit 50).

## CLI

```bash
abox --probe-vm
abox --resume <id> --probe-vm # existing disk; still no model
```

No provider key. `--probe-vm` is also the only CLI path that still talks to a
pre-protocol-4 guest (list files only).

## SDK

```go
{% include examples/sdk-list-files.go %}
```
