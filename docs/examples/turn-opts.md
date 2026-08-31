---
layout: default
title: Turn options
parent: Examples
nav_order: 4
permalink: /examples/turn-opts/
---

# Turn options

`Session.TurnOpts` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

`TurnOpts{MaxTurns, Timeout}` on a v2 guest. Prints usage when the provider sends it (OpenAI/Anthropic; xAI often nil).

```go
{% include examples/sdk-turn-opts.go %}
```
