---
layout: default
title: Set model
parent: Examples
nav_order: 9
permalink: /examples/set-model/
---

# Set model

Switch profile after boot (`config.yaml` name). Default argument: `openai-default`. Needs that provider's key.

```bash
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-set-model@latest
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-set-model@latest grok-default
```

```go
{% include examples/sdk-set-model.go %}
```
