---
layout: default
title: Export patch
parent: Examples
nav_order: 8
permalink: /examples/export-patch/
---

# Export patch

Asks the agent to add a file, then `ExportPatch` (guest `git diff` vs the imported baseline).

```bash
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-export-patch@latest
```

```go
{% include examples/sdk-export-patch.go %}
```
