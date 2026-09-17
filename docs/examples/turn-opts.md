---
layout: default
title: Turn options
parent: Examples
nav_order: 4
permalink: /examples/turn-opts/
---

# Turn options

`Session.TurnOpts` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

`TurnOpts{MaxTurns, Timeout}` on a protocol-4 guest. Prints usage when the provider sends it (OpenAI/Anthropic; xAI often nil).

## CLI

The CLI does not expose `MaxTurns` or `Timeout`. `abox exec` runs until the
turn finishes or you Ctrl+C ([Cancel]({{ '/examples/cancel' | relative_url }})).
It does turn on rich events — stdout JSON is the same as SDK `Event`.

```bash
abox exec --prompt "Inspect the repo, then stop."
```

## SDK

```go
{% include examples/sdk-turn-opts.go %}
```
