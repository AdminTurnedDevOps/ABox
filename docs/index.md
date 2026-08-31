---
layout: default
title: Overview
nav_order: 1
description: Embed a microVM-isolated agent in your Go program
permalink: /
---

# ABox Go SDK
{: .no_toc }

Build an agent without putting the model loop on the host.
{: .fs-6 .fw-300 }

The agent — prompt, provider HTTP, tools — runs in `abox-guest` inside a
libkrun microVM. Your process is the supervisor: open a session, stream
events, cancel a turn, close the VM.

| If you want to… | Use |
| --- | --- |
| Drive the agent from your own Go program | **This SDK** (`pkg/abox`) |
| Chat in a terminal | [CLI](https://github.com/AdminTurnedDevOps/ABox#quickstart) (`abox`) |
| Boot the VM and list files, no model | `abox --probe-vm` or `Session.ListFiles` |

```go
sess, err := abox.Open(ctx, abox.Options{})
if err != nil { log.Fatal(err) }
defer sess.Close()

_, err = sess.Turn(ctx, "What does this repo do?", func(ev abox.Event) {
    if ev.Kind == "text" { fmt.Print(ev.Text) }
})
```

{: .important }
Isolation claims stay **Planned** until the hardware suite in `PLAN.md` passes.
The SDK boots the same microVM as the CLI.

## What the SDK does

| Capability | Detail |
| --- | --- |
| Session | Clone golden disk, boot libkrun, snapshot repo into `/work/repo` |
| Turn | Stream `text` / `tool` / `result` / `done` events; optional usage |
| Cancel | `ctx` cancel → `cancel_turn`; kills in-flight `run_command` |
| Tools | Guest `list_files`, `read_file`, `search`, `apply_patch`, `run_command` + MCP |
| Resume | Boot an existing `root.raw` (`Resume`, same as `abox --resume`) |
| Probe | `ListFiles` / `ReadFile` / `RunCommand` without a model turn |

## What it does not do

- No host-side tool loop. Tools execute in the guest only.
- No `write_file`. Edits go through `apply_patch`.
- No Docker on the session path. Docker (today) only packs the golden `.raw`.
- No Linux/Windows VMM yet. Apple Silicon + libkrun.

## Runtime pieces

```text
your process  (pkg/abox)
  → abox-vmm
      → libkrun + libkrunfw   ← Linux kernel (not on the disk)
      → Hypervisor.framework
  guest: /dev/vda = session root.raw (ext4 userspace)
         /dev/vdb = config.raw (model, keys)
         process: /usr/local/bin/abox-guest
```

## Next

1. [Quickstart]({{ '/quickstart' | relative_url }}) — install, key, first turn
2. [Concepts]({{ '/concepts' | relative_url }}) — kernel vs disk, host vs guest
3. [Examples]({{ '/examples' | relative_url }}) — copy-paste programs
4. [API]({{ '/api' | relative_url }}) — `Open`, `Turn`, `TurnOpts`, …
5. [Troubleshooting]({{ '/troubleshooting' | relative_url }})
