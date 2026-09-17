---
layout: default
title: Basic turn
parent: Examples
nav_order: 1
permalink: /examples/basic/
---

# Basic turn

`abox.Open` + `Session.Turn` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

`Open`, one `Turn`, stream `text`, `Close` on exit. SIGINT cancels the process and the in-flight turn (protocol 4).

```go
{% include examples/sdk-basic.go %}
```
