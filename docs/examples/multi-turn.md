---
layout: default
title: Multi-turn
parent: Examples
nav_order: 15
permalink: /examples/multi-turn/
---

# Multi-turn

Two `Turn`s on the same VM. Guest context persists for the process lifetime (and on `root.raw` after `Close`, for `Resume`).

```bash
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-multi-turn@latest
```

```go
{% include examples/sdk-multi-turn.go %}
```
