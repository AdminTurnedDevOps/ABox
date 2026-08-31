---
layout: default
title: Troubleshooting
nav_order: 9
permalink: /troubleshooting/
---

# Troubleshooting
{: .no_toc }

1. TOC
{:toc}

## `guest image missing … (run: make image)`

No golden `.raw`. Docker must be running:

```bash
make image
```

Confirm `~/.abox/images/abox-guest.raw` exists (~768 MiB).

## `abox-vmm not found; build with make build`

The SDK looks on `PATH`, then next to the current executable.

```bash
export PATH="/path/to/ABox/bin:$PATH"
# or
abox.Open(ctx, abox.Options{VMMPath: "/path/to/bin/abox-vmm"})
```

On Apple Silicon, `abox-vmm` must be codesigned (`make vmm` / `make sign`).
Unsigned helpers fail Hypervisor.framework.

## `start vm` / libkrun errors

- `kern.hv_support` must be `1`
- `brew install libkrun libkrunfw`
- Ad-hoc sign: `codesign --entitlements assets/entitlements.plist --force -s - bin/abox-vmm`

## `ErrGuestTooOld`

That session's `root.raw` has a v1 guest. `Resume` cannot pick up a new
binary from the golden image.

- Plain `Turn` (no `TurnOpts`, no reliance on cancel): works
- Cancel / `MaxTurns` / `Timeout`: fails

Fix: `make image-update`, then `Open` a new session. Check
`sess.Capabilities().Protocol == 2`.

## `missing credential XAI_API_KEY` (or OpenAI / Anthropic)

SDK calls `credentials.ApplyToEnv()` from `~/.abox/credentials.env`. Use
`/provider` in the TUI or write that file (mode `0600`). Empty env vars are
not injected into the guest.

## Usage is always nil

xAI: the SDK omits `stream_options.include_usage` (unverified on that API).
OpenAI/Anthropic: need a protocol 2 guest and a completed turn (`Kind: result`).
Streaming still works without usage.

## `turn already in progress`

Only one `user_turn` at a time per VM. Wait for the previous `Turn` to return
(or cancel it) before starting another.

## Boot hangs then context deadline

Default `BootTimeout` is 45s. First boot after `make image` can be slower.
Raise `Options.BootTimeout`. Check `sessions/<id>/console.log`.

## Docker packer 404 / daemon down

Session path does not need Docker. Only `make image` / `make image-update`
does. Start Docker Desktop, or you cannot refresh `abox-guest` on the golden
disk.

## Guest egress denied

Provider HTTPS is allowlisted (`api.x.ai`, `api.openai.com`,
`api.anthropic.com`) plus MCP hostnames from config. Custom `BaseURL` hosts
must be allowed in the guest or calls fail.

## Still stuck

- `sessions/<id>/console.log` — guest serial
- `abox --probe-vm` — VM without a model call
- GitHub: [AdminTurnedDevOps/ABox/issues](https://github.com/AdminTurnedDevOps/ABox/issues)
