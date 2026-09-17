---
layout: default
title: Troubleshooting
nav_order: 13
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

## `ErrGuestTooOld` / `guest protocol N cannot enforce…`

That session's `root.raw` has a guest older than protocol 4 (host LLM/MCP
brokers + `run_command` approval). `Resume` cannot pick up a new binary
from the golden image.

Fix:

```bash
make build && make image-update
```

Then `Open` a **new** session. Check
`sess.Capabilities().Protocol == 4`.

`--probe-vm` can still list files on an old disk. A real turn cannot.

## `legacy guest config contains credentials or MCP endpoints`

The guest found secrets or MCP URLs on `config.raw`. Current images refuse
that. The host scrubs those fields on startup; if a resume still fails,
`Open` a new session after `make image-update`.

## `missing credential XAI_API_KEY` (or OpenAI / Anthropic)

Use `/provider` in the TUI, `/credential` for Vault/Azure/AWS, or write
`~/.abox/credentials.env` (mode `0600`). Keychain entries use service
`abox`. Empty env vars are not used. `abox creds migrate` moves the file
into the keychain.

`abox --probe-vm` does not need a key. A missing key fails the turn, not
VM boot.

## Model `run_command` always errors in `abox exec`

Headless has no approver. Default is deny. Use the TUI, or
`Session.SetApprover` in the SDK. `Session.RunCommand` is a supervisor
RPC and does not go through that gate.

## Usage is always nil

xAI: the SDK omits `stream_options.include_usage` (unverified on that API).
OpenAI/Anthropic: need a completed turn (`Kind: result`). Streaming still
works without usage.

## `turn already in progress`

Only one `user_turn` at a time per VM. Wait for the previous `Turn` to return
(or cancel it) before starting another.

## `offline mode: provider access is disabled`

`connectivity.mode: offline` blocks host LLM and MCP HTTPS. Switch to
`direct` or `agentgateway`.

## Boot hangs then context deadline

Default `BootTimeout` is 45s. First boot after `make image` can be slower.
Raise `Options.BootTimeout`. Check `sessions/<id>/console.log`.

## Docker packer 404 / daemon down

Session path does not need Docker. Only `make image` / `make image-update`
does. Start Docker Desktop, or you cannot refresh `abox-guest` on the golden
disk.

## MCP login / token failures

- Server must already exist (`abox mcp add`)
- PAT path: set the env named in `credential_env`, then `abox mcp login`
- OAuth path: omit `credential_env`; the authorization server must advertise
  S256 PKCE. Without `client_id`, it needs `registration_endpoint`
- Tokens stay on the host. `SetMCPTokens` does not inject guest env

## Still stuck

- `sessions/<id>/console.log` — guest serial
- `abox --probe-vm` — VM without a model call
- GitHub: [AdminTurnedDevOps/ABox/issues](https://github.com/AdminTurnedDevOps/ABox/issues)
