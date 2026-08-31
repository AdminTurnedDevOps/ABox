---
layout: default
title: History
parent: Examples
nav_order: 14
permalink: /examples/history/
---

# History

One SDK: [`pkg/abox`]({{ '/api' | relative_url }}). This page is a sample program that calls `Session.History`.

`History()` is the slice from guest **hello** (empty on a brand-new session). Turns after that live in guest context (`/var/lib/abox/context.json`); this example still calls `Turn` so you can see streaming vs hello.

Copy into your own `main.go` (`go get github.com/AdminTurnedDevOps/ABox@latest`):

```go
{% include examples/sdk-history.go %}
```

Optional: run this sample (same module as every other sample, not a second SDK):

```bash
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-history@latest
```
