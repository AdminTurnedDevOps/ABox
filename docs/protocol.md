---
layout: default
title: Protocol versions
nav_order: 11
permalink: /protocol/
---

# Protocol versions
{: .no_toc }

Host↔guest RPC (`HelloParams.Protocol`). Independent of GitHub `v1.0.0`.

Current guest binary sends **4**. `Open` / `Resume` (and a normal `abox`
session) require 4. `abox --probe-vm` may still boot an older guest just
far enough to call `list_files`.

1. TOC
{:toc}

## Negotiation

The guest sends its version in `hello`. The host stores it on `Sandbox` and
replies with `HelloResult.Protocol`. The SDK exposes it as
`Capabilities().Protocol`. `0` in hello is treated as v1.

A protocol-4 guest refuses a host that acks below 4. A current host refuses
a guest below 4 for anything except `--probe-vm`.

## v1

Plain `user_turn` `{ "text": "…" }` and `agent_event` without extra fields.
Unknown methods (`cancel_turn`) return an error frame.

SDK: `Open` / `Resume` fail with `ErrGuestTooOld` (secretless config).

## v2

Additive JSON (`omitempty` so v1 guests ignore it):

- `UserTurnParams`: `max_turns`, `timeout_sec`, `rich_events`
- `cancel_turn` `{ "id": "<turn frame id>" }`
- `AgentEvent`: `tool_id`, `tool_args`, `usage`, `stop_reason`
- Kind `result` for usage / stop reason

Guest runs the turn in a goroutine so the read loop can see `cancel_turn`.
In-flight `run_command` is killed; list/read/search complete.

v2 still **pushed secrets** into the guest (`set_model.secrets`, config disk).
SDK: `Open` / `Resume` fail with `ErrGuestTooOld`.

## v3

Host **LLM broker**. Guest-initiated RPCs:

- `provider_open` `{ "model": "<profile name>" }`
- `provider_send` / `provider_cancel` on that stream id

The guest sends a model alias, messages, and tool schemas. The host resolves
the credential and dials `base_url`. No provider key, URL, or header in the
guest.

MCP in v3 was still guest-dialed. SDK: `Open` / `Resume` fail — v3 cannot
enforce brokered MCP and command approvals.

## v4 (current)

Host **MCP broker** plus **run_command approval**. Guest-initiated RPCs:

- `mcp_list` / `mcp_call` / `mcp_cancel`
- `request_run_command_approval`

Guest config that still contains `secrets` or `mcp_servers` is rejected.
The guest names a configured MCP server and tool; it cannot supply a URL
or header. Model-authored `run_command` blocks until the host returns
`allow_once` or `deny` (default deny).

Caps on this path (host-enforced): concurrent guest calls, MCP tool/schema
size, provider stream count, command byte length. See `protocol/protocol.go`.

## Upgrading a machine

```bash
make build && make image-update
```

Then `Open` a **new** session. Old `root.raw` files keep the guest they were
cloned with. Resume of a pre-v4 disk returns `ErrGuestTooOld`.
