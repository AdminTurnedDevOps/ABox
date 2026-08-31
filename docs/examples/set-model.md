---
layout: default
title: Set model
parent: Examples
nav_order: 9
permalink: /examples/set-model/
---

# Set model

Switch profile after boot (`config.yaml` name). Default argument:
`openai-default`. Needs that provider's key.

```bash
go run ./examples/sdk-set-model
go run ./examples/sdk-set-model -- grok-default
```

Repo: [`examples/sdk-set-model`](https://github.com/AdminTurnedDevOps/ABox/tree/main/examples/sdk-set-model)
