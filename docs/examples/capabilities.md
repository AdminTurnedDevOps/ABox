---
layout: default
title: Capabilities
parent: Examples
nav_order: 11
permalink: /examples/capabilities/
---

# Capabilities

One SDK: [`pkg/abox`]({{ '/api' | relative_url }}). This page is a sample program that calls `Session.Capabilities`.

Prints protocol and v2 flags. Exit 1 if the guest is still v1.

Copy into your own `main.go` (`go get github.com/AdminTurnedDevOps/ABox@latest`):

```go
{% include examples/sdk-capabilities.go %}
```

Optional: run this sample (same module as every other sample, not a second SDK):

```bash
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-capabilities@latest
```
