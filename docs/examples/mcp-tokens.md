---
layout: default
title: MCP tokens
parent: Examples
nav_order: 16
permalink: /examples/mcp-tokens/
---

# MCP tokens

`Session.SetMCPTokens` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

Overrides a Bearer token on the **host** MCP broker. Configure servers with `abox mcp add` first. `Open` already resolves tokens from the OS keystore / `credentials.env` / cloud sources; this example is the live override path. Keys are destination env names (`GITHUB_MCP_TOKEN`, or `ABOX_MCP_<NAME>_TOKEN`).

## CLI

Configure the server on the host, then login. Tokens never enter the guest.

```bash
abox mcp add --mode direct --credential-env GITHUB_MCP_TOKEN github https://api.githubcopilot.com/mcp/
abox mcp login github
abox
```

`/mcp` in the TUI pastes a Bearer for a server that already exists.
Default token env if you omit `--credential-env` is `ABOX_MCP_<NAME>_TOKEN`.

`SetMCPTokens` is a live override for this process (SDK). [MCP]({{ '/mcp' | relative_url }}).

## SDK

To run this sample:

```bash
export ABOX_MCP_TOKEN_ENV=ABOX_MCP_GH_TOKEN
export ABOX_MCP_TOKEN=…   # not a real key in docs
```

```go
{% include examples/sdk-mcp-tokens.go %}
```
