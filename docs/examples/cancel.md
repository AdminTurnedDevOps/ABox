---
layout: default
title: Cancel
parent: Examples
nav_order: 3
permalink: /examples/cancel/
---

# Cancel

`Turn` with a canceled `context.Context` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

Requires protocol 4 (`Capabilities().Cancel` is true after `Open`). A 3s turn deadline fires `cancel_turn`. In-flight `run_command` is killed.

## CLI

There is no `--timeout` flag. Ctrl+C (SIGINT) cancels an in-flight turn
(protocol 4), including guest `run_command`:

```bash
abox exec --prompt "Count slowly from 1 to 50 in words."
# Ctrl+C → cancel_turn
```

In the TUI, Ctrl+C quits (and denies an in-flight approval first).
`MaxTurns` and a turn `Timeout` are [SDK-only](#sdk)
([Turn options]({{ '/examples/turn-opts' | relative_url }})).

## SDK

```go
{% include examples/sdk-cancel.go %}
```
