---
layout: default
title: Run command
parent: Examples
nav_order: 7
permalink: /examples/run-command/
---

# Run command

One SDK: [`pkg/abox`]({{ '/api' | relative_url }}). This page is a sample program that calls `Session.RunCommand`.

Guest `/bin/sh -c`. Default command: `uname -a && pwd && ls`.

Copy into your own `main.go` (`go get github.com/AdminTurnedDevOps/ABox@latest`):

```go
{% include examples/sdk-run-command.go %}
```

Optional: run this sample (same module as every other sample, not a second SDK):

```bash
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-run-command@latest
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-run-command@latest cat /etc/os-release
```
