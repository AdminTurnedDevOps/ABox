---
layout: default
title: Print events
parent: Examples
nav_order: 10
permalink: /examples/print-events/
---

# Print events

`Session.Turn` + `Event` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

JSON-encodes every `Event` (same shape as `abox exec`).

## CLI

```bash
abox exec --prompt "List two files in the repo."
```

One JSON object per event on stdout. Same shape as SDK `Event`.
[Events]({{ '/events' | relative_url }}).

## SDK

```go
{% include examples/sdk-print-events.go %}
```
