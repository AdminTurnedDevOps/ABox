---
layout: default
title: Errors
parent: Examples
nav_order: 13
permalink: /examples/errors/
---

# Errors

One SDK: [`pkg/abox`]({{ '/api' | relative_url }}). This page is a sample program that calls `abox.ErrGuestTooOld`.

Demonstrates a missing golden image, then `ErrGuestTooOld` when the guest is v1 and `TurnOpts` is used.

Copy into your own `main.go` (`go get github.com/AdminTurnedDevOps/ABox@latest`):

```go
{% include examples/sdk-errors.go %}
```

Optional: run this sample (same module as every other sample, not a second SDK):

```bash
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-errors@latest
```
