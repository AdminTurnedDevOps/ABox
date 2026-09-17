---
layout: default
title: CLI and TUI
nav_order: 4
permalink: /cli/
---

# CLI and TUI
{: .no_toc }

1. TOC
{:toc}

`abox` is the supervisor. The agent still runs only in the microVM.

## Commands

```bash
abox                              # TUI, new session from the current directory
abox --resume <id>                # that session's root.raw
abox --model grok-default         # profile name from config.yaml
abox --probe-vm                   # boot + list_files; no model call
abox exec --prompt "…"            # headless JSON events on stdout
abox mcp add --mode <direct|agentgateway> [--credential-env NAME] <name> <url>
abox mcp login <name>
abox creds migrate                # credentials.env → macOS keychain
```

`--exec` is an alias of the `exec` subcommand. `exec` requires `--prompt`.
Headless uses the same agent, broker, and approval paths as the TUI. There
is no TUI approver, so model-authored `run_command` is **denied**. See
[Approvals]({{ '/approvals' | relative_url }}).

New sessions print their id. Resume always requires that id and does not use
the current directory, Git repository, branch, or `HEAD` to choose a session.

`--probe-vm` does not need a provider key. It is also the only path that
will still talk to a pre-protocol-4 guest (list files only).

## TUI

Full-screen instrument panel (Bubble Tea v2). Status rail:

| Field | Meaning |
| --- | --- |
| MODEL | `provider/model` of the selected profile |
| VM | `ready` / `failed` / `unavailable` / `not-started` |
| NET | `connectivity.mode` (`direct`, `agentgateway`, `offline`) |
| KEY | `key ok` / `no key` / `checking` for the selected model |

Transcript uses speaker gutters (`you`, `abox`, `tool`). A spinner sits in
the assistant gutter until the first token arrives.

Set `NO_COLOR` (any non-empty value) to disable color. Status still uses
glyphs (`●` / `○` / `◐`) so health survives a monochrome terminal.

### Keys

| Key | Action |
| --- | --- |
| Enter | Send (or confirm a picker / approval) |
| Shift+Enter / Alt+Enter | Newline in the prompt |
| Tab-complete `/` | Slash command list; ↑/↓ to move |
| Esc | Cancel picker / deny approval |
| Ctrl+C | Quit (deny an in-flight approval first) |
| Ctrl+D | Quit |

Footer hints change with the mode. `/help` lists slash commands.

### Slash commands

| Command | What it does |
| --- | --- |
| `/provider` | Pick Grok, OpenAI, or Anthropic and paste an API key |
| `/credential` | Point that model at Vault, Azure Key Vault, or AWS Secrets Manager |
| `/mcp` | List configured MCP servers and paste a Bearer token |
| `/help` | Print the list above into the transcript |

`/provider` and `/mcp` save the secret with
[SavePreferred]({{ '/credentials' | relative_url }}#where-keys-live):
macOS keychain first, `~/.abox/credentials.env` if the keychain is locked.
OAuth for MCP is `abox mcp login`, not the TUI.

`/credential` writes a **reference** into `config.yaml`. It does not store
cloud tokens. The host must already be able to talk to that backend.

### Approvals

When the model calls `run_command`, the TUI opens **APPROVAL REQUIRED**.
Deny is selected by default.

| Key | Action |
| --- | --- |
| ← / k | Deny |
| → / j | Allow once |
| Enter | Confirm |
| Esc | Deny |

There is no “remember for this session” yet. MCP tools do not prompt.

## Config

First run creates `~/.abox/` (mode 0700), `sessions/`, `images/`, and
`config.yaml` if they are missing. `ABOX_HOME` overrides that directory.
Leftover files from `~/Library/Application Support/ABox` are seeded once
and then scrubbed of secrets.

Default VM size is 1 vCPU and 768 MiB:

```yaml
resources:
  vcpu: 1
  ram_mib: 768
runtime:
  isolation: microvm
  backend: libkrun
  network: deny-by-default
  # image: /path/to/abox-guest.raw
  # vmm_path: /path/to/abox-vmm
```

SDK `Options.VCPU` / `RAMMiB` / `Image` / `VMMPath` override the file for
that process.
