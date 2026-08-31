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

```go
{% include examples/sdk-set-model.go %}
```
