---
layout: default
title: Custom VM
parent: Examples
nav_order: 12
permalink: /examples/custom-vm/
---

# Custom VM

One SDK: [`pkg/abox`]({{ '/api' | relative_url }}). This page is a sample program that calls `abox.Options`.

Sets vCPU, RAM, boot timeout, and `VMMPath`. Lists files (no model).

Copy into your own `main.go` (`go get github.com/AdminTurnedDevOps/ABox@latest`):

```go
{% include examples/sdk-custom-vm.go %}
```

Optional: run this sample (same module as every other sample, not a second SDK):

```bash
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-custom-vm@latest
```
