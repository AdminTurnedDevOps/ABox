---
layout: default
title: Multi-turn
parent: Examples
nav_order: 15
permalink: /examples/multi-turn/
---

# Multi-turn

`Session.Turn` twice on one session on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

Two `Turn`s on the same VM. Guest context persists for the process lifetime (and on `root.raw` after `Close`, for `Resume`).

```go
{% include examples/sdk-multi-turn.go %}
```
