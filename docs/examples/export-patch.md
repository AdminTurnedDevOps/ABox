---
layout: default
title: Export patch
parent: Examples
nav_order: 8
permalink: /examples/export-patch/
---

# Export patch

`Session.ExportPatch` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

Asks the agent to add a file, then `ExportPatch` (guest `git diff` vs the imported baseline).

## CLI

There is no `abox export` (or similar). Guest edits stay on that session's
`root.raw`. Resume to keep working:

```bash
abox
# ask the agent to add a file
abox --resume <id>            # later; same disk
```

Dumping `git diff` vs the imported baseline is SDK `ExportPatch`.

## SDK

```go
{% include examples/sdk-export-patch.go %}
```
