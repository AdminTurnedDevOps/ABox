---
layout: default
title: Print events
parent: Examples
nav_order: 10
permalink: /examples/print-events/
---

# Print events

One SDK: [`pkg/abox`]({{ '/api' | relative_url }}). This page is a sample program that calls `Session.Turn` + `Event`.

JSON-encodes every `Event` (same shape as `abox exec`).

Copy into your own `main.go` (`go get github.com/AdminTurnedDevOps/ABox@latest`):

```go
{% include examples/sdk-print-events.go %}
```

Optional: run this sample (same module as every other sample, not a second SDK):

```bash
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-print-events@latest
```
