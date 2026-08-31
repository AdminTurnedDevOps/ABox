---
layout: default
title: History
parent: Examples
nav_order: 14
permalink: /examples/history/
---

# History

`Session.History` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

`History()` is the slice from guest **hello** (empty on a brand-new session). Turns after that live in guest context (`/var/lib/abox/context.json`); this example still calls `Turn` so you can see streaming vs hello.

```go
{% include examples/sdk-history.go %}
```
