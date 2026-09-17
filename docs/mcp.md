---
layout: default
title: MCP
nav_order: 6
permalink: /mcp/
---

# MCP
{: .no_toc }

1. TOC
{:toc}

ABox is an MCP **client**. Remote tools are Streamable HTTP on the **host**.
Stdio MCP is not implemented.

The **host** dials MCP. The guest asks for `mcp_list` / `mcp_call` /
`mcp_cancel` over vsock and only names a configured server plus a tool.
Tokens never enter the guest.

{: .note }
Protocol 4 is required. Rebuild the guest (`make build && make image-update`)
and start a new session if `Open` returns `ErrGuestTooOld`.

## Add a server

`--mode` is required:

```bash
# Direct: host dials the server. Token optional.
abox mcp add --mode direct --credential-env GITHUB_MCP_TOKEN github https://api.githubcopilot.com/mcp/

# Agentgateway: host dials the gateway only. Auth is on the gateway.
abox mcp add --mode agentgateway agw https://agw.example/mcp
```

`abox mcp login <name>` then either:

- If `credential_env` is set: save that env var as a Bearer (PAT path)
- If it is omitted: host OAuth (PKCE S256). Opens a browser, local
  `127.0.0.1` redirect. Uses `client_id` when set, otherwise dynamic client
  registration when the authorization server advertises
  `registration_endpoint`

`/mcp` in the TUI pastes a Bearer for a server that already exists in config.

Default token env if you omit `--credential-env` is
`ABOX_MCP_<NAME>_TOKEN` (name uppercased, hyphens to underscores).

## Connectivity modes

One guest-visible tool list. Every remote MCP is a Streamable HTTP `url`.
Direct GitHub and an agentgateway virtual MCP use the same field;
`connectivity.mode` changes who is allowed to be an origin.

| Mode | Who the host may dial |
| --- | --- |
| `direct` | Every `mcp_servers` URL |
| `agentgateway` | Those URLs are the gateway (typically one). Bind GitHub, Atlassian, and the rest **on the gateway** |
| `offline` | No remote MCP and no provider HTTPS |

`enforcement: required` (the default `abox mcp add` writes for agentgateway)
means exactly one `mcp_servers` entry. Config load fails if you list more.
That is fail-closed: no silent fallback to `api.githubcopilot.com`.

ABox does not install the gateway. Binary, Docker, or Kubernetes all work.

**Direct**

```yaml
connectivity:
  mode: direct

mcp_servers:
  - name: github
    url: https://api.githubcopilot.com/mcp/
    credential_env: GITHUB_MCP_TOKEN
    # client_id: optional   # OAuth, if you skip credential_env
    # scopes: [mcp]
    # tool_allowlist: [create_issue, list_issues]
```

**Agentgateway**

```yaml
connectivity:
  mode: agentgateway
  enforcement: required

mcp_servers:
  - name: agw
    url: https://agw.example/mcp
```

Server names match `^[a-z0-9-]+$`. URLs must be HTTPS. LLM traffic still
hits each model's `base_url`; `agentgateway` mode does not wrap providers.

## Tool allowlist

If `tool_allowlist` is non-empty, only those tool names from that server are
advertised to the model. Discovered tools are prefixed `server__tool`
(example: `github__list_issues`).

The host MCP client is origin-bound to the configured URL. The guest cannot
widen that set.

## SDK

`Session.SetMCPTokens` overrides tokens on the **host** broker for this
process. Keys are the destination env names (`TokenEnv`, or
`--credential-env`):

```go
_ = sess.SetMCPTokens(ctx, map[string]string{
    "GITHUB_MCP_TOKEN": os.Getenv("GITHUB_MCP_TOKEN"),
})
```

Configure servers with `abox mcp add` first. [MCP tokens example]({{ '/examples/mcp-tokens' | relative_url }}).

MCP **tools** do not go through the `run_command` approval prompt. That is
still Planned.
