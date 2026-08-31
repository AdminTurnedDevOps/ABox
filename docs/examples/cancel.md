---
layout: default
title: Cancel
parent: Examples
nav_order: 3
permalink: /examples/cancel/
---

# Cancel

One SDK: [`pkg/abox`]({{ '/api' | relative_url }}). This page is a sample program that calls `Turn` with a canceled `context.Context`.

Requires protocol 2 (`Capabilities().Cancel`). A 3s turn deadline fires `cancel_turn`. In-flight `run_command` is killed.

Copy into your own `main.go` (`go get github.com/AdminTurnedDevOps/ABox@latest`):

```go
{% include examples/sdk-cancel.go %}
```

Optional: run this sample (same module as every other sample, not a second SDK):

```bash
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-cancel@latest
```
