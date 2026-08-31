---
layout: default
title: Sessions
nav_order: 5
permalink: /sessions/
---

# Sessions
{: .no_toc }

1. TOC
{:toc}

A session is a directory under `~/.abox/sessions/<id>/`:

| File | Role |
| --- | --- |
| `root.raw` | Writable VM disk (`/dev/vda`) |
| `config.raw` | Sealed config (`/dev/vdb`) |
| `session.json` | Host metadata |
| `transcript.json` | CLI TUI log (SDK does not write this) |
| `console.log` | Guest serial |
| `rpc.sock` | Host vsock proxy |

`Open` creates a new id. `Resume(id)` or `Resume("")` (latest for repo) boots
that `root.raw` again. The host git tree is not copied on resume.

## Lifetime

```text
Open  → clone golden → boot → tar HEAD into /work/repo
Turn  → user_turn / agent_event (repeat)
Close → shutdown RPC, SIGINT abox-vmm
```

Always `defer sess.Close()`. Leaking a session leaves a VM and a disk.

## Resume vs new Open

| | `Open` | `Resume` |
| --- | --- | --- |
| Disk | New clone of golden | Existing `root.raw` |
| Guest binary | Whatever was in golden **at clone time** | Same as when that session was created |
| Repo | Fresh tar of current HEAD | Guest files already on disk |

To pick up a new `abox-guest` (protocol 2), `make image-update` then **Open**,
not Resume of an old id.
