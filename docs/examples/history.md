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

## CLI

`History()` is SDK. The TUI equivalent is resume: it reloads
`transcript.json` (or guest `/var/lib/abox/context.json` if that file is
missing).

```bash
abox                          # new session; hello history is empty
abox --resume                 # same conversation in the TUI
```

`abox exec` is one prompt; it does not dump history.

## SDK

```go
{% include examples/sdk-history.go %}
```
