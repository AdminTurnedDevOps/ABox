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
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-run-command@latest
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-run-command@latest cat /etc/os-release
```

```go
{% include examples/sdk-run-command.go %}
```
