---
layout: default
title: MCP tokens
parent: Examples
nav_order: 16
permalink: /examples/mcp-tokens/
---

# MCP tokens

`Session.SetMCPTokens` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

Overrides a Bearer token on the **host** MCP broker. Configure servers with `abox mcp add` first. `Open` already resolves tokens from keychain / `credentials.env` / cloud sources; this example is the live override path. Keys are destination env names (`GITHUB_MCP_TOKEN`, or `ABOX_MCP_<NAME>_TOKEN`).

```bash
export ABOX_MCP_TOKEN_ENV=ABOX_MCP_GH_TOKEN
export ABOX_MCP_TOKEN=…   # not a real key in docs
```

```go
{% include examples/sdk-mcp-tokens.go %}
```
