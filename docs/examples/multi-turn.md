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

## CLI

The TUI is multi-turn on one VM. Guest context persists on `root.raw` after
quit, for `--resume`.

```bash
abox
# Remember the codeword: cedar. Reply ok.
# What was the codeword?

abox --resume
```

`abox exec` is a single prompt. Two turns in one process is SDK (or two
TUI messages).

## SDK

```go
{% include examples/sdk-multi-turn.go %}
```
