---
layout: default
title: Protocol versions
nav_order: 7
permalink: /protocol/
---

# Protocol versions
{: .no_toc }

Host↔guest RPC (`HelloParams.Protocol`). Independent of GitHub `v1.0.0`.

Current guest binary sends **2**.

## Negotiation

`runtime` stores hello's protocol on `Sandbox`. The SDK exposes it as
`Capabilities().Protocol`. `0` in hello is treated as v1.

## v1

Plain `user_turn` `{ "text": "…" }` and `agent_event` without extra fields.
Unknown methods (`cancel_turn`) return an error frame the host ignores if the
id does not match the turn.

SDK: `Turn` without extra options works. Anything that needs v2 returns
`ErrGuestTooOld`.

## v2

Additive JSON (`omitempty` so v1 guests ignore it):

- `UserTurnParams`: `max_turns`, `timeout_sec`, `rich_events`
- `cancel_turn` `{ "id": "<turn frame id>" }`
- `AgentEvent`: `tool_id`, `tool_args`, `usage`, `stop_reason`
- Kind `result` for usage / stop reason

Guest runs the turn in a goroutine so the read loop can see `cancel_turn`.
In-flight `run_command` is killed; list/read/search complete.

## Upgrading a machine

```bash
make build && make image-update
```

Then `Open` a **new** session. Old `root.raw` files keep the guest they were
cloned with.
