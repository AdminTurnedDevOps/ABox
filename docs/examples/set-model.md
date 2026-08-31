---
layout: default
title: Set model
parent: Examples
nav_order: 9
permalink: /examples/set-model/
---

# Set model

One SDK: [`pkg/abox`]({{ '/api' | relative_url }}). This page is a sample program that calls `Session.SetModel`.

Switch profile after boot (`config.yaml` name). Default argument: `openai-default`. Needs that provider's key.

Copy into your own `main.go` (`go get github.com/AdminTurnedDevOps/ABox@latest`):

```go
{% include examples/sdk-set-model.go %}
```

Optional: run this sample (same module as every other sample, not a second SDK):

```bash
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-set-model@latest
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-set-model@latest grok-default
```
