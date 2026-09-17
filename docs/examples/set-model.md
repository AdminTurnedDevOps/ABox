---
layout: default
title: Set model
parent: Examples
nav_order: 9
permalink: /examples/set-model/
---

# Set model

`Session.SetModel` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

Switch profile after boot (`config.yaml` name). Default `openai-default`; pass another name as `os.Args[1]`. Needs that provider's key.

## CLI

Profile name from `~/.abox/config.yaml` (`grok-default`, `openai-default`,
`claude-default`, or a name you added):

```bash
abox --model openai-default
abox exec --model openai-default --prompt "Name the model you are."
```

Needs that provider's key. Mid-session switch in the TUI is `/provider`
(paste a key) or `/credential` (cloud pointer) — not a profile rename.
`SetModel` after boot is SDK-only.

## SDK

```go
{% include examples/sdk-set-model.go %}
```
