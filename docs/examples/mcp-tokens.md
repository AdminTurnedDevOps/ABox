---
layout: default
title: MCP tokens
parent: Examples
nav_order: 16
permalink: /examples/mcp-tokens/
---

# MCP tokens

`Session.SetMCPTokens` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

Pushes a Bearer token into the guest and reconnects MCP. Configure servers with `abox mcp add` first. Host `credentials.env` is still how `Open` injects tokens at boot; this example is the live update path.

```bash
export ABOX_MCP_TOKEN_ENV=ABOX_MCP_GH_TOKEN
export ABOX_MCP_TOKEN=…   # not a real key in docs
```

```go
{% include examples/sdk-mcp-tokens.go %}
```
