---
layout: default
title: Cancel
parent: Examples
nav_order: 3
permalink: /examples/cancel/
---

# Cancel

Requires protocol 2 (`Capabilities().Cancel`). A 3s turn deadline fires
`cancel_turn`. In-flight `run_command` is killed.

```bash
go run ./examples/sdk-cancel
```

Repo: [`examples/sdk-cancel`](https://github.com/AdminTurnedDevOps/ABox/tree/main/examples/sdk-cancel)
