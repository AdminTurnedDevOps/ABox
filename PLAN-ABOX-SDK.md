# ABox Go SDK (v0.2 scope) — Implementation Plan

## Context

ABox's host orchestration is already SDK-shaped — `cmd/abox/main.go` composes `config.Load` → `session.Create/LatestForRepo` → `repository.OpenForSession/ArchiveHEAD` → `runtime.Prepare/Start` → `Sandbox.UserTurn(ctx, prompt, onEvent)` — but it is all locked behind `internal/` and consumable only via the CLI. Goal: a public Go SDK package so other Go programs can embed ABox (spawn a microVM-isolated agent session, stream events, cancel turns, read usage/cost metadata), per user decisions:

- **Scope: full v0.2** — extraction + configurable turn options + mid-turn cancellation + richer result metadata (token usage, stop reason, tool args/IDs).
- **Purely additive to the CLI** — `cmd/abox`, `internal/tui`, `cmd/abox-vmm` untouched. The v0.2 features require *additive* changes to `protocol`, `cmd/abox-guest`, `internal/agent`, `internal/provider`, `internal/runtime`, `internal/guest/tools` (new fields/methods only; existing signatures and behavior preserved). Golden image rebuild via existing `make image-update` is a required deploy step.

## Additive boundary (exact)

| Untouched | Additive changes | New |
|---|---|---|
| `cmd/abox`, `internal/tui`, `cmd/abox-vmm`, `internal/vmmconfig`, images/, Makefile targets (except maybe `test` path glob) | `protocol/protocol.go`, `cmd/abox-guest/main.go`, `internal/agent/agent.go`, `internal/provider/provider.go`, `internal/runtime/runtime.go`, `internal/guest/tools/tools.go` | `pkg/abox/` (SDK), `examples/sdk-basic/`, tests |

Existing exported functions keep their signatures. New capability = new methods/fields. Old TUI ignores unknown JSON fields (std `encoding/json` behavior), and rich events are **opt-in per turn**, so a stock CLI against a new guest behaves identically.

## Critical compatibility constraint: resumed sessions run old guests

`--resume`/SDK resume boots the session's existing `root.raw`, cloned at session creation — `make image-update` patches only the *golden* image. So the SDK will routinely talk to guests that predate v0.2. Mandatory handling:

**Scope of the degradation: transitional only.** It applies solely to sessions created *before* the golden image was rebuilt with the v2 guest. Once a user runs `make build && make image-update` (or a fresh install runs `make image`), every session created from then on carries the v2 guest, and resume of those sessions is fully featured — cancel, rich events, turn options. Fresh installs never see the degraded path at all. The handling below exists only so resuming a pre-upgrade session fails explicitly instead of misbehaving.

- Bump `protocol.Version` to 2. Guest sends its version in the existing `HelloParams.Protocol` field.
- `runtime.waitHello` currently discards `hello.Protocol` (runtime.go:166-189) — store it on `Sandbox` (new field `GuestProtocol int`), expose via method.
- SDK degrades gracefully on a v1 guest: plain events, no cancel, no turn options; `Session.Capabilities()` reports what's available; `Turn` with cancel/options against v1 guest → typed error `ErrGuestTooOld` (or silent degrade — pick typed error, explicit beats silent).
- Stray-frame note for the implementer: if a `cancel_turn` frame does reach an old guest, it replies "unknown method" for that frame ID after the turn; the host turn-reader loop already drops non-matching IDs (see `UserTurn` loop, runtime.go:236-257) — harmless, do not "fix".

## Phase 1 — `pkg/abox` facade (pure extraction, no protocol changes)

