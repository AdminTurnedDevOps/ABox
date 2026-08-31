---
layout: default
title: Multi-turn
parent: Examples
nav_order: 15
permalink: /examples/multi-turn/
---

# Multi-turn

Two `Turn`s on the same VM. Guest context persists for the process lifetime
(and on `root.raw` after `Close`, for `Resume`).

```bash
go run ./examples/sdk-multi-turn
```

Repo: [`examples/sdk-multi-turn`](https://github.com/AdminTurnedDevOps/ABox/tree/main/examples/sdk-multi-turn)
