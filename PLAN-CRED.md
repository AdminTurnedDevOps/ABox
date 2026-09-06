# ABox Credential Overhaul — Sources, Secrets-at-Rest Removal, Host Provider Broker

## Context

At the start of this overhaul, ABox violated the intended host-only LLM credential boundary and stored credential values in session configuration:

- All secrets lived plaintext in `~/.abox/credentials.env` (internal/credentials/credentials.go).
- `cfg.SecretsFromEnv()` collected **every** provider key + **every** MCP token; every session start dumped the full map into `sessions/<id>/guest-config.json` and `config.raw`. Session directories retained plaintext copies, and SDK `SetModel` re-sent all secrets.
- The untrusted guest read the config disk and exported everything into its environment, exposing every key rather than only the selected model credential.
- mcpauth persisted an unused `<ENV>_REFRESH` token without the client metadata required to refresh it.

User research (Sept 2026) recommends: host-side credential-source abstraction, resolve only the selected model's credential, never persist resolved values, remove secrets from guest config, and move provider transport behind a host broker. **User approved all three phases**, keychain via `security(1)` subprocess (no cgo — `abox` stays plain `go build`), Vault via `VAULT_ADDR`/`VAULT_TOKEN` KV v2.

**Approved source set (user decision, Sept 2026):** `env`, `keychain` (macOS), `vault` (HashiCorp Vault KV v2), `azure` (Azure Key Vault), `aws` (AWS Secrets Manager). All cloud stores via stdlib HTTP or a CLI subprocess — no HashiCorp/Azure/AWS SDKs, no cgo.

**Deferred (documented, not built):** Kubernetes sources, workload identity federation (Azure managed identity, AWS IAM roles — this milestone uses static SP/env credentials only), MCP traffic brokering (guest MCP client keeps its TSI path this milestone; PLAN.md §14.4 is the follow-up that removes MCP tokens from the guest), agentgateway LLM routing (stays "direct base_url" exactly as today — flagged, never claimed enforced, per PLAN.md §14.3).

## Global decisions

1. New host-only package `internal/credsource`; `internal/credentials` stays as the credentials.env file store (env-source backend + fallback writer). Import direction: `credsource` may import `config`; `config` never imports `credsource`.
2. Cloud secret stores via stdlib HTTP or CLI subprocess only — no HashiCorp/Azure/AWS SDK dependency, no cgo: Vault = one `GET /v1/<mount>/data/<path>` with `X-Vault-Token`; Azure Key Vault = stdlib OAuth2 client-credentials token POST plus `GET {vault}/secrets/{name}` Data Plane REST; AWS Secrets Manager = in-package SigV4 over `GetSecretValue` REST with static env credentials.
3. `_REFRESH` write: **delete it** (oauth.go:83). Future work note: persist client_id + refresh token in keychain, implement the refresh grant.
4. One protocol bump, `protocol.Version` 2 → 3, at Phase 3. Phase 2 needs no protocol change: v2 guests already implement `set_model`/`set_mcp_tokens` (cmd/abox-guest/main.go:238-259) and tolerate secretless boot config. Protocol-1 guests cannot run agent sessions from the rewritten secretless config; resume is rejected explicitly rather than reporting a misleading ready state.
5. Phase 3 is **version-gated, not a config mode**: proto ≥ 3 guest binaries have no direct provider transport (broker is the only LLM path); proto == 2 guests get the legacy post-hello secret push + stderr deprecation warning. No `model_transport` knob.
6. Phase 3 prerequisite: before the reader-goroutine demux, the host could not receive guest-initiated frames because `Sandbox.Call` read the connection inline and dropped frames outside its awaited ID. Task 3.1 supplied that demux before broker methods were enabled.
7. Guest context is up to 2 MiB (internal/agent/agent.go:27) but `MaxFrameBytes` is 1 MiB (protocol/protocol.go:14) → broker requests are chunked (mirror of `archive_chunk`), 256 KiB per chunk.

---

## Phase 1 — Host credential Source abstraction

### 1.1 `internal/credsource` core
New: `internal/credsource/credsource.go` (+test).
```go
type Reference struct{ Source, Name, Field, Version string }
type Value struct { Bytes []byte; Version string; ExpiresAt time.Time; LeaseID string }
func (v *Value) Zero()           // best-effort overwrite
func (v Value) String() string   // "credsource.Value(redacted)" — defeats accidental %v logging
type Source interface { Resolve(context.Context, Reference) (Value, error); Close() error }
type Resolver struct{ ... }      // registers env, keychain (darwin), vault
var ErrNotFound, ErrLocked error
```
Errors mention only Source/Name, never values.

