---
layout: default
title: Read file
parent: Examples
nav_order: 6
permalink: /examples/read-file/
---

# Read file

One SDK: [`pkg/abox`]({{ '/api' | relative_url }}). This page is a sample program that calls `Session.ReadFile`.

Reads a guest path (default `README.md`).

Copy into your own `main.go` (`go get github.com/AdminTurnedDevOps/ABox@latest`):

```go
{% include examples/sdk-read-file.go %}
```

Optional: run this sample (same module as every other sample, not a second SDK):

```bash
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-read-file@latest
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-read-file@latest go.mod
```
