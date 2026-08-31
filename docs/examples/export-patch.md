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

```go
{% include examples/sdk-export-patch.go %}
```
