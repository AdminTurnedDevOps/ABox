---
layout: default
title: MCP tokens
parent: Examples
nav_order: 16
permalink: /examples/mcp-tokens/
---

# MCP tokens

One SDK: [`pkg/abox`]({{ '/api' | relative_url }}). This page is a sample program that calls `Session.SetMCPTokens`.

Pushes a Bearer token into the guest and reconnects MCP. Configure servers with `abox mcp add` first. Host `credentials.env` is still how `Open` injects tokens at boot; this example is the live update path.

Copy into your own `main.go` (`go get github.com/AdminTurnedDevOps/ABox@latest`):

```go
{% include examples/sdk-mcp-tokens.go %}
```

Optional: run this sample (same module as every other sample, not a second SDK):

```bash
export ABOX_MCP_TOKEN_ENV=ABOX_MCP_GH_TOKEN
export ABOX_MCP_TOKEN=…   # not a real key in docs
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-mcp-tokens@latest
```
