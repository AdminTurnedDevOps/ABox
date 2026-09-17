---
layout: default
title: Cancel
parent: Examples
nav_order: 3
permalink: /examples/cancel/
---

# Cancel

`Turn` with a canceled `context.Context` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

Requires protocol 4 (`Capabilities().Cancel` is true after `Open`). A 3s turn deadline fires `cancel_turn`. In-flight `run_command` is killed.

```go
{% include examples/sdk-cancel.go %}
```
