---
layout: default
title: Basic turn
parent: Examples
nav_order: 1
permalink: /examples/basic/
---

# Basic turn

`Open`, one `Turn`, stream `text`, `Close` on exit. SIGINT cancels the process (protocol 2 also cancels the in-flight turn).

```bash
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-basic@latest
```

```go
{% include examples/sdk-basic.go %}
```
