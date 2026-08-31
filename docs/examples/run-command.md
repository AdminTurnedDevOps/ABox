---
layout: default
title: Run command
parent: Examples
nav_order: 7
permalink: /examples/run-command/
---

# Run command

Guest `/bin/sh -c`. Default command: `uname -a && pwd && ls`.

```bash
go run ./examples/sdk-run-command
go run ./examples/sdk-run-command -- cat /etc/os-release
```

Repo: [`examples/sdk-run-command`](https://github.com/AdminTurnedDevOps/ABox/tree/main/examples/sdk-run-command)