### 1.2 env source
New: `internal/credsource/env.go` (+test). Order preserves current semantics: `os.Getenv(ref.Name)` first, then `credentials.Load()` map (reuse credentials.go:22). Remove now-unneeded `credentials.ApplyToEnv()` startup calls at cmd/abox/main.go:61 and pkg/abox/abox.go:59 (keep the one in `mcpLogin`, main.go:296, until 1.7).

### 1.3 keychain source (security(1), no cgo)
New: `internal/credsource/keychain.go` (+test). Service `abox`, account = `Reference.Name`.
- Get: `/usr/bin/security find-generic-password -s abox -a <NAME> -w` (password only on stdout; secret never in argv). ASCII-only storage documented (`-w` prints hex for non-ASCII).
- Set: run `/usr/bin/security -i`, write to **stdin**: `add-generic-password -U -s abox -a <NAME> -X <HEX> -j "managed by abox"`. `-i` = command-from-stdin mode; `-X` = hex password (avoids argv leak via ps and interactive quoting issues); `-U` upserts. Never use `-w value` (argv leak) or bare `-w` (tty prompt corrupts TUI).
- Delete: `security delete-generic-password -s abox -a <NAME>`.
- Errors: exit 44 → `ErrNotFound`; stderr `User interaction is not allowed` (locked/headless) → `ErrLocked` with unlock hint; else wrapped stderr (stderr never contains the secret).
- Subprocess behind package var `runSecurity` so tests fake it. `Available()` = darwin + `/usr/bin/security` exists. Export `SetKeychain`/`DeleteKeychain` for TUI/migration.

### 1.4 vault source
New: `internal/credsource/vault.go` (+test). `VAULT_ADDR`; token from `VAULT_TOKEN` then `~/.vault-token`; optional `VAULT_NAMESPACE` header. `Reference.Name` = KV v2 logical path (`secret/abox/anthropic`), source inserts `/data/`; `?version=` from `Reference.Version`; `Reference.Field` selects key in `data.data` (default `value`); `data.metadata.version` → `Value.Version`. 404 → ErrNotFound, 403 → clear permission error. `http.Client{Timeout: 15s}`.

