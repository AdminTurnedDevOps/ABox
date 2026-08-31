---
layout: default
title: Multi-turn
parent: Examples
nav_order: 15
permalink: /examples/multi-turn/
---

# Multi-turn

One SDK: [`pkg/abox`]({{ '/api' | relative_url }}). This page is a sample program that calls `Session.Turn` twice on one session.

Two `Turn`s on the same VM. Guest context persists for the process lifetime (and on `root.raw` after `Close`, for `Resume`).

Copy into your own `main.go` (`go get github.com/AdminTurnedDevOps/ABox@latest`):

```go
{% include examples/sdk-multi-turn.go %}
```

Optional: run this sample (same module as every other sample, not a second SDK):

```bash
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-multi-turn@latest
```