New package `pkg/abox` (matches user's pkg/ convention). Extraction source: `cmd/abox/main.go:32-180` (`run()`) and `main.go:209-222` (`runExec`). Do **not** modify main.go; replicate orchestration in the SDK (acknowledged duplication — user chose additive; note it in the SDK doc comment).

API (callback style, mirroring `Sandbox.UserTurn` — no channel API):

```go
package abox // import "github.com/AdminTurnedDevOps/ABox/pkg/abox"

type Options struct {
    RepoPath  string        // default: cwd
    Model     string        // profile name; "" = config default (config.ModelNamed(""))
    Image     string        // "" = config.GuestImagePath() / cfg.Runtime.Image
    VMMPath   string        // "" = lookPath("abox-vmm")
    VCPU, RAMMiB int        // 0 = cfg.Resources.Resolved()
    BootTimeout  time.Duration // default 45s (matches CLI)
}

func Open(ctx context.Context, opts Options) (*Session, error)          // new session: snapshot repo, clone disk, boot, transfer archive
func Resume(ctx context.Context, sessionID string, opts Options) (*Session, error) // "" = latest for repo (session.LatestForRepo via repository.TopLevel)

type Session struct { /* wraps *runtime.Sandbox + *session.Session */ }
func (s *Session) ID() string
func (s *Session) Turn(ctx context.Context, prompt string, onEvent func(Event)) (*TurnResult, error) // Phase 1: ctx = deadline only; Phase 3: real cancel
func (s *Session) History() []protocol.HistoryLine
func (s *Session) SetModel(ctx, model string) error        // resolves profile via config, wraps Sandbox.SetModel
func (s *Session) SetMCPTokens(ctx, secrets map[string]string) error
func (s *Session) ExportPatch(ctx) (patch, summary string, err error) // wraps "export_patch" RPC
func (s *Session) ListFiles / ReadFile / RunCommand(...)   // thin wrappers over Sandbox.Call builtins (probe-vm equivalent)
func (s *Session) Close() error                            // Sandbox.Stop
type Event = protocol.AgentEvent                           // alias; extended in Phase 2
```

Reuse directly (no copies): `config.Load`, `credentials.ApplyToEnv`, `cfg.ResolvedMCPServers`, `cfg.SecretsFromEnv`, `session.*`, `repository.*`, `runtime.Prepare/Start`. Errors wrapped `fmt.Errorf("context: %w", err)` per repo convention. `Open` fails with the existing actionable messages (missing image → "run: make image").

Files: `pkg/abox/abox.go`, `pkg/abox/session.go`, `pkg/abox/doc.go`, `pkg/abox/abox_test.go`.

## Phase 2 — Protocol v2: rich events, turn options, usage metadata

**Protocol (`protocol/protocol.go`, additive):**
- `Version = 2` (host accepts 1 and 2).
- `UserTurnParams` += `MaxTurns int`, `TimeoutSec int`, `RichEvents bool` (all `omitempty`; old guest ignores; enforcement gated on `GuestProtocol >= 2`).
- `AgentEvent` += `ToolID string`, `ToolArgs string` (`omitempty`; populated only when `RichEvents`).
- New event kind `"result"` carried in a new struct or on AgentEvent: `Usage *UsageInfo`, `StopReason string`. `UsageInfo{InputTokens, OutputTokens int}` — **all optional/best-effort**.

**Provider (`internal/provider/provider.go`, additive):**
- `Event` += `Usage *UsageInfo`, `StopReason string`.
- OpenAI-compat path: request `stream_options: {"include_usage": true}` **only when the caller opts in** (new low-risk approach: always safe for OpenAI; xAI support unverified — implementation step: verify against xAI docs/live API; if unsupported, omit for provider "xai" and leave usage nil). Parse trailing `usage` chunk.
- Anthropic path: parse `message_start` (`usage.input_tokens`), `message_delta` (`usage.output_tokens`, `stop_reason`).
- Ship the README's long-standing TODO here: `httptest` fixtures in `provider_test.go` for stream/tool/usage/error cases (canned SSE bodies; no live API in tests).

**Agent loop (`internal/agent/agent.go`, additive):**
- `Loop` += `MaxTurns int` (0 = existing 16), accumulate usage across iterations, emit final `AgentEvent{Kind: "result", Usage, StopReason}` when rich events requested (new field `Rich bool` on Loop or param threaded from `user_turn`).
- Existing plain-event behavior byte-identical when `RichEvents` false.

**Guest (`cmd/abox-guest/main.go`):** thread `UserTurnParams.MaxTurns/TimeoutSec/RichEvents` into the loop per turn.

## Phase 3 — Mid-turn cancellation

Core problem (both sides currently block):
- Guest: read loop blocks inside `handleTurn` (main.go:95-100) — never sees frames during a turn.
- Host: `Sandbox.UserTurn` holds `s.mu` for the whole turn; no way to write a second frame.

**Guest refactor (`cmd/abox-guest/main.go`):**
- `user_turn` runs in a goroutine with `context.WithCancel`; read loop keeps reading. Track active turn: `map[frameID]context.CancelFunc` (only one concurrent turn expected; reject a second `user_turn` while one is active with a typed error).
- All conn writes go through a `sync.Mutex`-guarded writer (turn goroutine emits `agent_event`s concurrently with RPC replies).
- New method `cancel_turn` `{ "id": "<turn frame id>" }` → calls the cancel func; turn goroutine returns error frame `{code: "canceled"}` for the turn ID.
- `provider.Stream` already takes ctx (HTTP request canceled mid-stream) → cancellation is immediate during model streaming.

**Tool cancellation (`internal/guest/tools/tools.go`, additive):**
- `Run` is timeout-only (`runWithTimeout`, tools.go:392-406). Add ctx-aware variants: `RunContext(ctx, ...)` and `runWithTimeout` gains a sibling selecting on `ctx.Done()` → `Process.Kill()`. **Chosen semantic: cancel kills an in-flight `run_command`** (and interrupts provider streaming); read/list/search finish naturally (fast, bounded).
- Additive `CallToolCtx(ctx, ...)`; existing `CallTool` delegates with `context.Background()`. Agent loop `execTool` uses the ctx path.

**Host (`internal/runtime/runtime.go`, additive):**
- Split locking: new `writeMu` guards frame writes; existing `mu` becomes the request/turn serializer. Existing `Call`/`UserTurn` behavior unchanged.
- New method `UserTurnCtx(ctx, text, opts TurnOptions, onEvent) (*TurnOutcome, error)`: sends `user_turn` (with v2 params), reads frames; on `ctx.Done()` writes `cancel_turn` (via `writeMu`) and keeps reading until the turn's final frame. Gate on `GuestProtocol >= 2`, else `ErrGuestTooOld`.

**SDK:** `Session.Turn` uses `UserTurnCtx` when guest is v2; ctx cancellation = clean cancel; returns `*TurnResult{Text, Usage, StopReason, Canceled bool}`.

## Phase 4 — Docs, example, release plumbing

- `examples/sdk-basic/main.go`: open session, run a turn with live event printing, cancel on SIGINT, print usage/cost fields, export patch, close.
- README: new "Go SDK" section (import path, minimal snippet, guest-version caveat re: resume + `make image-update`).
- `pkg/abox/doc.go` package docs. Note the Apple Silicon + libkrun + golden image runtime requirements up front.
- Makefile `test` target already globs `./internal/...` — extend to `./protocol ./internal/... ./pkg/...`. (Single-line additive change; call out to user.)

The docs should be built in GitHub Pages

## Testing strategy (no VM required for CI)

1. **Fake guest over `net.Pipe`**: protocol is pure Go — implement a scripted fake guest speaking length-prefixed frames (`protocol.WriteFrame/ReadFrame`) in `internal/runtime/runtime_test.go` + `pkg/abox` tests. Covers: hello version negotiation (v1 vs v2), `UserTurnCtx` happy path, cancel-write interleaving (cancel frame arrives while agent_events streaming), old-guest fallback/`ErrGuestTooOld`, stray unknown-method reply dropped.
2. **Provider httptest fixtures**: SSE bodies for text-only, tool call, usage chunk (OpenAI + Anthropic shapes), error status, malformed chunks.
3. **Agent loop tests**: extend `agent_test.go` pattern — MaxTurns enforcement, usage accumulation, rich vs plain event parity.
4. **Tools**: `RunContext` cancel kills a `sleep`-style process (guarded for linux vs darwin — existing `freeze_stub.go` pattern shows the build-tag approach; run tests run fine on darwin with /bin/sh).

## Verification (end-to-end, on this machine)

1. `gofmt -w`, `golangci-lint run` (repo convention), `make test` — all green.
2. `make build && make image-update` (rebuild guest binary onto golden image).
3. `abox --probe-vm` — CLI still works (untouched code path, proves no internal regression).
4. `go run ./examples/sdk-basic` in a test repo with a provider key: full turn streams events, usage populated; Ctrl+C mid-turn → clean cancel, VM shuts down.
5. Resume an SDK session (`Resume("")`) — degrades or works per guest version.
6. Manual TUI smoke test (`abox`, one prompt) — behavior identical to pre-change.

## Execution order

Phase 1 → 2 → 3 → 4; each phase independently verifiable (1: fake-guest tests + example without rich features; 2: provider fixtures; 3: cancel tests; 4: docs). Suggested commits per phase.

## Known risks / notes

- xAI `stream_options` support unverified — usage stays nil for xAI if unsupported; verify during Phase 2.
- Guest concurrency refactor is the riskiest change (Phase 3); the write-mutex + single-active-turn constraint keeps it small. Old plain-event path must remain frame-for-frame identical (fake-guest test asserts it).
- SDK duplicates ~100 lines of orchestration from `cmd/abox/main.go` by user's explicit "purely additive" choice; future follow-up could flip the CLI onto the SDK.
