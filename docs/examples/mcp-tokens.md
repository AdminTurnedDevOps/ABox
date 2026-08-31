---
layout: default
title: MCP tokens
parent: Examples
nav_order: 16
permalink: /examples/mcp-tokens/
---

# MCP tokens

Pushes a Bearer token into the guest and reconnects MCP. Configure servers with `abox mcp add` first. Host `credentials.env` is still how `Open` injects tokens at boot; this example is the live update path.

```bash
export ABOX_MCP_TOKEN_ENV=ABOX_MCP_GH_TOKEN
export ABOX_MCP_TOKEN=…   # not a real key in docs
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-mcp-tokens@latest
```

```go
{% include examples/sdk-mcp-tokens.go %}
```
