---
layout: default
title: Concepts
nav_order: 3
permalink: /concepts/
---

# Concepts
{: .no_toc }

1. TOC
{:toc}

## Host vs guest

| | Host (your process) | Guest (`abox-guest`) |
| --- | --- | --- |
| Runs | `pkg/abox` or `abox`, `abox-vmm`, LLM broker, MCP broker | Agent loop, five tools |
| Sees | Session dir on the Mac, `config.yaml`, credentials | `/work/repo` inside the VM |
| HTTPS | Provider and MCP Streamable HTTP | None. No guest NIC, no TSI inet |
| Must not | Execute model-authored commands itself | Import `internal/tui`, `internal/provider`, or `cmd/abox` |

The SDK never runs the agent loop on the host. It sends `user_turn` over
vsock and renders `agent_event` frames. When the guest needs a model or an
MCP tool, it names a configured alias; the host broker dials the network.

{: .important }
Isolation claims stay **Planned**. The device plan is vsock-only (`krun_add_vsock`
with flags 0), no guest NIC, no host-path virtio-fs. That is an intended
allowlist, not a passed hardware suite.

## Kernel vs disk

The `.raw` file is **only a disk**: ext4 userspace (Alpine, `git`, `patch`,
`abox-guest`). No kernel, no bootloader.

The guest kernel is **libkrunfw** (Homebrew). `abox-vmm` attaches host files
as virtio-blk:

```text
host:  ~/.abox/sessions/<id>/root.raw      Mac file (ext4)
         ↓ libkrun virtio-blk
guest: /dev/vda  →  /                      Alpine + abox-guest + /work/repo

host:  ~/.abox/sessions/<id>/config.raw
         ↓
guest: /dev/vdb                            session id + model alias
                                           (no secrets, no MCP URLs)
```

## Three files

1. **Golden image** — `~/.abox/images/abox-guest.raw`. Packed once (`make image`). Template. Not attached to a running VM.
2. **Session disk** — `~/.abox/sessions/<id>/root.raw`. Clone of (1). This is `/dev/vda`. Destroy the session dir and this disk is gone; the golden stays.
3. **Config disk** — `sessions/<id>/config.raw`. ~1 MiB, read-only `/dev/vdb`. Not cloned from the golden image. Model profile only; credentials stay on the host.

`Open` copies (1)→(2) and writes (3). `Resume` boots the existing (2) and
rewrites (3) without secrets. Startup also scrubs leftover plaintext keys
out of old `config.raw` / `guest-config.json` files.

A guest that still finds `secrets` or `mcp_servers` in that config refuses
to boot. Rebuild the image and start a new session.

## Host brokers

Two host-side clients sit between the guest and the network:

| Broker | Guest may send | Host does |
| --- | --- | --- |
| LLM (`internal/llmbroker`) | model alias + messages + tool schemas | Resolves the key, dials `base_url` |
| MCP (`internal/mcpbroker`) | configured server name + tool name + args | Resolves the token, dials that server's URL |

The guest cannot supply a URL, header, or credential. `connectivity.mode: offline`
disables both remote MCP and provider HTTPS.

LLM traffic does **not** use `connectivity.mode` for routing. `direct` vs
`agentgateway` applies to MCP servers. The LLM broker always hits the selected
model's `base_url`.

## Protocol version

This is the **host↔guest RPC** version (`HelloParams.Protocol`), not the
GitHub release tag. Current guest binary sends **4**. `Open` / `Resume`
require 4.

| Guest | What it added | SDK today |
| --- | --- | --- |
| v1 | Plain `Turn` | Rejected (`ErrGuestTooOld`) |
| v2 | Cancel, `TurnOpts`, usage / rich events. Secrets still pushed into the guest | Rejected |
| v3 | Host LLM broker; secretless provider path | Rejected |
| v4 | Host MCP broker + `run_command` approval | Required |

`Resume` always uses that session's disk. Updating the golden image does not
upgrade old sessions. Check `sess.Capabilities()`. After a successful `Open`,
`Protocol` is 4 and `Cancel`, `RichEvents`, `TurnOptions`, `Approvals`, and
`MCPBroker` are all true.

Details: [Protocol versions]({{ '/protocol' | relative_url }}).

## Tools

Five builtins in the guest: `list_files`, `read_file`, `search`,
`apply_patch`, `run_command`. Paths cannot escape `/work/repo`. No
`write_file`.

MCP tools are discovered on the host and advertised to the model as
`server__tool`. Optional per-server `tool_allowlist` in `config.yaml`.

Model-authored `run_command` asks the host before exec. Default is deny.
[Approvals]({{ '/approvals' | relative_url }}). MCP tools do not prompt yet.

Host-initiated `Session.RunCommand` / `abox --probe-vm` are supervisor RPCs,
not model tool calls, and do not go through that gate.

## Source snapshot

`Open` snapshots exactly the configured source directory into the guest. It
does not discover a Git root, inspect branches or `HEAD`, or require Git on the
host. `.git` files and directories are excluded, and symlinks and special files
are rejected. The guest creates its own private Git baseline after transfer so
patch export remains available. `Resume(id)` boots the existing disk and does
not recopy the host source directory.

Git ignore rules do not control this snapshot. All regular files and dotfiles
other than `.git` metadata are included, so the selected source directory must
contain only files the guest is allowed to read.