### 1.4a azure source (Azure Key Vault)
New: `internal/credsource/azure.go` (+test). `Reference.Name` = Key Vault secret identifier URI (`https://<vault>.vault.azure.net/secrets/<name>`); specific version via `Reference.Version` (`/secrets/<name>/<version>`), else latest. `Reference.Field` unused (Key Vault secrets are single-value).
- Auth precedence: (1) service principal env — `AZURE_CLIENT_ID` + `AZURE_TENANT_ID` + `AZURE_CLIENT_SECRET` → stdlib OAuth2 client-credentials `POST {AZURE_AUTHORITY_HOST|https://login.microsoftonline.com}/<tenant>/oauth2/v2.0/token` with `scope=https://vault.azure.net/.default`; (2) fallback `az account get-access-token --resource https://vault.azure.net` subprocess (requires `az login`; mirrors keychain's subprocess pattern; behind package var `runAz` for test faking).
- Fetch: `GET {vaultUri}/secrets/{name}[/{version]}?api-version=7.5` with `Authorization: Bearer <token>`; response `{"value": ...}` → `Value.Bytes`. 404 → ErrNotFound, 403 → clear permission error mentioning Key Vault access policy/RBAC.
- Missing SP env **and** `az` unavailable → clear setup error (never mentions secret values). No Azure SDK, no cgo.

### 1.4b aws source (AWS Secrets Manager)
New: `internal/credsource/aws.go` (+test). `Reference.Name` = secret ID (arbitrary AWS name, may contain `/`). Static env credentials only: `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, optional `AWS_SESSION_TOKEN`; region from `AWS_REGION` then `AWS_DEFAULT_REGION`. Missing creds/region → clear setup error. No AWS SDK, no cgo.
- Fetch: `POST https://secretsmanager.<region>.amazonaws.com/` with `X-Amz-Target: secretsmanager.GetSecretValue`, `Content-Type: application/x-amz-json-1.1`, body `{"SecretId": <name>}`, signed with in-package SigV4 (service `secretsmanager`; `x-amz-security-token` included when a session token exists).
- Response: `SecretString` (or base64 `SecretBinary` decoded) → `Value.Bytes`. `Reference.Field` supported: when set, parse `SecretString` as JSON and select the key (cloud-manager convention of multi-key secrets); unset → whole `SecretString`. `ResourceNotFoundException` → ErrNotFound; `AccessDeniedException` → clear permission error.
- SigV4 signing is implemented in-package with `crypto/hmac` (canonical request, SHA256 payload hash, `Authorization: AWS4-HMAC-SHA256 ...`); token/secret never appear in error text or logs.

### 1.5 config.yaml credential references + back-compat
Modify: internal/config/config.go, providers.go, config_test.go.
```yaml
models:
  - name: claude-default
    provider: anthropic
    model: claude-sonnet-4-20250514
    credential:
      source: keychain          # env | keychain | vault | azure | aws
      name: ANTHROPIC_API_KEY   # env: var; keychain: account; vault: KV-v2 path; azure: secret URI; aws: secret ID
      field: api_key            # vault/aws only (vault default "value"; aws unset = whole SecretString)
      version: "4"              # vault/azure only (optional)
    # credential_env: X         # DEPRECATED alias == {source: env, name: X}
mcp_servers:
  - name: github
    url: https://...
    credential: {source: keychain, name: ABOX_MCP_GITHUB_TOKEN}
```
- `CredentialRef` struct in `config`; `Model.Credential *CredentialRef`, `MCPServer.Credential *CredentialRef`.
- `Model.CredentialReference()`: explicit ref, else `{env, CredentialEnv}`, else `{env, EnvName()}`. New `Model.EnvName()`: CredentialEnv, else canonical provider env from `DefaultProviders()`, else `ABOX_MODEL_<NAME>_KEY` — fills `protocol.GuestModel.CredentialEnv` (ToGuest, config.go:250) so Phase-1 wire format is unchanged. `MCPServer.CredentialReference()` reuses `TokenEnv` (config.go:376).
- Validate: reject both `credential` and `credential_env` set; source ∈ {env, keychain, vault, azure, aws}; env names pass `ValidEnvName`; `field` vault/aws only; `version` vault/azure only; azure `name` must be an `https://…vault.azure.net/secrets/…` (or other region suffix) URI.
- **Delete `SecretsFromEnv`** (config.go:281-300) + its test. Replace `Model.CredentialPresent` (config.go:396; sole caller tui.go:444) with resolver-backed presence check so keychain/vault keys don't render "missing". Presence is resolved **once when the picker opens** (cached per session), never in the render path — a `security` subprocess or Vault HTTP call per frame would freeze the TUI.
- `credsource.FromConfig(config.CredentialRef) Reference` glue.

### 1.6 Resolve only what's selected; fix SDK SetModel
New: `internal/credsource/resolve.go` (+test).
```go
// Selected model's credential keyed by model.EnvName() + one token per enabled
// MCP server keyed by TokenEnv. Missing model credential -> error naming the
// reference. Missing MCP token -> skipped (guest MCP degrades gracefully).
func ResolveSelected(ctx, *Resolver, config.File, config.Model) (map[string]string, error)
```
Uses `cfg.ResolvedMCPServers()` (config.go:307) — offline resolves no MCP tokens. Call sites: cmd/abox/main.go:124 and pkg/abox/abox.go:104 replace `cfg.SecretsFromEnv()`; pkg/abox/abox.go:246 `SetModel` resolves **only** the new model's credential. Zero intermediate `Value`s after copying (best-effort, documented).

### 1.7 TUI keychain-by-default, migration, mcpauth
Modify: internal/tui/commands.go (:47, :56), tui.go (:335-389, :444), internal/mcpauth/oauth.go (:57-86), cmd/abox/main.go.
- `applyProviderKey`: try `SetKeychain`; on success upsert the model's `credential: {keychain, ...}` ref and `cfg.Save()` (config.go:354); status "key saved to macOS keychain (service abox)". On ErrLocked/unavailable, fall back to `credentials.Save` and replace any stale explicit cloud/keychain reference with `credential: {source: env, ...}`. `applyMCPKey` mirrors this selected-source update.
- `mcpauth.LoginNamed`: same keychain-preferred writer; **delete the `_REFRESH` write**.
- New CLI `abox creds migrate` (dispatched like `mcp`, main.go:35): move credentials.env entries to keychain (including MCP OAuth tokens; re-login is the fallback for expired ones), update matching config refs, drop `*_REFRESH` keys, rewrite credentials.env to a comment (kept, 0600). No silent startup migration — env source keeps working indefinitely.

### Phase 1 verification
`go build ./...` && `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/abox-guest` && `go test ./protocol ./internal/... ./pkg/...` && `golangci-lint run ./...`.
Tests in existing style (single-purpose funcs, `t.Setenv("HOME"/"ABOX_HOME", t.TempDir())`): env precedence; keychain fake `runSecurity` asserting no secret in argv, exit-44 → ErrNotFound, locked → ErrLocked; vault httptest asserting path/version/token header/field, `~/.vault-token` fallback; azure httptest asserting token POST form + secret GET api-version + version path + fake `runAz` fallback + 404/403 mapping; aws httptest asserting SigV4 Authorization header shape, X-Amz-Target, SecretId body, session-token header, JSON `field` selection, 404/403 mapping; config alias mapping + both-set rejection + YAML round-trip; ResolveSelected selectivity + offline; credentials.env 0600 mode assertion; oauth_test asserts no `*_REFRESH` persisted.

---

## Phase 2 — Remove secrets at rest

### 2.1 Stop writing secrets to guest-config.json / config.raw
Modify: session.go:175-199 (`WriteGuestConfig` drops secrets param, never sets `GuestConfig.Secrets`), runtime.go:55-75 (`Prepare` drops secrets param), call sites main.go:124 / abox.go:104, tests session_test.go:13, runtime_test.go. `protocol.GuestConfig.Secrets` stays in the struct, commented Deprecated (old images still parse; old hosts still work).

### 2.2 Post-hello secret push
New: `Sandbox.PushSecrets(ctx, model, secrets)` — for protocol 2, `set_model` carries the selected model credential, then `set_mcp_tokens` carries MCP tokens; for protocol 3, `set_model` carries metadata only and only MCP tokens are pushed. It reuses SetModel/SetMCPTokens (runtime.go:352-361). Ordering: `runtime.Start` returns only after `waitHello` (runtime.go:183); callers push immediately after Start, **before** TransferArchive/first UserTurn (main.go:141-154, abox.go:124-138). Resume paths push too because `config.raw` is rewritten secretless. Protocol-1 agent startup and resume are rejected; only a diagnostic no-key probe may use an old image.

### 2.3 Scrub existing session dirs
New: `internal/session/scrub.go` (+test). `ScrubSecrets(root)` — surgical, never deletes sessions:
1. Walk `sessions/<id>/` (pattern from `LatestForRepo`, session.go:94).
2. `guest-config.json`: unmarshal to `map[string]json.RawMessage` (preserves unknown fields); no `"secrets"` key → skip (idempotent); else delete key, tmp+rename write, 0600.
3. `config.raw`: chmod 0600 → write scrubbed JSON zero-padded to 1 MiB → chmod 0400. Extract the pad-write body of `writeConfigDisk` (runtime.go:84-95) into shared `session.WritePaddedConfig` and have runtime reuse it (runtime already imports session).
4. root.raw / transcript.json / console.log / session.json untouched.
Invoke once per startup (main.go `run()` and pkg/abox `open()` after `config.Load()`); stderr "abox: scrubbed plaintext secrets from N old session(s)" when N > 0. An incomplete scrub is returned as a startup error by both CLI and SDK rather than allowing a session to proceed with uncertain secret removal.

### 2.4 Old-image compatibility (decision)
Protocol-2 images supported via the 2.2 push; `make image` required only for protocol < 2. Rationale: v2 guests already implement both push handlers and tolerate secretless boot config.

### Phase 2 verification
Build/test/lint gates as Phase 1. session_test: no `"secrets"` key + 0600 mode. runtime_test: keep 0400 resume assertion (runtime_test.go:61); config.raw contains no secret marker. scrub_test: legacy fixture scrubbed, modes preserved, other fields byte-identical, second run no-op, root.raw untouched. net.Pipe fake-guest test (runtime_turn_test.go style): PushSecrets emits set_model then set_mcp_tokens before any user_turn; protocol-1 agent startup/resume is refused. Manual: `make build && make image`, run a session, `strings ~/.abox/sessions/<id>/config.raw | grep -c API_KEY` → 0.

---

## Phase 3 — Host provider broker

### 3.1 Sandbox reader-goroutine demux (prerequisite; land as its own PR)
Modify: runtime.go (Call :226, userTurnLocked :270, waitHello :190, Stop :383). After hello, a single `readLoop()` goroutine owns the conn and routes: response frames → per-call channel keyed by ID (subsumes inline loops incl. late-cancel logic tested at runtime_turn_test.go:63-256); `agent_event` → active turn's channel; guest-originated frames (ID prefix `g-`, Method set) → `GuestCallHandler`:
```go
type GuestCallHandler interface {
    Handle(ctx context.Context, method string, params json.RawMessage,
           notify func(method string, params any) error) (any, *protocol.Error)
}
```
Unknown guest methods → typed error frame. **Each guest call is dispatched on its own goroutine** — never inline on readLoop (a `provider_send` Last=true otherwise blocks the loop for the whole provider stream, deadlocking `provider_cancel` and every other frame); `notify` and reply frames serialize through `writeMu`. All existing turn/cancel tests must stay green.

### 3.2 Protocol types, Version = 3
Modify: protocol/protocol.go (+test). Methods: `provider_open`, `provider_send`, `provider_cancel` (guest→host); `provider_event` (host→guest push).
```go
const Version = 3
const MaxProviderChunk = 256 << 10
const MaxProviderToolArgs = 512 << 10
const MaxProviderStreams = 2

type ProviderOpenParams struct{ Model string; Rich bool }   // configured alias ONLY — no URL/headers/credential names
type ProviderOpenResult struct{ StreamID string }
type ProviderSendParams struct{ StreamID string; Data []byte; Last bool } // chunks of marshaled ProviderRequest
type ProviderRequest struct{ Messages []ProviderMessage; Tools []ProviderToolSchema }
type ProviderCancelParams struct{ StreamID string }
type ProviderEventParams struct{ StreamID, Type, Text, ToolID, ToolName, ToolArgs string; Usage *UsageInfo; StopReason, Err string }
```
`ProviderMessage`/`ProviderToolSchema` mirror `provider.Message`/`ToolSchema` (protocol imports no internal packages). Bounds host-side: ≤ MaxProviderStreams open, reassembled request ≤ 4 MiB, tool args truncated at MaxProviderToolArgs (error event beyond), 5-min idle stream timeout, cancel path on every stream. Broker methods accepted only post-hello from proto ≥ 3 guests. `SetModelParams.Secrets` documented unused for LLM at proto ≥ 3.

### 3.3 Provider transport host-callable
Modify: internal/provider/provider.go. Inject credential + client: `Stream(ctx, model, key string, client *http.Client, ...)` / same for `StreamWithUsage` — deletes `os.Getenv(model.CredentialEnv)` reads (:48, :72) and the `egress.Client()` default (:19). SSE loops (:164-229, :310-374) reused verbatim; `provider.Event` maps 1:1 onto ProviderEventParams. Host client `Timeout: 5m`; request URL built exclusively from cfg — guest input contributes only the alias.

### 3.4 Host LLM broker
New: `internal/llmbroker/broker.go` (+test); wire as `sb.OnGuestCall` in cmd/abox/main.go + pkg/abox. `Broker{cfg, resolver}`:
- `provider_open`: reject offline; `cfg.ModelNamed` lookup (config.go:384), unknown alias → error; allocate stream.
- `provider_send`: budget-checked append; on Last, unmarshal, **resolve credential at call time** via `Resolve(model.CredentialReference())`, call `provider.StreamWithUsage`, fan events into `notify("provider_event", ...)`; zero Value after request build; terminal done/error + cleanup.
- `provider_cancel`: cancel HTTP context.
- agentgateway mode: LLM keeps configured base_url exactly as today (current code never routed LLM through the gateway either); gateway LLM adapter is follow-up, fail-closed noted for `enforcement: required` once it exists.
- Logs stream lifecycle only (alias, status, byte counts) — never headers/bodies.

### 3.5 Guest broker client + agent hook
New: `internal/guest/brokerclient/client.go` (+test). Modify: cmd/abox-guest/main.go, internal/agent/agent.go.
- `agent.Loop` gains `Stream func(...) (<-chan provider.Event, error)`; `Turn` (agent.go:69-104, call sites :73/:75) uses it instead of calling provider directly; event-consumption loop untouched.
- brokerclient: `provider_open` (IDs `g-1…`), chunked `provider_send` (mirror TransferArchive, runtime.go:363-381), `provider_event` → `provider.Event`, `provider_cancel` on ctx cancel.
- Guest read loop (main.go:93-127): route response frames (empty Method) → brokerclient pending map; `provider_event` → stream dispatch. `set_model` ignores Secrets for the model; boot `applySecrets(cfg.Secrets)` (main.go:46) deleted in proto-3 guest; MCP tokens still via `set_mcp_tokens`.
- Mixed-binary fail-fast: guest learns the host's protocol in the hello exchange; if host < 3, refuse model calls with "host binary too old; run make build" (an old host's inline `Call` loop silently discards guest-initiated frames, so without this a proto-3 image against an old `abox` hangs — exactly the state after `make image` without `make build`).
- Hardening: drop the three provider hosts from `defaultAllowed` in internal/guest/egress/egress.go:16-20 — proto-3 guests need TSI egress only for configured MCP URLs.

