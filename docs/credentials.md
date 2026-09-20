---
layout: default
title: Credentials
nav_order: 5
permalink: /credentials/
---

# Credentials
{: .no_toc }

1. TOC
{:toc}

LLM keys and MCP tokens stay on the **host**. The guest never receives them.
`config.yaml` stores a pointer (`credential.source` + `name`), not the secret.

{: .important }
Do not put API keys in `config.raw`, git, or tests. Startup scrubs leftover
plaintext secrets out of old session files.

## Sources

| Source | `name` is | Auth |
| --- | --- | --- |
| `env` | environment variable (also reads `~/.abox/credentials.env`) | — |
| `keystore` | macOS Keychain or Linux Secret Service account (service `abox`) | Linux: `secret-tool`, `gdbus`, session bus/provider |
| `vault` | Vault KV v2 path (`secret/abox/anthropic`) | `VAULT_ADDR` + `VAULT_TOKEN` (or `~/.vault-token`) |
| `azure` | Key Vault secret URI | `AZURE_CLIENT_ID` / `AZURE_TENANT_ID` / `AZURE_CLIENT_SECRET`, or `az login` |
| `aws` | Secrets Manager secret id | `AWS_ACCESS_KEY_ID` + `AWS_SECRET_ACCESS_KEY` (`AWS_REGION`), or `~/.aws/credentials` |

```yaml
models:
  - name: claude-default
    provider: anthropic
    model: claude-sonnet-4-20250514
    credential:
      source: keystore            # env | keystore | vault | azure | aws
      name: ANTHROPIC_API_KEY     # env var, OS-keystore account, vault path, Azure URI, or AWS id
      # field: value              # vault/aws only
      # version: "4"              # vault/azure only
    base_url: https://api.anthropic.com
```

`credential_env: XAI_API_KEY` is the same as `{source: env, name: XAI_API_KEY}`.
Set either `credential` or `credential_env`, not both.

Each destination env name must be unique across models and MCP servers.

## Where keys live

`/provider` and `/mcp` in the TUI, and `abox mcp login`, call
`SavePreferred`:

1. OS keystore, service `abox`, account = env name
2. If unavailable, locked, or timed out: warned plaintext fallback at
   `~/.abox/credentials.env` (mode 0600)

```bash
abox creds migrate    # move existing credentials.env entries into the OS keystore
```

On macOS, `keystore` uses Keychain through `security(1)`. On Linux, it uses
Secret Service through `secret-tool` and a bounded `gdbus` availability/item
probe. GNOME Keyring, KWallet (`org.kde.secretservicecompat`), and KeePassXC
can provide that service. Values are sent to `secret-tool store` on stdin with
no trailing newline and never placed in argv.

If there is no session bus/provider, an item is locked, the provider disappears,
or an unlock prompt exceeds the timeout, ABox warns and uses the 0600 file
fallback. `keychain` and `secretservice` are accepted input aliases for existing
config; saved config canonicalizes them to `keystore`.

For headless Linux, prefer `vault`, `azure`, or `aws` when plaintext fallback is
not acceptable. These cloud sources are cross-platform and do not depend on
Secret Service.

Refresh-token leftovers (`*_REFRESH`) are dropped during migrate. Re-login
when an MCP access token expires; ABox does not persist refresh tokens.

## Cloud references

`/credential` in the TUI writes a Vault / Azure / AWS pointer into
`config.yaml`. It does **not** store the cloud login token.

| TUI choice | You type | Host already needs |
| --- | --- | --- |
| HashiCorp Vault | KV v2 path (`secret/abox/anthropic`) | `VAULT_ADDR` + `VAULT_TOKEN` or `~/.vault-token` |
| Azure Key Vault | `https://myvault.vault.azure.net/secrets/name` | service principal env, or `az login` |
| AWS Secrets Manager | secret id (`abox/anthropic`) | `AWS_*` env, or `~/.aws/credentials` (+ region from `~/.aws/config`) |

Azure accepts first-party Key Vault DNS suffixes only: public
(`vault.azure.net`), US Gov, China, and Germany. Optional version is the
URI's last path segment or `credential.version`.

No HashiCorp / Azure / AWS SDKs: stdlib HTTP or a CLI subprocess.

## Default model profiles

Created on first run if `config.yaml` is missing:

| Name | Provider | Model | Env |
| --- | --- | --- | --- |
| `grok-default` | xai | grok-4 | `XAI_API_KEY` |
| `openai-default` | openai | gpt-4.1 | `OPENAI_API_KEY` |
| `claude-default` | anthropic | claude-sonnet-4-20250514 | `ANTHROPIC_API_KEY` |

Edit `~/.abox/config.yaml` to change the model id per provider. Pick one with
`/provider` or `--model grok-default`. A missing key fails the **turn**, not
VM boot (`abox --probe-vm` still works).

`connectivity.mode: offline` disables provider HTTPS on the host broker.
