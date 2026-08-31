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
| Runs | `pkg/abox` + `abox-vmm` | Agent loop, tools, provider HTTP |
| Sees | Session dir on the Mac | `/work/repo` inside the VM |
| Must not | Execute model-authored commands | Import `internal/tui` |

The SDK never runs the agent loop on the host. It sends `user_turn` over
vsock and renders `agent_event` frames.

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
guest: /dev/vdb                            sealed model + keys
```

## Three files

1. **Golden image** — `~/.abox/images/abox-guest.raw`. Packed once (`make image`). Template. Not attached to a running VM.
2. **Session disk** — `~/.abox/sessions/<id>/root.raw`. Clone of (1). This is `/dev/vda`. Destroy the session dir and this disk is gone; the golden stays.
3. **Config disk** — `sessions/<id>/config.raw`. ~1 MiB, `/dev/vdb`. Not cloned from the golden image.

`Open` copies (1)→(2) and writes (3). `Resume` boots the existing (2).

## Protocol version

This is the **host↔guest RPC** version (`HelloParams.Protocol`), not the
GitHub release tag.

| Guest | How you got it | SDK |
| --- | --- | --- |
| v1 | Session disk cloned before the v2 `abox-guest` rebuild | Plain `Turn` only |
| v2 | `make build && make image-update`, then a **new** `Open` | Cancel, `TurnOpts`, usage |

`Resume` always uses that session's disk. Updating the golden image does not
upgrade old sessions. Check `sess.Capabilities()`.

## Tools

Five builtins in the guest: `list_files`, `read_file`, `search`,
`apply_patch`, `run_command`. MCP tools are discovered over Streamable HTTP
from the guest. No `write_file`.
