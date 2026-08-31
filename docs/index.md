---
layout: home
title: ABox
---

ABox is an isolated agent harness. The prompt, model client, and tools run
inside a libkrun microVM on Apple Silicon. The host process is TUI (or your
Go program) plus a small VMM helper.

**Isolation claims stay Planned** until the hardware suite in `PLAN.md` passes.

## Docs

- **[Go SDK]({{ '/sdk' | relative_url }})** — embed ABox in a Go program
- [README](https://github.com/AdminTurnedDevOps/ABox#readme) — CLI, disks, release
- [`examples/sdk-basic`](https://github.com/AdminTurnedDevOps/ABox/tree/main/examples/sdk-basic)

## How a session boots

The golden `.raw` is an ext4 **root filesystem** (Alpine + `abox-guest`), not a
kernel. The kernel comes from **libkrunfw**. A session clones that golden disk
to `root.raw` (`/dev/vda`) and attaches sealed `config.raw` as `/dev/vdb`.
