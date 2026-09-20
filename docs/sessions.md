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
| `session.json` | Host metadata: id/source, image identity, guest architecture/protocol, VMM backend |
| `transcript.json` | CLI TUI log (SDK does not write this) |
| `console.log` | Guest serial |
| `rpc.sock` | Host vsock proxy |

`Open` creates a new id. `Resume(id)` boots that `root.raw` again. An id is
required; resume does not infer session identity from a directory or Git state.
The host source directory is not copied on resume.

Only one supervisor may own a session at a time. A per-session runtime lock
rejects concurrent resume before config or disk preparation. The root-owned
guest agent remains protected from model commands because shell and Git tool
subprocesses run as the unprivileged guest repository owner.

On every start, ABox scrubs leftover plaintext secrets out of `config.raw`
and `guest-config.json` (including leftover sessions under the old
`~/Library/Application Support/ABox` path). It never deletes sessions.

## Lifetime

```text
Open  → snapshot source directory → clone golden → boot → transfer into /work/repo
Turn  → user_turn / agent_event (repeat); host brokers HTTPS
Close → shutdown RPC, platform helper signal, bounded kill, Wait
```

Always `defer sess.Close()`. Leaking a session leaves a VM and a disk.

On macOS, the helper fallback signal is interrupt (`SIGINT`). On Linux, it is
`SIGTERM`, never `SIGINT`; if the helper does not exit within the bound, ABox
uses `SIGKILL` and always waits/reaps it. The Linux CLI handles top-level
`SIGINT`, `SIGTERM`, and `SIGHUP` through the same cleanup path. A second
termination signal restores the operating system's default forced-exit behavior.
The inherited liveness pipe also makes `abox-vmm` exit if its supervisor dies.

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

Resume also checks that the recorded guest architecture, protocol, and VMM
backend match the current host. Linux rejects sessions that predate this
metadata; macOS/arm64 may backfill the narrow legacy format because arm64/HVF
was the only previously runnable combination.

To pick up a new `abox-guest` (protocol 4), `make image-update` then **Open**,
not Resume of an old id. Resume of a pre-v4 disk returns `ErrGuestTooOld`.
