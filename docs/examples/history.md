---
layout: default
title: History
parent: Examples
nav_order: 14
permalink: /examples/history/
---

# History

`History()` is the slice from guest **hello** (empty on a brand-new session).
Turns after that live in guest context (`/var/lib/abox/context.json`); this
example still calls `Turn` so you can see streaming vs hello.

```bash
go run ./examples/sdk-history
```

Repo: [`examples/sdk-history`](https://github.com/AdminTurnedDevOps/ABox/tree/main/examples/sdk-history)
