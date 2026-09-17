---
layout: default
title: Basic turn
parent: Examples
nav_order: 1
permalink: /examples/basic/
---

# Basic turn

`abox.Open` + `Session.Turn` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

`Open`, one `Turn`, stream `text`, `Close` on exit. SIGINT cancels the process and the in-flight turn (protocol 4).

## CLI

New session in the TUI:

```bash
abox
```

Headless one-shot. JSON events on stdout (same shape as the SDK `Event`):

```bash
abox exec --prompt "Say hello in one sentence."
```

`--exec` is an alias of the `exec` subcommand. `exec` requires `--prompt`.
[CLI and TUI]({{ '/cli' | relative_url }}).

## SDK

```go
{% include examples/sdk-basic.go %}
```
