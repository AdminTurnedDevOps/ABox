---
layout: default
title: Basic turn
parent: Examples
nav_order: 1
permalink: /examples/basic/
---

# Basic turn

One SDK: [`pkg/abox`]({{ '/api' | relative_url }}). This page is a sample program that calls `abox.Open` + `Session.Turn`.

`Open`, one `Turn`, stream `text`, `Close` on exit. SIGINT cancels the process (protocol 2 also cancels the in-flight turn).

Copy into your own `main.go` (`go get github.com/AdminTurnedDevOps/ABox@latest`):

```go
{% include examples/sdk-basic.go %}
```

Optional: run this sample (same module as every other sample, not a second SDK):

```bash
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-basic@latest
```
