---
layout: default
title: Turn options
parent: Examples
nav_order: 4
permalink: /examples/turn-opts/
---

# Turn options

One SDK: [`pkg/abox`]({{ '/api' | relative_url }}). This page is a sample program that calls `Session.TurnOpts`.

`TurnOpts{MaxTurns, Timeout}` on a v2 guest. Prints usage when the provider sends it (OpenAI/Anthropic; xAI often nil).

Copy into your own `main.go` (`go get github.com/AdminTurnedDevOps/ABox@latest`):

```go
{% include examples/sdk-turn-opts.go %}
```

Optional: run this sample (same module as every other sample, not a second SDK):

```bash
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-turn-opts@latest
```
