---
layout: default
title: List files
parent: Examples
nav_order: 5
permalink: /examples/list-files/
---

# List files

One SDK: [`pkg/abox`]({{ '/api' | relative_url }}). This page is a sample program that calls `Session.ListFiles`.

No model call. Same idea as `abox --probe-vm`.

Copy into your own `main.go` (`go get github.com/AdminTurnedDevOps/ABox@latest`):

```go
{% include examples/sdk-list-files.go %}
```

Optional: run this sample (same module as every other sample, not a second SDK):

```bash
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-list-files@latest
```