### 3.6 Host stops pushing LLM secrets to proto-3 guests
Modify: PushSecrets/SetModel (runtime.go:352-361), abox.go:246, tui.go:379-380. Proto ≥ 3 → `set_model` carries model only; proto == 2 → legacy full push + stderr deprecation ("run make image"). On proto ≥ 3, `/provider` keys stay host-side entirely.

### Phase 3 verification
Gates as before + `make build && make image` smoke (`abox --probe-vm`, then a real turn). protocol_test: round-trip new types; oversized provider_send rejected. runtime_turn_test additions: guest `provider_open` mid-`user_turn` — both complete (the demux test); unknown guest method → typed error; all existing tests green. broker_test: httptest SSE provider asserting Authorization/x-api-key built host-side; unknown-alias reject; offline reject; cancel aborts HTTP; chunk budget; credential re-resolved per call (rotate fixture). agent_test: Turn with fake Stream (no HTTP). brokerclient net.Pipe test: chunking, reassembly, cancel.

---

## Documentation updates
- PLAN.md §4.1 and §13.4 distinguish protocol-3 host-only LLM credentials from guest-held MCP tokens and the protocol-2 legacy push. The §2 model-traffic decision and README security story describe the implemented protocol-3 host broker while retaining compatibility `base_url` metadata in the guest config.

