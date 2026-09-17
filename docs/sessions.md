---
layout: default
title: Sessions
nav_order: 9
permalink: /sessions/
---

# Sessions
{: .no_toc }

1. TOC
{:toc}

A session is a directory under `~/.abox/sessions/<id>/` (`ABOX_HOME` overrides
the home):

| File | Role |
| --- | --- |
| `root.raw` | Writable VM disk (`/dev/vda`) |
| `config.raw` | Sealed config (`/dev/vdb`): session id + model alias. **No secrets** |
| `guest-config.json` | Host-side copy of that config, also secretless |
| `session.json` | Host metadata (id, source directory, created) |
| `transcript.json` | CLI TUI log (SDK does not write this) |
| `console.log` | Guest serial |
| `rpc.sock` | Host vsock proxy |

`Open` creates a new id. `Resume(id)` boots that `root.raw` again. An id is
required; resume does not infer session identity from a directory or Git state.
The host source directory is not copied on resume.

On every start, ABox scrubs leftover plaintext secrets out of `config.raw`
and `guest-config.json` (including leftover sessions under the old
`~/Library/Application Support/ABox` path). It never deletes sessions.

## Lifetime

```text
Open  → snapshot source directory → clone golden → boot → transfer into /work/repo
Turn  → user_turn / agent_event (repeat); host brokers HTTPS
Close → shutdown RPC, SIGINT abox-vmm
```

Always `defer sess.Close()`. Leaking a session leaves a VM and a disk.

Guest conversation state lives on the session disk at
`/var/lib/abox/context.json`. Resume reloads it. The TUI also keeps
`transcript.json` on the host.

## Resume vs new Open

| | `Open` | `Resume` |
| --- | --- | --- |
| Disk | New clone of golden | Existing `root.raw` |
| Guest binary | Whatever was in golden **at clone time** | Same as when that session was created |
| Source files | Fresh snapshot of exact configured directory | Guest files already on disk |
| Config disk | Secretless, current model | Rewritten secretless; old keys stripped |

To pick up a new `abox-guest` (protocol 4), `make image-update` then **Open**,
not Resume of an old id. Resume of a pre-v4 disk returns `ErrGuestTooOld`.
