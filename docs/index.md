---
layout: default
title: Overview
nav_order: 1
description: Embed a microVM-isolated agent in your Go program
permalink: /
redirect_from:
  - /sdk/
---

# ABox Go SDK
{: .no_toc }

Build an agent without putting the model loop on the host.
{: .abox-lede }

The agent loop and tools run in `abox-guest` inside a libkrun microVM. Your
process is the supervisor: open a session, stream events, approve shell,
cancel a turn, close the VM. Provider HTTPS and MCP HTTPS stay on the host.
The guest never sees a credential, base URL, or MCP endpoint.

| If you want to… | Use |
| --- | --- |
| Drive the agent from your own Go program | **This SDK** (`pkg/abox`) |
| Chat in a terminal | [CLI and TUI]({{ '/cli' | relative_url }}) (`abox`) |
| Store keys | [Credentials]({{ '/credentials' | relative_url }}) |
| Attach remote tools | [MCP]({{ '/mcp' | relative_url }}) |
| Gate model-authored shell | [Approvals]({{ '/approvals' | relative_url }}) |
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
The SDK boots the same microVM as the CLI. A current guest speaks **protocol 4**.
`Open` / `Resume` reject older disks.

## What the SDK does

| Capability | Detail |
| --- | --- |
| Session | Clone golden disk, boot libkrun, snapshot repo into `/work/repo` |
| Turn | Stream `text` / `tool` / `result` / `done` events; optional usage |
| Cancel | `ctx` cancel → `cancel_turn`; kills in-flight `run_command` |
| Tools | Guest `list_files`, `read_file`, `search`, `apply_patch`, `run_command` + host-brokered MCP |
| Approvals | `SetApprover` for model-authored `run_command` (default deny) |
| Resume | Boot an existing `root.raw` (`Resume`, same as `abox --resume`) |
| Probe | `ListFiles` / `ReadFile` / `RunCommand` without a model turn |

## What it does not do

- No host-side tool loop. Tools execute in the guest only.
- No `write_file`. Edits go through `apply_patch`.
- No Docker on the session path. Docker (today) only packs the golden `.raw`.
- No Linux/Windows VMM yet. Apple Silicon + libkrun.
- No guest NIC and no TSI inet. LLM and MCP HTTPS are host-brokered.
- No MCP tool approval yet. Only `run_command` prompts.

## Runtime pieces

```text
your process  (pkg/abox or abox)
  → host LLM broker     (provider HTTPS; keys stay here)
  → host MCP broker     (Streamable HTTP; tokens stay here)
  → abox-vmm
      → libkrun + libkrunfw   ← Linux kernel (not on the disk)
      → Hypervisor.framework
      → vsock only (krun_add_vsock flags 0)
  guest: /dev/vda = session root.raw (ext4 userspace)
         /dev/vdb = config.raw (session id + model alias; no secrets)
         process: /usr/local/bin/abox-guest
```

## Next

1. [Quickstart]({{ '/quickstart' | relative_url }}) — install, key, first turn
2. [Concepts]({{ '/concepts' | relative_url }}) — host vs guest, protocol 4
3. [CLI and TUI]({{ '/cli' | relative_url }}) — `abox`, slash commands, keys
4. [API]({{ '/api' | relative_url }}) — `Open`, `Turn`, `SetApprover`, …
5. [Troubleshooting]({{ '/troubleshooting' | relative_url }})
