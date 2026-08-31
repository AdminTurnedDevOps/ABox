---
layout: default
title: Export patch
parent: Examples
nav_order: 8
permalink: /examples/export-patch/
---

# Export patch

One SDK: [`pkg/abox`]({{ '/api' | relative_url }}). This page is a sample program that calls `Session.ExportPatch`.

Asks the agent to add a file, then `ExportPatch` (guest `git diff` vs the imported baseline).

Copy into your own `main.go` (`go get github.com/AdminTurnedDevOps/ABox@latest`):

```go
{% include examples/sdk-export-patch.go %}
```

Optional: run this sample (same module as every other sample, not a second SDK):

```bash
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-export-patch@latest
```