## Risks / notes
1. Demux refactor (3.1) touches every RPC path incl. cancel edge cases — own PR, existing suite green before broker methods.
2. Keychain headless/SSH (`abox exec`, locked keychain) → ErrLocked with guidance; use env-source refs in CI. Document.
3. MCP tokens still enter the guest this milestone — accepted; §14.4 brokering is the follow-up.
4. Zeroing is best-effort (Go GC copies); stated in package docs.
5. Old `abox` binary resuming a scrubbed session re-writes secrets into config.raw (old Prepare); next new-binary start re-scrubs. Mixed-binary users only.
6. Tool args > 512 KiB → error event (today's ceiling was 1 MiB); named const, acceptable.

## Integration status

- Protocol 3 rich turns request usage through the guest broker client and return accumulated usage and stop reason through the SDK `TurnResult`.
- Protocol 3 provider HTTPS and LLM authentication are host-brokered. LLM credential values are absent from the guest and session config; compatibility model metadata, including `base_url` and the credential environment-variable name, remains on the guest config disk but is not trusted for protocol-3 routing.
- Startup credential resolution is partial: successfully resolved MCP tokens are pushed even if the selected model credential is missing. Missing optional MCP tokens are skipped; other source failures are reported after the partial push. Interactive CLI may continue after reporting the error; headless CLI and SDK startup return it.
- Protocol-1 resume is rejected after the host rewrites `config.raw` without secrets. Protocol 2 remains the legacy secret-push path; protocol 3 keeps LLM credentials host-side.
- MCP tokens still enter guest memory through `set_mcp_tokens`; MCP traffic and credential brokering remain follow-up work.
- Session scrubbing reports aggregate per-session errors and aborts CLI/SDK startup if any legacy session could not be scrubbed.
- Azure and AWS credential-source authentication is limited to static host credentials (or an existing Azure CLI login); managed/workload identity is deferred.
- `llmbroker.Broker` still has no config-update API. The SDK and TUI therefore install a newly constructed broker after an idle model change so subsequent streams use current aliases, base URLs, and credential references. A broker-owned atomic `UpdateConfig` API would remove direct handler replacement and better define concurrent SDK `SetModel` behavior.
- Isolation remains **Planned** until the named hardware tests pass.
