# ABox Implementation Plan

## 1. Purpose

ABox is a terminal-native agent harness comparable to OpenCode, Grok Build,
Claude Code, and Codex CLI. It is its own harness and does not wrap or launch
another coding-agent harness.

Its defining property is microVM-native agent execution:

> The agent loop, prompt and context construction, built-in tools,
> model-authored commands, generated code, and repository effects run inside
> the microVM. Provider HTTPS and remote Streamable HTTP MCP are performed by
> narrow host brokers using host-held configuration and credentials. The host
> also owns the CLI/TUI/SDK, VM and session orchestration, approvals, and any
> future reviewed patch import. Model-controlled code and repository effects
> remain guest-only.

ABox's first milestone must provide:

- An agent loop
- Model selection and routing
- Context management and compaction
- Skills and repository instructions
- Tool definitions
- MCP client functionality
- Sessions and memory
- A terminal UI
- Approval workflows
- MicroVM lifecycle, checkpointing, rollback, and forking
- Optional agentgateway integration

Implementation remains incremental, but the first milestone is not complete
until every capability above has a working, tested implementation. The first
vertical slice establishes the secure prompt-to-patch path; subsequent phases
within the same milestone add context compaction, skills, MCP, persistent
sessions and memory, and cold checkpoint, rollback, and fork operations. The
vertical slice is an implementation increment, not a release or shippable
product subset.

Treat this milestone operationally as a 0.1/1.0 release program. Phases are
internal development checkpoints for dogfooding and review. They are not
separately shippable products and they do not reduce the section 23
acceptance bar.

## 2. Current Decisions

The current plan makes these decisions:

| Area | Decision |
| --- | --- |
| Implementation language | Go 1.25 |
| Go module | `github.com/AdminTurnedDevOps/ABox` |
| License | Apache-2.0 |
| Initial host | macOS on Apple Silicon |
| Initial guest | ARM64 Linux |
| Initial microVM backend | libkrun over Apple Hypervisor.framework |
| Runtime integration | Dedicated `abox-vmm` Go helper with a narrow cgo boundary |
| Guest network | No guest NIC and no TSI inet or Unix hijacking; RPC uses vsock only. Hardware isolation remains unverified |
| Model traffic | Protocol-4 host LLM broker; the guest supplies a configured model alias and bounded request data |
| Remote MCP traffic | Protocol-4 host Streamable HTTP broker; the guest supplies configured server/tool identities and arguments, never endpoints or credentials |
| Providers | OpenAI and xAI through Chat Completions today; Anthropic through Messages. OpenAI/xAI Responses remain Planned |
| Repository state | Git worktree required. Clean trees archive `HEAD`; dirty or unborn trees use a private ephemeral snapshot |
| Host workspace sharing | Prohibited |
| Repository transfer | Private snapshot copied into a writable guest disk |
| Change return | Guest patch export is implemented. Reviewed host import remains Planned |
| TUI framework | Bubble Tea v2, Bubbles, and Lip Gloss v2 |
| TUI style | Full-screen near-black interface with restrained status colors |
| Public SDK | `pkg/abox`, including Open, Resume, Turn, cancellation, capabilities, approvals, probe methods, and patch export |
| Host-guest protocol | Protocol 4: protocol 3 introduced the host LLM broker; protocol 4 added the host MCP broker and `run_command` approval |
| Approval state | Model-authored `run_command` supports deny or allow-once and defaults to deny. Broader approvals remain Planned |
| Session resume | Existing `root.raw`, guest conversation context, and TUI transcript can be resumed. Full event persistence, memory, and checkpoints remain Planned |
| agentgateway | Implemented as an MCP-origin policy mode; a dedicated LLM gateway adapter remains Planned |
| Connectivity broker | Host LLM and remote Streamable HTTP MCP brokers are implemented. Package-index brokering remains Planned |
| Package-manager compatibility | Planned origin rewrite to a guest loopback adapter, not HTTP(S) proxying |
| Instruction loading | Planned; scoped `AGENTS.md`, global instructions, and skills are not implemented |
| Repo instruction authority | Repo text cannot change policy, limits, connectivity, or tools |
| libkrun isolation profile | Product code uses no net calls, explicit vsock with flags 0, two raw disks, and no host-path virtio-fs; hardware behavior remains unverified |
| Model-visible tools | Five built-in ABox tools plus dynamically discovered MCP tools. No `write_file` |
| Headless operation | `abox exec` uses the same guest agent and brokers; without an approver, model-authored `run_command` is denied |
| Default VM resources | 1 vCPU and 768 MiB RAM; upper resource limits and acceptance measurements remain Planned |
| VM concurrency | No global limit is enforced today; the target is one running VM by default. Checkpoint and fork orchestration are not implemented |
| Background services | No resident ABox daemon |

## 2.1 Resource Efficiency

Low local resource use is a product requirement and a milestone acceptance
criterion. Hardware isolation must not be implemented by stacking ABox inside
Docker, a container engine, Kubernetes, or another agent harness.

### Design Rules

- Use libkrun directly through the narrow `abox-vmm` helper.
- Run no Docker engine, SSH daemon, systemd instance, cloud-init service,
  graphical stack, audio device, GPU device, or general-purpose VM manager.
- Use a minimal guest init that starts `abox-guest` directly.
- Configure one vCPU and 768 MiB RAM by default.
- Allow smaller profiles down to the measured minimum for repository browsing
  and patching.
- Require explicit configuration or approval before granting a session more
  CPU, memory, disk, or concurrent VMs.
- Keep at most one VM running by default. Checkpoints and forks are cold disk
  states until the user selects one.
- Start the VM on demand. If repository instructions and configured MCP
  servers do not require the guest before the first model turn, prepare the VM
  concurrently and boot it only before the first effectful tool call.
- Stop an idle VM after a configurable inactivity period while preserving its
  private disk and resumable session state.
- Terminate the helper and release VM resources immediately when the session
  is destroyed or ABox exits.
- Use one verified immutable base image and APFS copy-on-write clones for
  session disks. Fall back to a full copy only when clone support is absent.
- Stream provider, RPC, command, search, and patch data rather than buffering
  unbounded results in memory.
- Keep bounded TUI scrollback and spill retained session events to compact
  on-disk records.
- Compact model context incrementally instead of retaining and resending an
  unlimited transcript.
- Do not run a resident ABox daemon for the first milestone.

### Initial Resource Budgets

These are milestone targets to validate on a named baseline Apple Silicon
machine. Measurements must be recorded and the budgets may be changed only
with benchmark evidence and an ADR update.

| Resource | Initial target |
| --- | --- |
| Host supervisor before VM boot | At most 50 MiB RSS, measured as process RSS |
| Default guest allocation | 1 vCPU and 768 MiB configured VMM RAM. This is the browse-and-patch default, not a compile or test default |
| Combined default-path budget | Host ABox process RSS plus configured guest RAM allocation, at most 1 GiB, excluding model servers and user build workloads. Record the two numbers separately. Do not treat Hypervisor.framework lazy backing as guest RSS |
| Cold CLI startup | At most 100 ms |
| Minimal guest ready time | At most 500 ms after VMM start |
| Compressed base image | At most 256 MiB, or an ADR-raised limit after measuring the named demonstration repository and its toolchain |
| Default sparse session disk limit | 4 GiB logical, with physical use monitored |
| Buffered output per tool call | At most 1 MiB by default |
| Running VMs | One by default |
| Processes after clean exit | Zero |

Repository builds can legitimately require more memory, CPU, disk, or time.
Compile and test workloads use an explicit raised per-session profile after
approval. ABox must report memory pressure and must not silently reserve a
large VM for every user. Default-path resource acceptance excludes those
raised-profile workloads and reports them separately.

### Resource Evidence

- Add benchmarks for supervisor RSS, startup latency, VM boot latency, guest
  idle RSS, disk growth, and teardown.
- Run representative browse, patch, checkpoint, rollback, and fork workloads
  on the default profile, and compile or test workloads only on an explicit
  raised profile.
- Fail the resource acceptance job when a default-path budget regresses beyond
  an agreed tolerance.
- Display current VM CPU, memory limit, disk use, and active process state in
  the TUI without adding a polling-heavy telemetry stack.

## 3. Security Invariant

The primary invariant applies to every model-controlled operation:

- Model-authored shell commands execute only in the guest.
- Model-authored code executes only in the guest.
- Model-requested file reads and writes occur only in the guest repository.
- Model-requested searches occur only in the guest.
- Model-requested patch application occurs only in the guest.
- Model-requested Git and package-management commands occur only in the guest.
- The host never exposes a general-purpose shell or arbitrary file API to the
  guest.
- Changes reach the host only through an explicit patch review and import
  operation initiated by the user.

The host is allowed to perform narrowly defined, trusted operations required
to provision the guest, broker configured remote package and MCP requests, and
import a reviewed patch. Those operations must use fixed code paths and
arguments. Model-generated strings must never become host shell commands, host
command-line options, or remote destinations.

## 4. Trust Model

### 4.1 Trusted Host Supervisor

The host-side `abox` process owns:

- Terminal UI
- User interaction forwarded into the guest agent
- Session metadata
- MicroVM lifecycle orchestration
- Policy and connectivity configuration
- Audit records
- Patch review and confirmed import

Provider credentials and MCP tokens are entered or referenced on the host and
resolved from env-backed storage, macOS keychain, Vault, Azure Key Vault, or
AWS Secrets Manager. Resolved values are never written to session metadata,
`guest-config.json`, `config.raw`, or the guest disk. Host brokers perform
provider and remote MCP HTTPS; the agent loop remains in the guest.

The host supervisor must remain small. It must not contain an arbitrary shell
execution path, generated-code runner, or generic guest-to-host file service.

### 4.2 VMM Helper

The host-side `abox-vmm` helper owns the cgo interaction with libkrun. It is a
separate process from the TUI and model loop because:

- libkrun's VM entry point naturally fits a dedicated process.
- A VMM or cgo crash must not corrupt supervisor memory.
- The supervisor should not link directly against the VMM library.
- The helper can receive a small, validated configuration rather than broad
  application state.
- The helper can run with a minimal environment and a strict file-descriptor
  allowlist.

The helper is trusted host code, but it is part of the security-sensitive
computing base. It receives no model-authored flags or paths. The supervisor
sends one validated configuration blob through standard input or an inherited
descriptor, or writes it to a mode `0700` session directory. The helper's
command-line arguments are fixed by trusted code.

The supervisor keeps an inherited liveness pipe open. The helper must stop the
VM and exit when that pipe closes so supervisor death does not leave an
unmanaged VM. A recorded helper PID is used only for validated stale-session
cleanup and must not be trusted without verifying process identity and session
ownership.

### 4.3 Untrusted Guest

The `abox-guest` worker and everything it starts are untrusted. The design
assumes the guest can become fully compromised, including guest root and the
guest kernel.

The guest owns the agent: the prompt, tools, and everything the model
starts. Provider HTTPS is host-brokered.

The guest owns all effectful tools:

- `list_files`
- `read_file`
- `search`
- `apply_patch`
- `run_command`
- Repository writes
- Guest Git operations
- Tests and builds
- Generated code execution
- Applications started by the agent

The guest receives no provider credential values, MCP token values, host
home-directory access, cloud credentials, SSH keys, Docker socket, or
read-write host mount. Protocol-4 guest configuration also excludes MCP server
endpoints.

### 4.4 External Services

Model providers and agentgateway are external trust boundaries. Prompt data,
tool results, and repository excerpts sent to a provider leave the machine.
The UI and documentation must communicate that behavior clearly.

## 5. Why libkrun

libkrun provides hardware-backed isolation. On Apple Silicon, the stack is:

```text
ABox supervisor (Go)
  -> abox-vmm helper (Go + cgo)
  -> libkrun userspace VMM
  -> Apple Hypervisor.framework
  -> ARM hardware virtualization
  -> isolated Linux guest kernel and memory
```

A userspace VMM does not imply container or process-only isolation. Firecracker,
Cloud Hypervisor, QEMU, and vfkit also use userspace VMM components over a
hardware virtualization API.

libkrun is preferred for the initial backend because:

- It is explicitly designed for lightweight microVM-style workloads.
- It uses Hypervisor.framework on macOS ARM64.
- It supports KVM on Linux, providing a credible future backend path without
  coupling the higher-level harness to macOS.
- It supports raw block devices and virtio-vsock.
- It can add an explicit vsock device with TSI feature flags set to zero.
- It has a stable C API suitable for a narrow Go cgo wrapper.
- It avoids building a custom VMM.

### 5.1 Important libkrun Warning

libkrun documents that the VMM and guest should be considered part of the same
host security context when the VMM proxies host resources. In particular:

- `virtio-fs` can expose more of a host filesystem than intended unless the
  VMM itself has host mount isolation.
- Transparent Socket Impersonation can proxy guest sockets using the host's
  network context. TSI flags include `KRUN_TSI_HIJACK_INET` and
  `KRUN_TSI_HIJACK_UNIX`, so a guest Unix-socket probe is a required
  isolation test, not an optional extra.
- The VMM process runs with the host user's permissions.
- A libkrun, Hypervisor.framework, or device-emulation vulnerability may permit
  a guest escape.

ABox therefore excludes the risky proxy features rather than trying to filter
them inside the guest.

### 5.2 Intended libkrun Isolation Profile

The current product helper uses the stable-1.19-style libkrun API, and current
documentation reports libkrun 1.19.4. It explicitly disables implicit vsock,
adds one vsock with TSI flags set to zero, attaches two RAW block disks,
remounts `/dev/vda` as the root filesystem, and launches `abox-guest` through
`krun_set_exec`. Moving to libkrun main after removal of transitional APIs
requires a compatibility update. Artifact pinning and reproducible
distribution remain open work.

The current device-plan calls and remaining profile requirements are:

- Call `krun_add_vsock(ctx, 0)` once. Zero TSI flags means no
  `KRUN_TSI_HIJACK_INET` and no `KRUN_TSI_HIJACK_UNIX`. Only one vsock
  device is supported.
- Add only the one RPC port required by ABox, using `krun_add_vsock_port`
  or `krun_add_vsock_port2`. Record which side listens.
- Call no `krun_add_net_*` function, including `krun_add_net_tap`. If no
  net device is added, libkrun automatically enables the TSI backend.
  `krun_add_vsock(..., 0)` is what keeps vsock without TSI. Omitting net
  devices is not isolation.
- Current product code directs guest console output to the session's
  `console.log`. Removing that console or proving its acceptable device
  exposure remains hardening work; the plan must not claim that no console is
  present today.
- Do not call `krun_set_root`.
- Do not call `krun_add_virtiofs*` with a host path. That rule is
  inspectable in code review. An in-memory overlay created by
  `krun_fs_add_overlay_file` is backed by host memory, not a host file, and
  is allowed only if Phase 0.5 proves it exposes no host path.
- `krun_set_root_disk_remount` may use that in-memory dummy root to
  switch-root onto the block device. Treat a host-path virtio-fs as the
  leak, not the mere existence of a virtio-fs tag.
- Attach two raw disks: a writable session root and a sealed read-only
  config disk. Use an explicit `KRUN_DISK_FORMAT_RAW` constant. Prefer
  `krun_add_disk3(..., KRUN_DISK_FORMAT_RAW, read_only, direct_io,
  KRUN_SYNC_FULL)` for the writable session disk so checkpoint flush is a
  full drive flush. Do not rely on `krun_add_disk`'s macOS default of
  `KRUN_SYNC_RELAXED`. Never pass qcow2 or vmdk. Never probe format.
- Call `krun_has_feature(KRUN_FEATURE_BLK)` and refuse to start if block
  devices are unavailable.
- Use `krun_get_shutdown_eventfd` for orderly `Sandbox.Stop` when the
  pinned flavor provides it. On `stable-1.19.x` that call is documented as
  libkrun-efi only. If the pin lacks it, document forced stop as the
  remaining path.
- Reject unknown runtime options and arbitrary extra device arguments.
- Bind the RPC Unix socket inside a mode `0700` session directory.
- Current code configures vCPU/RAM and bounds command duration/output;
  explicit upper resource limits, disk quotas, and acceptance measurements
  remain Planned.

Guest process configuration is no longer undecided in the product code:
`abox-vmm` calls `krun_set_exec` with `/usr/local/bin/abox-guest` and a fixed
environment on the documented libkrun 1.19.4 path. The composed product boot
path is implemented and runnable. Its no-NIC, no-TSI, and no-host-path-
filesystem isolation properties remain implemented but unverified until the
named Apple Silicon hardware suite passes. Documentation must distinguish
"boots successfully" from "hardware isolation verified." The effective
device configuration is an allowlist.

### 5.3 Initial Backend Limitations

- Hardware virtualization does not protect against a compromised trusted host.
- It does not provide confidential memory or remote attestation on Apple
  Silicon.
- A VMM or Hypervisor.framework escape remains in scope as a residual risk.
- Packaging requires pinned libkrun and libkrunfw artifacts.
- The cgo helper is platform-specific even though the higher-level runtime
  interface is not.
- macOS runtime upgrades can change the effective hypervisor behavior and must
  be tested.

## 6. Runtime Abstraction

Higher-level host code depends on a `SandboxRuntime` interface rather than
libkrun directly. The interface should model actual lifecycle operations, not
unimplemented operations.

The initial shape should cover:

```go
type SandboxRuntime interface {
    Prepare(context.Context, PrepareRequest) (PreparedSandbox, error)
    Start(context.Context, PreparedSandbox) (Sandbox, error)
    Checkpoint(context.Context, Sandbox, CheckpointRequest) (Checkpoint, error)
    Rollback(context.Context, Checkpoint, RollbackRequest) (PreparedSandbox, error)
    Fork(context.Context, Checkpoint, ForkRequest) (PreparedSandbox, error)
}

type Sandbox interface {
    RPC() protocol.Client
    Stop(context.Context) error
    Destroy(context.Context) error
    Preserve(context.Context) (PreservedSandbox, error)
}
```

`Checkpoint` consumes the running `Sandbox`: it freezes, stops, and flushes
that VM. The previous handle is invalid afterward. `Rollback` and `Fork` both
return a `PreparedSandbox` that must be `Start`ed; they never mutate the
checkpoint disk and they do not return a live handle to the stopped source.

Exact names may change during implementation, but the boundary must preserve:

- Runtime-independent session orchestration
- Private disk preparation
- Boot and readiness
- Typed guest RPC
- Graceful and forced termination
- Destruction or explicit preservation
- Handle invalidation on checkpoint
- Rollback and fork as prepare-then-start operations

The first milestone implements real cold checkpoint, rollback, and fork
operations. A checkpoint is a bundle, not a disk clone alone:

- Guest disk snapshot
- Host event cursor into the append-only session log
- Working context and provider continuation state
- Active instruction and skill set
- Approval state

Audit history stays append-only. Rollback and fork restore the bundle's
cursor and working state. They do not rewrite or delete earlier audit
events. An "exact" restoration means the guest files and the host agent
state that produced them, not the disk in isolation.

Quiesce protocol:

1. Host sends `Quiesce`.
2. `abox-guest` runs `sync` and `FIFREEZE` via ioctl. Do not depend on a
   guest `fsfreeze` binary.
3. Guest acknowledges while the filesystem is still frozen.
4. VMM stops. Disk is flushed with `KRUN_SYNC_FULL`. ABox clones the
   stopped disk.
5. `FITHAW` is only for abort recovery if the VM remains running. Do not
   thaw before stop. That reopens the write race.

Crash-consistent clones without this freeze-and-ack are not
first-milestone checkpoints.

APFS copy-on-write does not make a file immutable. ABox enforces
checkpoint immutability: never attach a checkpoint file writable, clone
it before every boot, restrict permissions, and record identity plus
digest metadata. "Immutable" means ABox-enforced, not an APFS guarantee.

Rollback and fork boot a new writable clone of that verified disk and
restore the matching host bundle. Live memory snapshots are not part of
the first milestone and must not be implied by these APIs.

### 6.1 Lifecycle Glossary

These terms are distinct and must be used consistently in APIs, the TUI,
documentation, tests, and audit records:

| Operation | Identity | Disk | VM |
| --- | --- | --- | --- |
| Idle stop and resume | Same session | Same private disk | Stop, later start |
| Preserve | Same session | Keep disk after ABox exits | No VM |
| Checkpoint | New immutable snapshot | Copy-on-write clone with frozen parent | Stop, flush, clone, then optionally restart source |
| Rollback | Same session with a new running disk | Boot a clone of the checkpoint | Replaces current VM |
| Fork | New session | Clone of checkpoint | Cold until selected |

Idle stop and preserve must never be labeled as checkpoints. A checkpoint is
an ABox-enforced immutable bundle. Rollback and fork always clone the
checkpoint disk and restore the matching host cursor rather than mutating
the parent files.

## 7. Repository Layout

ABox uses one Go module with three binaries and a public SDK:

- `cmd/abox`: trusted CLI/TUI supervisor
- `cmd/abox-guest`: Linux guest worker and agent loop
- `cmd/abox-vmm`: narrow libkrun/cgo helper
- `pkg/abox`: public Go SDK
- `protocol`: versioned protocol-4 host/guest RPC
- `internal/agent` and `internal/agentapi`: guest agent loop and normalized types
- `internal/guest/tools`: five built-in guest tools
- `internal/guest/brokerclient`: guest proxy for the host LLM broker
- `internal/guest/mcpclient`: guest proxy for semantic MCP operations
- `internal/llmbroker`, `internal/mcpbroker`, and `internal/hostbroker`:
  host-side network brokers
- `internal/credsource` and `internal/credentials`: host credential resolution
  and fallback storage
- `internal/repository`, `internal/runtime`, `internal/session`,
  `internal/config`, `internal/tui`, and `internal/vmmconfig`
- `images`: current Docker-based guest-image builder
- `docs` and `examples`: SDK and CLI documentation and examples

Dedicated audit, patch-import, checkpoint-lineage, memory, skills,
package-broker, and architecture/ADR packages remain Planned rather than
current repository components. The module path is
`github.com/AdminTurnedDevOps/ABox`.

### 7.1 Host Storage Layout

The current default root is `~/.abox`; `ABOX_HOME` overrides it:

```text
~/.abox/config.yaml
~/.abox/credentials.env
~/.abox/sessions/<session-id>/
~/.abox/images/abox-guest.raw
```

The former `~/Library/Application Support/ABox` and
`~/Library/Caches/ABox/images` locations are legacy migration or fallback
paths, not the primary layout. The configuration file stores credential
references, never credential values. A signed or checksummed image manifest
and digest verification remain Planned.

The ABox application-support root, every session directory, and every preserved
disk directory must be mode `0700`. Cleanup resolves and validates every target
beneath the configured ABox root and must never follow a symlink or remove a
path outside that root.

## 8. Guest Image

The current guest is a 768 MiB raw ext4 ARM64 Linux root filesystem packed
with Docker. Docker is used only to build or update the golden filesystem and
is not on the session execution path. The image contains:

- The statically compiled `abox-guest` worker
- A POSIX-compatible shell
- Git
- Patch tooling
- Standard file utilities
- Alpine userspace and basic file utilities
- No systemd, SSH server, Docker engine, graphical stack, or idle package
  daemon

A reproducible controlled build, signed or checksummed manifest, image
identity, digest verification, vulnerability-update policy, and measured
compressed-image budget remain first-milestone requirements. The future image
pipeline must:

- Run in a controlled CI or Linux build environment.
- Produce a raw disk image or a trusted kernel plus raw root disk supported by
  the selected libkrun configuration.
- Publish a manifest containing versions, size, and SHA-256 digest.
- Verify the digest before first use and before cloning a session disk.
- Keep the immutable base image separate from per-session writable copies.
- Provide a documented update process for guest OS vulnerabilities.

On APFS, ABox may use `clonefile` to create an efficient copy-on-write session
disk. On filesystems where cloning is unavailable, it must make a full private
copy. It must never fall back to a read-write directory mount.

The initial image will not contain every language toolchain. Missing toolchains
must be reported as image limitations rather than bypassed through host
execution. Phase 0 names the demonstration repository and the exact guest
toolchain set. If that set does not fit the compressed-image budget, raise
the budget with an ADR after measurement.

Package installation is unavailable through ABox today because the package
broker is not implemented. The target direct mode must use the configured,
policy-bound connectivity broker in section 14.4 rather than an unrestricted
guest NIC. Required agentgateway mode will continue to refuse package
acquisition until a first-class gateway package route exists.

Guest package tools must use origin rewrite to a loopback adapter inside
`abox-guest`, not `http_proxy`/`https_proxy`. The host broker terminates
HTTPS. The guest never issues CONNECT and never needs a CA bundle for
brokered fetches.

## 9. Repository Provisioning

The current implementation requires the starting directory to be inside a Git
worktree and rejects submodules. It supports both clean and dirty worktrees.

### 9.1 Preconditions

For a clean worktree with an existing commit, ABox records the repository root
and `HEAD` and archives `HEAD`.

If the worktree is dirty or has no commit, ABox creates a private ephemeral
snapshot under the mode-`0700` session directory. It copies tracked files and
non-ignored untracked regular files, preserves executable bits, reflects
tracked modifications and deletions, initializes a private Git repository,
and creates a private baseline commit. It does not modify the host Git
repository.

Untracked ignored files are excluded. Tracked files remain part of the
snapshot even when an ignore rule matches them. The dirty-tree copy path
rejects symlinks, symlinked directories, and unsupported special files. A
directory that is not inside a Git worktree is not currently supported.

### 9.2 Transfer

The host archives the selected clean or ephemeral baseline with a narrowly
constrained Git operation and streams bounded chunks over authenticated RPC.

The guest extraction code must reject:

- Absolute paths
- `..` traversal
- Paths escaping through symlinks
- Device nodes
- FIFOs and unsupported special files
- Unsafe hard links
- Oversized files
- Excessive file counts
- Archives exceeding the configured total size

The archive is extracted only into the private guest repository directory.

### 9.3 Guest Baseline

After transfer, the guest initializes a private Git baseline. The guest
image or `abox-guest` installs a fixed in-guest git identity
(`user.name` and `user.email` scoped to the private repository) so baseline
and later guest commits do not require host Git config. All subsequent Git
operations happen inside the guest. The host `.git` directory is never
mounted or copied as a live writable repository.

### 9.4 Resume and Future Import

Resume boots the existing session `root.raw` and does not recopy the host
worktree.

Guest patch export is implemented, but host patch review and import are not.
Before import is added, clean snapshots may use `HEAD` and worktree-cleanliness
rechecks. Ephemeral dirty snapshots require a recorded source manifest and a
design that distinguishes pre-existing host changes from agent changes.
Current code must not claim safe dirty-tree import.

Starting another session from a dirty worktree is supported; the former rule
that the user must commit or stash before the next session is obsolete.

## 10. Host-Guest Protocol

The current host-guest RPC protocol is version 4. Normal CLI and SDK sessions
require protocol 4. `abox --probe-vm` may speak to an older guest only far
enough to perform its limited probe.

The transport is bounded length-prefixed JSON over virtio-vsock. On macOS,
libkrun maps the selected vsock port to a protected Unix socket.

Protocol history:

- v2: turn cancellation, options, usage, and rich events
- v3: host provider broker
- v4: host Streamable HTTP MCP broker and model `run_command` approval

Every connection must include:

- Protocol version negotiation
- Session identifier
- Per-session random capability
- Request identifier
- Method name from a closed allowlist
- Typed parameters
- Typed result or structured error

The host writes the session identifier and capability to a sealed read-only
raw config block attached before boot. The guest reads that block at startup
and presents the capability on the first vsock handshake. The capability binds
this socket to this session. It does not authenticate a compromised guest, and
it is not a host-user access-control mechanism. The mode `0700` session
directory already excludes other host users. Do not place the capability on
the kernel command line.

The protocol is bidirectional. Host-initiated methods and guest-initiated
methods have separate allowlists.

Host may call:

- Tool requests (`list_files`, `read_file`, `search`, `apply_patch`,
  `run_command`)
- Chunked archive upload and a single-frame, frame-limited patch export response
- `Quiesce`
- `SetTime`
- Shutdown / cancel

Guest may call only:

- `provider_open`, `provider_send`, `provider_cancel` (host LLM broker)
- `mcp_list`, `mcp_call`, `mcp_cancel` (host MCP broker)
- `request_run_command_approval`

`FetchPackage` remains Planned. Protocol 4 does not implement the proposed
generic MCP stream-open/read/push contract; it uses semantic MCP list, call,
and cancel operations through the host broker.

The guest must not invoke host tool, import, shell, or arbitrary-fetch
methods. Phase 3 tests both directions.

The guest implements `set_time` and `quiesce`, but the normal supervisor
lifecycle does not yet invoke them on startup, resume, or checkpoint. Clock
synchronization and checkpoint orchestration remain Planned.

The transport must enforce:

- Maximum frame size
- Maximum archive chunk size
- Maximum tool result size
- Maximum command output size
- Read and write deadlines
- Request cancellation
- One active session identity per socket
- Rejection of unknown methods and fields where practical
- Redaction-safe structured logging

Guest-initiated provider and MCP methods accept configured identifiers and
typed bounded data. They never accept a raw URL, TCP destination, CONNECT
target, or arbitrary HTTP request from the guest. The future package-fetch
contract in section 14.4 must preserve the same restriction.

Repository archives are chunked today. Patch export remains a single
frame-limited response; chunked patch transfer remains Planned if the patch
budget grows beyond the frame limit.

The guest must never be able to request:

- Arbitrary host file reads
- Arbitrary host file writes
- Host shell execution
- Arbitrary host URL fetching
- Direct provider credentials
- Docker socket access
- Unrestricted credential use

## 11. Guest Tool Contract

The first built-in ABox tool set is exactly five tools: `list_files`,
`read_file`, `search`, `apply_patch`, and `run_command`. Dynamically
discovered MCP tools are additional model-visible tools. They are not a
sixth built-in ABox tool and they are not a single MCP meta-tool. The
provider tool set is therefore "five ABox tools plus configured and allowlisted
discovered MCP tools."

This is a product decision, not an omission. Milestone one does not add
`write_file`; file creation and edits go through `apply_patch`. Read-only
Git inspection (`status`, `diff`, `log`) goes through `run_command` and
therefore through the default approval gate. A later milestone may add
`write_file` or an auto-approved read-only command allowlist.

### 11.1 `list_files`

Lists paths beneath the guest repository root.

Controls:

- Repository-relative paths only
- Bounded recursion depth
- Bounded result count
- Stable ordering
- No symlink traversal outside the repository

### 11.2 `read_file`

Reads a file beneath the guest repository root.

Controls:

- Repository-relative paths only
- Maximum bytes per call
- Optional line range
- Explicit binary-file handling
- No symlink traversal outside the repository

### 11.3 `search`

Searches file names or contents beneath the guest repository root.

Controls:

- Repository-relative scope
- Bounded match count
- Bounded output bytes
- Time limit
- No search outside the repository

The implementation may use Go traversal and regular expressions or a fixed
guest-side search binary. Any subprocess remains in the guest.

### 11.4 `apply_patch`

Applies a model-produced patch inside the guest repository.

Controls:

- Patch size limit
- Relative paths only
- Rejection of traversal and unsupported targets
- Structured success or failure output
- No effect on the host repository

Patch application may invoke a fixed guest-side executable with patch content
on standard input. This is allowed because the executable and effect are
inside the microVM.

### 11.5 `run_command`

Runs a model-authored command inside the guest.

Controls:

- Fixed guest repository working directory unless an allowed relative
  subdirectory is supplied
- Explicit shell inside the guest
- Wall-clock timeout
- Output byte limit
- Cancellation
- Exit status and duration reporting
- No host environment inheritance
- No host credentials
- stdin is `/dev/null` so interactive prompts fail immediately instead of
  consuming the wall-clock timeout
- Minimal guest environment only: `PATH`, `HOME` inside the guest, `LANG`,
  `TERM=dumb`, `TMPDIR` inside the guest, and fixed `GIT_AUTHOR_NAME`,
  `GIT_AUTHOR_EMAIL`, `GIT_COMMITTER_NAME`, and `GIT_COMMITTER_EMAIL` for
  the private guest identity. No host `PATH`, no host `HOME`, no host secrets

The guest is treated as compromised regardless of whether the initial process
runs as root or an unprivileged user. Running unprivileged remains useful
defense in depth but is not the primary boundary.

## 12. Agent Loop

The guest owns the model interaction loop. The host broker performs provider HTTPS:

1. Receive the user's prompt from the TUI or `abox exec`.
2. Build the model request using configured instructions, the five ABox
   tool schemas, and any approved discovered MCP tool schemas.
3. Send the request through the host provider broker and stream events back.
4. When the model requests a tool, validate the tool name and arguments.
5. Run the tool in the guest.
6. Stream or collect the bounded guest result.
7. Display activity and result status in the TUI.
8. Return the result to the same provider conversation.
9. Repeat until the model returns a final answer or a configured limit is
   reached.
10. Ask the guest to export the final patch.

The loop must enforce:

- Maximum turns
- Maximum tool calls
- Maximum parallel tool calls, initially one unless concurrency is proved safe
- Context cancellation
- Provider timeouts
- Guest operation timeouts
- Session-wide output and token accounting
- Clear terminal states for success, failure, cancellation, and policy denial

Provider-side built-in code execution, shell, browser, MCP, or file tools must
not be enabled. ABox exposes only its client tools so every effectful operation
uses the guest execution plane.

## 12.1 Context Management and Compaction

Context management is required in the first milestone. It must:

- Track provider limits, estimated or reported token use, tool schemas,
  instructions, messages, and tool results.
- Compact before the provider limit is reached.
- Preserve the active task, user constraints, repository instructions,
  decisions, changed files, unresolved errors, and recent tool state.
- Keep a lossless session event record on disk while sending a bounded working
  context to the provider.
- Use deterministic truncation for oversized tool output before asking a model
  to summarize it.
- Record when compaction occurred and which source event range it replaced.
- Support provider-specific continuation requirements without losing required
  reasoning or tool-call items.

Compaction is a correctness and resource feature. It must be testable with a
fake provider that has a deliberately small context window.

## 12.2 Skills and Repository Instructions

The first milestone must load and apply:

- Repository `AGENTS.md` files using nearest-scope precedence.
- User-configured global ABox instructions.
- ABox skills consisting of metadata, instructions, and optional resources.
- Explicitly selected skills and skills matched by documented activation
  rules.

Instructions and skill metadata are read by the trusted supervisor from the
host-captured repository snapshot and trusted host configuration. This
is a provisioning read, not a model-directed host file tool. The path is
trusted. The file contents are not. Repository `AGENTS.md`, repo-bundled
skills, and other repo-sourced instruction text are untrusted model input.

Repo-sourced instructions and skill metadata must never change:

- Approval policy
- Connectivity mode or broker allowlists
- CPU, memory, disk, or VM-concurrency limits
- The model-visible tool allowlist

Only trusted host configuration may change those controls. MCP server
descriptions already follow the same rule. Any executable skill resource runs
only inside the guest. Skill loading must be bounded and must not permit
arbitrary host file inclusion.

The TUI must show which instruction files and skills are active for the current
turn.

## 12.3 Sessions and Memory

**Current status:** Basic same-disk session resume is implemented. The guest
persists conversation messages in `/var/lib/abox/context.json`; the host stores
`session.json` and, for the TUI, `transcript.json`. CLI `--resume` and SDK
`Resume` boot the existing `root.raw` without recopying the repository. This
is not yet the append-only normalized event store, inspectable memory system,
checkpoint bundle, approval restoration, retention policy, or corruption
recovery specified below. Explicit-ID resume works for ephemeral dirty
snapshots, but automatic latest-for-repository matching needs correction
because current session metadata records the private snapshot root.

The first milestone must persist sessions and useful memory without a resident
daemon or heavyweight database service.

Session persistence includes:

- Prompt and normalized event history
- Provider and model selection
- Context compaction records
- Repository baseline
- Tool activity and approvals
- Patch and checkpoint metadata
- Runtime image and resource configuration
- Resume, completion, cancellation, and failure state

A checkpoint bundle stores a host event cursor, working context,
provider continuation state, active instructions, and approval state
together with the disk snapshot. The session event log remains
append-only. Rollback seeks the working context back to that cursor. It
does not truncate audit history.

Session records use the lifecycle glossary in section 6.1. Resume means an
idle-stopped session, or a preserved same-session disk, starts again from that
same private disk. Checkpoint, rollback, and fork records retain ABox-enforced
lineage and are never represented as ordinary resume operations.

Memory includes explicit user facts and task summaries selected for reuse.
Memory must be inspectable, editable, and deletable. It must not silently store
credentials, authorization headers, or unbounded command output.

Use compact local files or an embedded single-process store with atomic writes.
Do not add a network database, background service, or eager in-memory index of
all sessions. Load indexes and event ranges on demand.

## 12.4 MCP Client

Protocol 4 implements remote Streamable HTTP MCP through a host broker. The
host owns remote initialization, capability negotiation, tool discovery,
Streamable HTTP sessions, credential attachment, invocation, cancellation,
origin enforcement, and bounded result conversion. The guest uses `mcp_list`,
`mcp_call`, and `mcp_cancel`, caches validated tool schemas, adds configured
tools to the model-visible set, and returns results to the guest agent loop.

The guest supplies only a configured server name, discovered tool name, call
identifier, and bounded JSON arguments. It cannot supply an endpoint, HTTP
header, authorization value, or redirect destination. MCP tokens remain on
the host.

Implemented:

- Remote Streamable HTTP MCP
- Host-side discovery and invocation
- Per-server configured tool allowlists
- Same-origin redirect enforcement
- Schema, argument, result, timeout, concurrency, and cancellation bounds
- Direct, agentgateway-origin, and offline policy modes

Still Planned:

- Guest-local stdio MCP servers
- MCP tool approval prompts
- MCP resources and prompts
- Richer provenance and audit UI

MCP server-provided instructions, schemas, and tool descriptions are untrusted
model input.

## 13. Model Providers

Milestone one supports OpenAI, Anthropic, and Grok through xAI.

| Provider | Initial API | Default credential environment variable |
| --- | --- | --- |
| OpenAI | Chat Completions today; Responses Planned | `OPENAI_API_KEY` |
| Anthropic | Messages API | `ANTHROPIC_API_KEY` |
| Grok/xAI | Chat Completions today; Responses Planned | `XAI_API_KEY` |

### 13.1 Provider Interface

The provider abstraction must normalize:

- Streaming text deltas
- Tool definitions
- Tool calls and call identifiers
- Tool results
- Usage information
- Finish reasons
- Provider errors
- Provider-specific continuation state needed for multi-turn tool use

The interface must not erase provider information required to continue a
reasoning or tool-use turn correctly.

### 13.2 OpenAI and xAI

OpenAI and xAI currently share an OpenAI-compatible Chat Completions streaming
implementation while remaining separate provider types with different default
base URLs and compatibility behavior. Migration to Responses remains a
first-milestone requirement.

### 13.3 Anthropic

Anthropic requires a native Messages and content-block adapter. It must map
ABox tool schemas to `tool_use` blocks and guest results to `tool_result`
blocks while preserving the assistant content needed for subsequent turns.

### 13.4 Credentials

- LLM credentials and MCP tokens remain host-side.
- Approved host sources are env-backed storage, macOS keychain, Vault KV v2,
  Azure Key Vault, and AWS Secrets Manager.
- Credential references may be stored in `config.yaml`; resolved values are
  not.
- Resolved values are never written to session logs, `guest-config.json`,
  `config.raw`, or the guest disk.
- Protocol 4 resolves provider credentials in `internal/llmbroker` and MCP
  credentials in `internal/mcpbroker`.
- `Session.SetMCPTokens` changes host-memory MCP overrides and never injects
  guest environment variables.
- Deprecated secret-bearing protocol fields remain only for compatibility; a
  protocol-4 guest rejects secret-bearing boot configuration.

The credential source is distinct from connectivity policy: it resolves a
host-held value, while the selected host broker determines the permitted
network destination.

### 13.5 Configured Models

The initial TUI selects from configured provider/model profiles rather than
assuming that every provider implements a reliable model-list endpoint.

Example conceptual configuration:

```yaml
models:
  - name: openai-default
    provider: openai
    model: configured-openai-model
    credential_env: OPENAI_API_KEY
  - name: claude-default
    provider: anthropic
    model: configured-anthropic-model
    credential_env: ANTHROPIC_API_KEY
  - name: grok-default
    provider: xai
    model: configured-grok-model
    credential_env: XAI_API_KEY
```

Concrete model defaults should be chosen from current provider documentation
at implementation time rather than frozen in this plan.

## 14. Connectivity Modes

Connectivity is independent from the guest runtime isolation profile.

### 14.1 `offline`

- No external provider or service traffic.
- A loopback model endpoint may be allowed only if explicitly configured and
  documented as local connectivity.
- The guest remains without a NIC and without TSI.

### 14.2 `direct`

- The trusted host LLM broker may contact the selected configured provider
  `base_url`.
- The trusted host MCP broker may contact configured Streamable HTTP MCP URLs.
- Package-index fetch remains Planned (section 14.4).
- The guest has no NIC and no TSI inet or Unix hijacking.
- Direct mode does not imply unrestricted guest or host egress.

### 14.3 `agentgateway`

Today `agentgateway` is an MCP-origin policy mode. Configured `mcp_servers`
URLs are treated as gateway origins; with `enforcement: required`, exactly one
server is allowed and configuration fails closed otherwise. ABox is a client
of a pre-existing gateway and does not install a local gateway, Kubernetes
CRDs, Helm charts, or a gateway control plane. The guest device plan remains
identical.

LLM traffic still uses each selected model's `base_url`. A dedicated LLM
gateway adapter is Planned. That adapter must speak the gateway's documented
frontend, map ABox profile names to gateway aliases, use gateway credentials,
fail closed when required, and never silently fall back to a direct provider.
API, A2A, package, and other route types remain unsupported until first-class
clients and enforcement paths exist.

Current MCP configuration:

```yaml
connectivity:
  mode: agentgateway
  enforcement: required

mcp_servers:
  - name: agw
    url: https://agentgateway.example.com/mcp
```

### 14.4 Connectivity Broker Contract

Two host brokers are implemented:

- `internal/llmbroker`: provider open/send/cancel streams
- `internal/mcpbroker`: semantic MCP list/call/cancel operations over
  host-owned Streamable HTTP sessions

For MCP, the guest sends configured server and discovered tool identifiers.
The host maps the server identifier to trusted configuration, resolves any
credential, enforces the configured origin, and performs the MCP operation.
The current protocol methods are:

```text
mcp_list {}
mcp_call { call_id, server, tool, arguments }
mcp_cancel { call_id }

FetchPackage {
  index_id
  method: GET | HEAD
  normalized_relative_path
  bounded_query
  optional_range
}

FetchPackageResult {
  status
  content_type
  content_length
  bounded_cache_headers
  bounded_body
}
```

The package-index broker, `FetchPackage`, and guest loopback origin-rewrite
adapters remain Planned. Protocol 4 does not use the earlier proposed generic
MCP stream-open/read/push contract.

The contract enforces:

- The guest sends a configured server or index identifier, never a URL.
- For MCP, the host pins requests and redirects to the configured HTTPS
  origin. The Planned package broker must map each `index_id` to configured
  origins and allowed path prefixes.
- Package fetch permits only fixed `GET` and `HEAD` methods.
- `FetchPackageResult` carries status, content type, content length, and a
  bounded cache-header allowlist. Unknown or hop-by-hop headers are dropped.
- Range requests are optional and only forwarded when the configured index
  allows them. Partial responses stay inside the same byte budget.
- Response bodies are decoded with an explicit size limit. Compressed
  bodies are decompressed only for content types that must be rewritten,
  and only up to that decoded-size limit.
- MCP permits only the methods and headers required by the configured MCP
  transport.
- The host constructs authority, authentication, and other sensitive headers.
- Guest-supplied `Host`, authorization, forwarding, proxy, and connection
  headers are rejected.
- Redirects are disabled unless every redirect origin is explicitly
  allowlisted under the same configured identifier. Every hop is revalidated.
- Request and response bytes, duration, redirect count, and concurrency are
  bounded and cancellable.
- The broker is not a TCP, CONNECT, SOCKS, DNS, or general HTTP forwarder.
- Provider, gateway, package-index, MCP, and other host credential values
  remain on the host and are never returned to the guest.
- In offline mode, all remote broker methods are refused.
- With required agentgateway enforcement, the MCP broker may open only the
  configured gateway origin. Package acquisition fails closed; LLM traffic
  continues to use the selected `base_url` until the dedicated adapter exists.
- The host opens HTTPS using the operating system's default trust store. ABox
  does not manage custom CA bundles or copy CA certificates into the guest.

Package-manager compatibility uses origin rewrite, not proxying. A
loopback-only adapter inside the already running `abox-guest` process is the
configured registry origin. Tools must treat that origin as their actual
index, not as `http_proxy`/`https_proxy`. HTTPS indexes plus proxy variables
make the guest issue CONNECT and perform end-to-end TLS, which this contract
forbids.

| Tool | Guest origin rewrite |
| --- | --- |
| npm | `registry=http://127.0.0.1:<port>/` |
| pip | `--index-url http://127.0.0.1:<port>/simple` and `--trusted-host 127.0.0.1` |
| cargo | source replacement to `http://127.0.0.1:<port>/` |
| Go | `GOPROXY=http://127.0.0.1:<port>` plus a second configured `index_id` for the sum database, or an explicit `GOSUMDB`/`GONOSUMDB` policy in host config |

The adapter maps a request path to `index_id` plus a normalized relative path
and sends that over vsock. The host broker fetches the real HTTPS origin.

Request rewrite is not enough. pip simple indexes, npm packuments, and cargo
sparse-index configs return absolute HTTPS URLs for the next hop. The adapter
must rewrite those response bodies so follow-up fetches stay on the loopback
origin. Rewrite only declared content types for that index. Each secondary
origin (for example `files.pythonhosted.org`, npm tarball hosts, cargo `dl`)
needs its own configured `index_id` and path prefix. Go is the only
first-milestone ecosystem whose payloads stay relative to `GOPROXY`; the sum
database is a second `index_id` or an explicit `GOSUMDB`/`GONOSUMDB` policy.

Git-based package dependencies (`git+`, VCS URLs, cargo git sources, Go
pseudo-versions fetched as git) are unsupported in the first milestone
unless a later ADR designs a separate fetch path. They are image
limitations, not a reason to add CONNECT or a guest NIC.

Integrity remains in lockfiles and checksum files (`go.sum`, npm integrity).
`HTTP_PROXY` and `HTTPS_PROXY` are not the compatibility path and are not an
enforcement mechanism.

Offline mode performs no package or remote MCP acquisition. A missing
toolchain in offline mode remains an image limitation.

## 15. Terminal UI

Use Bubble Tea v2 for the event loop, Bubbles for focused components, and Lip
Gloss v2 for styling.

### 15.1 Visual Direction

The TUI should have a deliberate dark terminal aesthetic rather than a generic
dashboard appearance.

| Token | Proposed value |
| --- | --- |
| Canvas | `#050505` |
| Panels | `#0D0D0F` |
| Raised panel | `#141416` |
| Borders | `#27272A` |
| Primary text | `#F4F4F5` |
| Muted text | `#71717A` |
| Running state | Restrained cyan |
| Warning and approval | Restrained amber |
| Error and removal | Muted red |
| Addition | Muted green |

The application should fill the alternate screen with the near-black canvas.
It must remain usable on terminals with reduced color support.

### 15.2 Main Screen

The main screen contains:

- A compact top bar with ABox, provider, model, microVM state, and network mode
- A scrolling transcript and tool-activity viewport
- Collapsible tool calls showing arguments, status, duration, and bounded
  output
- A multiline prompt composer
- A concise key-hint footer

### 15.3 Model Picker

The model picker lists configured profiles and displays:

- Friendly profile name
- Provider
- Model identifier
- Connectivity route
- Credential availability without revealing the credential

### 15.4 Patch Review

The patch-review screen provides:

- Changed-file navigation
- Hunk navigation
- Addition and deletion highlighting
- Binary-file indication
- Patch statistics
- Reject and import actions
- A final explicit import confirmation modal
- After import, a notice that the host worktree is now dirty; another session
  may use the dirty-tree ephemeral snapshot path

The default action must be non-destructive. Cancellation or terminal closure
must not import the patch.

### 15.5 Responsive Behavior

- Use a compact layout below the normal width threshold.
- Preserve prompt and approval usability on small terminals.
- Truncate status metadata before truncating important model or patch content.
- Keep scrolling and cancellation responsive while model and guest operations
  run asynchronously.

### 15.6 Prohibited Host Actions

The TUI must not include:

- A host shell escape
- An arbitrary host command palette action
- A file browser capable of returning arbitrary host files to the guest
- Automatic patch import

### 15.7 Default Approval Policy

**Current status:** Protocol 4 and the TUI implement model-authored
`run_command` approval with `deny` and `allow_once`; deny is the default. The
SDK exposes `SetApprover`, while `abox exec` installs no approver and therefore
denies commands. Remember-for-session, MCP, resource, checkpoint, rollback,
fork, and patch-import approval flows remain Planned. The table below is the
target first-milestone policy, not the current implementation. MCP tools and
`apply_patch` do not currently prompt.

| Action | Default |
| --- | --- |
| `list_files`, `read_file`, `search` | Allow |
| `apply_patch` in guest | Allow and always display activity |
| `run_command` | Require approval; user may explicitly remember for the session |
| MCP tool | Require approval per configured server and tool |
| Resource increase or additional running VM | Require approval |
| Checkpoint, rollback, or fork | Require approval |
| Patch import to host | Require approval plus a second final confirmation |
| Cancel, reject, or quit | Never mutates the host |

The selected action on every effectful approval prompt defaults to the
non-destructive choice. A remembered `run_command` decision is explicit,
visible, scoped to one session, and revocable.

### 15.8 Headless Driver

`abox exec` provides a headless driver for CI and integration tests. It uses
the same agent, runtime, broker, tool, lifecycle, audit, and approval code paths
as the TUI and emits bounded structured JSONL events.

Headless operation is a test and automation surface, not a reduced-security
milestone. If an effectful action requires approval and no explicit headless
policy authorizes it, the action is denied. There is no implicit
`--yes-to-everything` behavior.

## 16. Patch Export and Import

**Current status:** Guest patch export exists, but it currently uses
`git diff HEAD`; untracked names appear only in the summary and their contents
are absent from the patch. Host validation, review, and import are not
implemented. The requirements below describe the completed target path.

### 16.1 Export

At the end of a successful session, the guest:

- Captures all changes relative to its private baseline.
- Includes additions, modifications, deletions, and supported binary changes.
- Produces a bounded patch and summary.
- Returns the patch over the authenticated RPC channel.

### 16.2 Host Validation

Before review, the host validates:

- Patch size and file-count limits
- Relative paths
- No traversal
- No writes outside the repository
- No unsupported file modes or special files
- For a clean baseline, captured `HEAD` and worktree cleanliness still match
- For an ephemeral dirty baseline, a recorded source manifest still matches
  and pre-existing changes are distinguished from guest changes
- Patch applies cleanly in check mode

### 16.3 Review and Confirmation

The patch is displayed before any host modification. Import requires an
explicit approval followed by a second final confirmation in the TUI or an
equivalent explicit headless policy. A rejected patch leaves the host
repository unchanged.

### 16.4 Import

The host may use a fixed Git executable invocation or a suitable Go library to
apply the reviewed patch. If Git is used:

- No shell is involved.
- The executable and arguments are fixed by trusted code.
- The patch is supplied through a controlled file or standard input.
- Model-generated data cannot add command-line options.
- The repository root is the captured trusted path.

Host patch import is an explicit exception to the guest-only effect rule
because it is a reviewed user action owned by the trusted control plane.

## 17. Session Lifecycle

The first lifecycle is:

1. Validate configuration and repository state.
2. Create a mode `0700` session directory.
3. Capture repository baseline metadata.
4. Verify the trusted guest image.
5. Clone or copy a private writable session disk.
6. Start `abox-vmm` with a fixed device plan.
7. Wait for authenticated guest readiness and set the guest clock from the
   host clock.
8. Transfer the selected clean or ephemeral repository snapshot.
9. Run the agent and tool loop.
10. Idle-stop and resume the same session disk when resource policy requires.
    Set the guest clock again after every resume.
11. Preserve the same session disk when the user exits without destruction.
12. Quiesce the guest, then create immutable cold checkpoints at configured,
    approved boundaries.
13. Allow rollback in the same session or a new cold fork from a selected
    checkpoint.
14. Export and review the final patch.
15. Import only after approval and the second final confirmation.
16. Stop the guest.
17. Destroy or preserve the private disk according to the session setting.
18. Persist compact session state and a redacted audit summary.

Unexpected supervisor termination should cause the VMM helper to terminate or
be recoverable through recorded process and session metadata. Stale session
cleanup must never delete paths outside ABox's protected session root.

## 18. Audit Records

The host stores structured records for:

- Session identifier
- Repository identity and baseline commit
- Selected provider and model
- Connectivity mode
- Runtime backend and image digest
- VM resource configuration
- Tool names, timing, status, and bounded/redacted summaries
- Approval decisions
- Patch digest
- Import result
- Context compaction and memory decisions
- Active skills and repository instructions
- MCP server and tool provenance
- Checkpoint, rollback, and fork operations
- Resource budget and measured peak use
- VM destruction or preservation result

Audit logs must not contain:

- API keys
- Authorization headers
- Full environment dumps
- Unbounded command output
- Secrets detected in provider errors

## 19. Documentation Deliverables

Current documentation includes the README and dedicated SDK, API, CLI/TUI,
protocol, session, credential, approval, MCP, event, example, quickstart, and
troubleshooting pages. `docs/architecture.md`, `docs/threat-model.md`,
`docs/roadmap.md`, and the planned ADR set have not been created and remain
outstanding. The former instruction to create them before the vertical slice
is historical; they must now document the implemented protocol-4 architecture
and clearly distinguish implemented-but-unverified controls from future work.

### 19.1 `README.md`

- Concise project vision
- Current status
- Supported platform and backend
- Clear security disclaimer
- Explicit statement that the project is experimental
- Clean and ephemeral dirty-tree snapshot behavior
- Link to architecture and threat model

### 19.2 `docs/architecture.md`

- Trusted and untrusted components
- Control-plane and execution-plane split
- Provider flow
- Repository transfer flow
- Tool RPC flow
- Patch return flow
- Connectivity modes
- Connectivity broker and origin-rewrite package-fetch flow
- Lifecycle glossary and disk lineage
- Phase 0.5 boot-path verification status
- Resource budgets and measurement boundaries
- Runtime interface

### 19.3 `docs/threat-model.md`

- Assets
- Adversaries
- Trust boundaries
- Threats
- Controls
- Residual risks
- Non-goals
- Claim-to-test matrix
- Broker SSRF and redirect threats
- Repo-sourced instruction injection into policy
- Explicit status for every security feature

### 19.4 `docs/roadmap.md`

- Milestone phases
- Entry and exit criteria
- Deferred features
- Security gates

### 19.5 `docs/adr/`

Initial ADRs:

- ADR-0001: Split trusted host and untrusted guest architecture
- ADR-0002: Select libkrun as the initial microVM backend
- ADR-0003: Prohibit host workspace mounts and use private repository copies
- ADR-0004: Use versioned typed RPC over virtio-vsock
- ADR-0005: Separate guest network isolation from host connectivity routing
- ADR-0006: Use native provider adapters behind a common model interface
- ADR-0007: Use a dedicated VMM helper process for the cgo boundary
- ADR-0008: Use clean `HEAD` archives or private dirty/unborn snapshots
- ADR-0009: Enforce lightweight default resource budgets
- ADR-0010: Use cold disk checkpoints for rollback and fork
- ADR-0011: Use a semantic host broker for remote MCP and keep future stdio MCP in the guest
- ADR-0012: Use an endpoint-bound host broker while the guest has no NIC

ADR-0002 must compare at least libkrun, vfkit, direct
Virtualization.framework integration, Tart, and Lima. It must record
maintenance, license, device-policy behavior, packaging, portability, and
whether ABox pins `stable-1.19.x` (transitional `krun_disable_implicit_*`)
or a main-line API (`krun_add_vsock` only). ADR-0002 and the threat-model
isolation claims must be updated if Phase 0.5 cannot boot the intended
device plan.

ADR-0012 must specify origin rewrite rather than HTTP(S) proxying, including
response-body rewrite, secondary origins, and Go sum-database handling.

### 19.6 `AGENTS.md`

- Go conventions
- Package boundaries
- Host execution prohibition
- Test expectations
- Security claim rules
- Documentation requirements
- No secret handling in tests or logs
- Requirement to preserve the no-mount and no-guest-network invariants
- Requirement that broker requests never accept raw destinations or URLs
- Requirement that repo-sourced instructions cannot change policy or limits
- Requirement to use origin rewrite, not proxy variables, for guest package tools
- Requirement to keep five built-in ABox tools plus discovered MCP tools
  unless a later ADR adds built-in tools
- Requirement to benchmark and preserve the default resource budgets

## 20. Incremental Implementation Plan

Each phase below is an ordered internal checkpoint toward the complete
first-milestone product defined in section 23. This is a 0.1/1.0 program.
No individual phase is a separately shippable product or reduced ABox
release.

**Status note:** These phase checklists are the acceptance roadmap, not a
claim that implementation proceeded in this order. Development advanced out
of order: protocol 4, the public SDK, TUI, basic resume, host LLM/MCP brokers,
host-only credentials, dirty-tree snapshots, and `run_command` approval exist,
while several earlier documentation, image, runtime-hardening, and hardware
gates remain incomplete. Completing an implementation task does not imply
that its phase exit criteria or security evidence passed.

### Phase 0: Project Decisions

- Record the selected Go module path, `github.com/AdminTurnedDevOps/ABox`.
- Record the selected Apache-2.0 license.
- Confirm the minimum macOS version.
- Pin a maintained stable libkrun release and compatible libkrunfw artifact.
- Decide whether runtime artifacts are downloaded, bundled, or discovered from
  an installation.
- Record the current `~/.abox` session/image layout and name the Apple Silicon
  resource baseline machine.
- Name the demonstration repository and the exact guest toolchain set used to
  judge the image-size budget.
- Secure a dedicated Apple Silicon host that can run Hypervisor.framework
  without nested virtualization. GitHub-hosted macOS ARM runners are not
  sufficient for Phase 0.5, Phase 6, or Phase 18.
- Write draft `docs/architecture.md`, `docs/threat-model.md`, and ADR-0002
  marked Planned. These drafts exist so the spike has a written target.
  They must not claim a verified isolation profile.

Exit criteria:

- Module and license decisions are recorded.
- Runtime versions and distribution assumptions are documented.
- The hardware runner exists and can create a hardware-virtualized VM.
- The demonstration repository and toolchain set are named.
- Draft architecture, threat model, and ADR-0002 exist and are marked
  Planned.

### Phase 0.5: libkrun Boot Spike

The product VMM path now boots with the intended device-plan calls, but the
disposable research record and named-hardware evidence required by this phase
do not exist. Reproduce the product call sequence on the dedicated Apple
Silicon host and record the evidence without treating a successful boot as
proof of isolation. Isolation claims stay Planned until the hardware suite
passes.

- Link the pinned libkrun and libkrunfw from a narrow cgo helper.
- Compare the current 1.19-style product API to `containers/libkrun` main and
  record the migration requirements for removed transitional calls.
- Build the intended release device plan from section 5.2:
  `krun_add_vsock(ctx, 0)`, no net, no host-path virtio-fs, two raw disks.
- Record the current `krun_set_exec` guest process configuration and verify it
  on the named supported runtime.
- Boot a guest with a writable root disk, a sealed read-only config disk,
  and vsock RPC.
- Prove guest-local loopback and guest-local Unix sockets work. They are
  required by the package adapter.
- Prove those sockets cannot reach host canary listeners, LAN, or external
  endpoints, and that TSI inet and Unix hijacking are off.
- Prove no host-path virtio-fs mount exists. An in-memory overlay is not
  automatically a failure.
- Record the working vsock listen direction and the exact call sequence.
- If the intended sequence cannot boot without TSI, a guest NIC, or a
  host-path virtio-fs, stop and update ADR-0002 before Phase 1 isolation
  prose.

Exit criteria:

- A recorded call sequence boots and speaks vsock on the pinned runtime.
- Guest-local loopback and Unix sockets work.
- Guest probes cannot reach host canaries, LAN, or the external network.
  TSI is off.
- No host filesystem is exposed.
- Guest process launch and init ownership are written down.
- Failures change ADR-0002 and section 5.2; they do not silently add
  host-path virtio-fs, TSI, or a guest NIC.

### Phase 1: Documentation First

- Complete README, architecture, threat model, roadmap, ADRs, and AGENTS.md
  from the Phase 0 drafts and the Phase 0.5 research record.
- Mark product controls as implemented where code exists, but keep isolation
  claims Planned until the named hardware suite verifies them.
- Document the libkrun TSI, Unix-socket hijack, and host-path virtio-fs
  hazards explicitly. Do not document `krun_disable_implicit_*` as required
  APIs unless the chosen pin still has them.
- Document origin rewrite, untrusted repo instructions, checkpoint quiesce,
  dirty-tree-after-import, and resource-metric definitions.
- Do not describe the section 5.2 profile as verified until Phase 0.5 passes.

Exit criteria:

- Documentation is internally consistent.
- No document claims an untested control is enforced.
- Isolation claims cite Phase 0.5 or remain Planned.

### Phase 2: Go Scaffolding

- Initialize the Go module.
- Add `cmd/abox`, `cmd/abox-guest`, and `cmd/abox-vmm`.
- Preserve the current provider, broker, runtime, protocol, agent, repository,
  SDK, and TUI package boundaries; add patch/import boundaries when built.
- Scaffold `abox exec` as a second driver over the same application services.
- Define and test the macOS session, configuration, memory, and image-cache
  paths.
- Add configuration parsing and strict validation.
- Add unit-test fakes for providers, RPC, and runtime orchestration.

Exit criteria:

- All binaries build for their intended targets or fail with a clear
  unsupported-platform error.
- Package dependency tests prevent guest packages from importing host-only
  code.

### Phase 3: Protocol

- Implement framed messages and version negotiation.
- Add session capabilities and typed methods.
- Preserve protocol-4 semantic MCP methods (`mcp_list`, `mcp_call`, and
  `mcp_cancel`) and add typed package-fetch methods separately, with no raw
  URL or destination fields.
- Split host-initiated and guest-initiated allowlists.
- Add `Quiesce` and `SetTime` methods.
- Add deadlines, cancellation, and size limits.
- Add archive streaming and patch streaming.
- Add fuzz tests for frame decoding and malformed requests.

Exit criteria:

- Protocol tests cover malformed, oversized, unauthorized, unknown, and
  cancelled requests.

### Phase 4: Guest Worker and Tools

- Implement repository-root confinement.
- Implement the five tools.
- Implement command timeout, output bounding, `/dev/null` stdin, and the
  minimal guest environment.
- Implement host-driven `Quiesce` and `SetTime`. Quiesce uses `sync` and
  `FIFREEZE`, acknowledges while frozen, and uses `FITHAW` only to abort
  while the VM is still running.
- Implement private baseline initialization and patch export.
- Add Linux tests for path traversal, symlink escape, binary files, command
  cancellation, and output limits.

Exit criteria:

- Every model-visible effect is implemented in `abox-guest`.
- Tool tests run without any host runtime integration.

### Phase 5: Guest Image

- Build the ARM64 Linux image reproducibly.
- Install the guest worker and required tooling.
- Publish and verify an image manifest and digest.
- Add image boot-readiness tests.

Exit criteria:

- The image boots under the pinned runtime.
- The worker reports its protocol version and image identity.

### Phase 6: libkrun Runtime

- Promote the Phase 0.5 call sequence into `abox-vmm`.
- Implement the narrow cgo wrapper in `abox-vmm`.
- Build the explicit device plan around `krun_add_vsock(ctx, 0)`.
- Attach the writable root with `krun_add_disk3` and
  `KRUN_DISK_FORMAT_RAW` plus `KRUN_SYNC_FULL`.
- Attach the sealed read-only config disk as a second raw disk.
- Call `krun_has_feature(KRUN_FEATURE_BLK)` before start.
- Use `krun_get_shutdown_eventfd` for orderly stop when the pin provides it.
- Pass one validated config blob through a protected descriptor, never through
  model-authored command-line arguments.
- Add supervisor-liveness handling and validated stale-PID cleanup.
- Implement boot, readiness, stop, forced stop, destroy, and preserve.
- Add call-sequence tests through a narrow libkrun API abstraction.

Exit criteria:

- Runtime unit tests prove the builder emits the intended allowlist and
  rejects plans that add net devices, host-path virtio-fs, or a non-RAW
  format.
- Unit tests do not claim to prove what libkrun would do if TSI were left
  implicit. That proof is a Phase 0.5 and Phase 18 guest probe.
- A real Apple Silicon integration test boots and communicates with the guest.
- Device inspection shows no network device and no host-path filesystem
  share.

### Phase 7: Repository Transfer

- Support clean committed snapshots through `git archive HEAD`.
- Support dirty and unborn Git worktrees through a private ephemeral baseline.
- Include tracked and non-ignored untracked regular files; preserve tracked
  modifications, deletions, and executable bits.
- Reject submodules and unsafe or unsupported file types.
- Stream and safely extract the selected snapshot in the guest.
- Initialize the private guest baseline.
- Verify guest changes do not change host files.

Exit criteria:

- Clean, dirty, and unborn Git worktrees transfer correctly.
- Non-Git directories, submodules, unsafe symlinks, and malicious archive
  paths fail clearly.
- Ignored untracked files do not enter the snapshot.
- Malicious archive-path tests are rejected.

### Phase 8: Providers

- Implement the common provider contract.
- Implement OpenAI Responses streaming and tool calls.
- Implement xAI Responses streaming and tool calls.
- Implement Anthropic Messages streaming and tool calls.
- Add `httptest` fixtures for success, parallel calls, malformed calls, rate
  limits, authentication errors, timeouts, and interrupted streams.

Exit criteria:

- Each provider can complete a fixture-backed multi-turn tool loop.
- No provider enables server-side effectful tools.

### Phase 9: Agent Loop

- Connect provider events to guest RPC.
- Connect the TUI and `abox exec` drivers to the same agent event stream.
- Enforce turn, tool-call, timeout, and output limits.
- Add cancellation and error propagation.
- Record redacted audit events.

Exit criteria:

- A fake provider requesting `run_command` results only in guest RPC.
- No model-authored command reaches a host process API.
- Headless approval requests fail closed unless an explicit policy authorizes
  them.

### Phase 10: Context, Instructions, and Skills

- Implement context accounting and deterministic compaction.
- Preserve provider-specific continuation state.
- Load scoped `AGENTS.md` files from the captured repository.
- Load bounded global instructions and skill definitions.
- Ensure executable skill resources run only in the guest.
- Reject repo-sourced attempts to change approval, connectivity, limits, or
  the tool allowlist.
- Add small-context provider tests and instruction-precedence tests.

Exit criteria:

- Long tool loops compact and continue without exceeding provider limits.
- Compaction preserves active constraints and changed-file state.
- Active instructions and skills are visible and auditable.
- Hostile `AGENTS.md` fixtures cannot relax approvals or raise resource limits.

### Phase 11: Persistent Sessions and Memory

- Persist normalized session events through atomic, append-oriented storage.
- Resume interrupted sessions without loading all historical output into RAM.
- Add inspectable, editable, and deletable user memory.
- Add retention, size, redaction, and corruption-recovery tests.
- Avoid a background daemon or external database.

Exit criteria:

- ABox resumes a terminated session with its model, context, approvals, guest
  disk, and patch state intact.
- Memory survives restart and can be deleted completely.
- Secret-shaped fixtures do not enter retained memory automatically.

### Phase 12: Connectivity Broker and MCP Client

Remote Streamable HTTP MCP is implemented through the protocol-4 semantic host
broker. Remaining work is MCP approval, guest-local stdio MCP, richer
provenance, and the separate package-index broker and origin-rewrite path. Do
not replace the semantic MCP contract with a generic HTTP or TCP proxy.

- Preserve and extend the implemented offline and direct host routing.
- Preserve host MCP list/call/cancel and implement `FetchPackage` separately.
- Implement guest origin rewrite, including response-body rewrite and
  secondary `index_id` entries.
- Reject raw URLs, CONNECT, proxy-variable, and unconfigured origins.
- Launch stdio MCP servers only inside the guest.
- Add per-server and per-tool approvals.
- Preserve discovered MCP translation into the provider tool set and add
  richer provenance.
- Add malicious schema, oversized output, cancellation, timeout, and server
  crash tests.
- Add SSRF, raw-URL, redirect, header-injection, and offline-refusal tests.

Exit criteria:

- A local stdio MCP tool runs entirely in the guest.
- A remote MCP tool in direct mode reaches only its configured endpoint
  through the semantic host broker.
- Offline mode cannot reach a remote MCP server or package index.
- Package follow-up URLs in pip, npm, and cargo responses stay on configured
  `index_id` origins.

Required-agentgateway fail-closed tests belong to Phase 16.

### Phase 13: TUI and Approval Workflows

- Implement the dark full-screen layout.
- Add prompt composition and streaming transcript.
- Add tool activity and expandable output.
- Add provider/model selection.
- Add VM and connectivity status.
- Add approval views for guest commands, MCP tools, resource increases,
  rollback, fork, and patch import.
- Add session, memory, skill, and checkpoint navigation.
- Add responsive compact rendering.
- Add renderer and update-loop tests.

Exit criteria:

- The UI remains responsive during model and tool operations.
- Cancellation works from every running state.
- Reduced-size terminal snapshots remain usable.
- The default choice for every effectful approval is non-destructive.

### Phase 14: Patch Review and Import

- Export a patch from the guest.
- Validate patch paths, size, baseline, and applicability.
- Render files and hunks in the TUI.
- Require approval followed by a second final confirmation.
- Apply through a fixed trusted import path.
- Recheck the baseline immediately before import.

Exit criteria:

- Rejection leaves the host unchanged.
- Approval plus the second confirmation imports exactly the reviewed patch.
- Concurrent host changes prevent import.

### Phase 15: Checkpoint, Rollback, and Fork

- Freeze the guest filesystem, acknowledge while frozen, then stop and
  flush before creating a cold checkpoint. Thaw only if aborting while the
  VM is still running.
- Clone the raw disk using APFS copy-on-write where available. Never attach
  a checkpoint file writable. Clone again before every boot.
- Store the checkpoint bundle: disk identity and digest, host event cursor,
  working context, continuation state, instructions, and approvals.
- Keep the session audit log append-only.
- Roll back by stopping the current VM, restoring the host bundle cursor,
  and booting a private clone of the selected checkpoint disk.
- Fork a new cold session from a selected checkpoint without mutating its
  parent.
- Keep only one fork running by default.
- Add interrupted-clone, insufficient-disk, parent-deletion, and lineage tests.

Exit criteria:

- A checkpoint without freeze-and-ack is rejected.
- Rollback restores the checkpointed guest disk and the matching host
  agent state. Later conversation turns are not visible to the restored
  working context. Audit history still contains those later events.
- Parent and sibling disks remain unchanged after work in a fork.
- Checkpoint disk growth and operation latency meet the resource budgets.
- The UI labels these as cold disk checkpoints, not live memory snapshots.

### Phase 16: agentgateway Adapter

- Implement the dedicated gateway provider adapter that speaks
  agentgateway's OpenAI-compatible frontend with model aliases and
  gateway credentials.
- Do not reuse a direct OpenAI, xAI, or Anthropic client in required
  gateway mode.
- Make required gateway routes fail closed.
- Prove runtime device configuration is unaffected by connectivity mode.
- Reject unimplemented API and A2A route values.
- Provide tests with a local fixture gateway process or a documented
  pre-existing endpoint. ABox does not install agentgateway.

Exit criteria:

- ABox functions without agentgateway.
- Required gateway mode cannot silently use a direct provider endpoint.
- Required MCP gateway mode cannot contact the configured MCP server directly.
- Guest requests cannot make the host fetch an unconfigured URL or origin.
- Offline mode refuses both package-fetch and remote MCP broker methods.
- Guest isolation remains identical.
- Package tools use origin rewrite; CONNECT and proxy-variable paths fail.

### Phase 17: Resource Acceptance

- Measure cold CLI startup and idle supervisor RSS.
- Record host process RSS and configured guest RAM as separate numbers.
- Measure guest boot time, configured allocation, and process count.
- Measure base image size and copy-on-write disk growth.
- Measure browse, patch, MCP, checkpoint, rollback, fork, and teardown
  workloads on the default profile. Measure compile or test workloads only
  on an explicit raised profile.
- Verify idle shutdown and zero remaining processes after exit.
- Add regression thresholds for the documented default budgets.

Exit criteria:

- Default-path measurements meet the resource budgets on the named baseline
  machine.
- Any exception is documented with evidence and accepted through an ADR.

### Phase 18: Security Acceptance

- Run all hardware-backed acceptance tests.
- Capture runtime versions and host environment metadata.
- Generate a control status report.
- Mark failed or unexecuted controls as unverified.
- Resolve failures before making corresponding security claims.

Exit criteria:

- Every published claim points to a passing test or directly inspectable
  enforcement mechanism.

## 21. Test Strategy

### 21.1 Unit Tests

- Configuration validation
- Provider translation and streaming
- Agent turn limits and dispatch
- Protocol framing and authorization
- Archive extraction safety
- Repository path confinement
- Patch path validation
- Runtime device-plan construction
- TUI update behavior and rendering
- Audit redaction
- Context compaction and instruction precedence
- Session persistence, memory deletion, and recovery
- MCP schema translation and approval policy
- Checkpoint lineage, host-cursor restore, and append-only audit
- Resource accounting
- Connectivity broker identifier mapping, limits, and offline refusal
- Bidirectional RPC allowlists and semantic MCP method framing
- Origin rewrite versus proxy-variable rejection
- Repo instruction isolation from policy and limits
- Checkpoint quiesce and handle invalidation

### 21.2 Fuzz Tests

- RPC frame decoder
- JSON request decoding
- Archive metadata and extraction paths
- Unified-diff path parsing
- Provider streaming event parsing
- MCP protocol and schema parsing
- Session event recovery

### 21.3 Integration Tests

- Host-to-guest RPC over real vsock
- Repository transfer into a private disk
- All five guest tools
- Patch export and host review flow
- Provider tool loop using local fixture servers
- VM stop, destroy, and preserve behavior
- Context compaction across provider turns
- Session stop and resume
- Guest-local stdio MCP and brokered remote MCP
- Configured package fetch through origin rewrite and the typed broker
- Cold checkpoint bundle: disk plus host cursor, context, and approvals
- Guest clock set on ready and resume
- Resource budget measurement

### 21.4 Hardware Security Tests

These tests require a real Apple Silicon host capable of hardware
virtualization. A mocked libkrun API is not sufficient evidence.

The suite should:

- Create unique canary files in a protected host test home.
- Create host SSH, cloud-credential, and Docker-socket canary paths without
  using real secrets.
- Boot the actual guest with the release device plan.
- Attempt to read host canary paths from guest root.
- Inspect guest mounts and devices.
- Confirm guest-local loopback and guest-local Unix sockets work.
- Attempt connections from the guest to host canary listeners, LAN, and
  external endpoints over IPv4, IPv6, and hijacked Unix sockets, and prove
  they fail. TSI must be off.
- Run destructive guest commands against guest paths.
- Verify all host canaries and the host repository remain unchanged.
- Modify the guest repository and verify the host stays unchanged until
  approval.
- Reject the patch and verify no host change.
- Repeat and approve the patch, verifying only reviewed changes appear.
- Run with no agentgateway configuration.
- Run with gateway mode and verify the guest device plan is identical.
- Verify remote MCP cannot bypass a required gateway route.
- Verify package and MCP requests cannot make the host fetch an unconfigured
  URL, follow an unconfigured redirect, or attach guest-selected credentials.
- Verify a guest `https_proxy` plus HTTPS index cannot induce CONNECT.
- Checkpoint, mutate guest files and host conversation state, roll back,
  and verify the disk and host working context match the bundle. Audit
  history remains append-only.
- Fork, mutate the child, and verify parent and sibling disks are unchanged.
- Verify idle shutdown and that no ABox or VMM process remains after exit.

## 22. Security Acceptance Matrix

| Acceptance criterion | Enforcement | Required evidence |
| --- | --- | --- |
| Guest cannot read host home | No host filesystem device | Real guest canary probe plus device inspection |
| Guest cannot read SSH keys | No host filesystem device | Synthetic SSH canary probe |
| Guest cannot read cloud credentials | No host filesystem device and no credential forwarding | Synthetic credential canary probe and RPC review |
| Repository is not mounted read-write | Private raw disk only | Device-plan test, guest mount inspection, host hash comparison |
| Guest cannot reach host or LAN | No net device and `krun_add_vsock(ctx, 0)` | Guest-local loopback works; host canary, LAN, and external probes fail |
| Model shell commands execute in guest | Agent dispatches only typed RPC | Fake-provider dispatch test and real guest command test |
| Destructive guest command cannot damage host | Hardware VM and no host mounts | Host canaries survive destructive guest test |
| Guest cannot access Docker socket | No host filesystem or socket forwarding | Synthetic socket probe and device inspection |
| Changes return only through review | No shared workspace and gated import | Reject/approve end-to-end tests |
| ABox works without agentgateway | Direct provider adapter | End-to-end direct-mode test |
| Gateway does not weaken isolation | Connectivity independent from runtime plan | Device-plan equality and real guest probes |
| MCP does not expose a host shell | Guest stdio execution and endpoint-bound broker | Local and remote MCP integration tests |
| Guest cannot induce arbitrary host fetches | Identifier-to-endpoint mapping; no raw URL method | SSRF, redirect, header, and unconfigured-origin tests |
| Repo instructions cannot relax policy | Host config is the only policy source | Hostile `AGENTS.md` fixture cannot change approvals or limits |
| Checkpoints are independent | ABox-enforced bundle: frozen disk clone plus host cursor | Rollback restores disk and working context; parent files stay read-only; audit stays append-only |
| Default use is lightweight | Explicit budgets and on-demand lifecycle | Named-host resource benchmark report |
| Unimplemented controls are visible | Explicit feature status | Documentation and generated status report |

Passing unit tests prove code intent but do not, by themselves, prove guest
isolation. Hardware-backed tests are required before describing those controls
as verified.

## 23. First Milestone Demonstration

The milestone is complete when a user can:

1. Start `abox` in a Git repository on Apple Silicon, using either a clean
   `HEAD` archive or a private ephemeral snapshot of a dirty or unborn tree.
2. Select a configured OpenAI, Anthropic, or Grok model.
3. Start a real libkrun hardware-isolated ARM64 Linux microVM.
4. Transfer the captured repository privately into the guest.
5. Enter a prompt in the dark full-screen TUI.
6. Watch model text and tool activity stream in the terminal.
7. Allow the model to use the five guest tools.
8. Observe that every tool and model command executes in the guest.
9. Receive a final patch generated from the guest baseline.
10. Review files and hunks in the TUI.
11. Reject the patch with no host change, or approve and separately confirm
    import. For an initially dirty snapshot, prove that pre-existing host
    changes are distinguished from agent changes and concurrent changes fail
    closed.
12. Load repository instructions and activate configured skills.
13. Compact an intentionally long context and continue the same task.
14. Discover and invoke a guest-local MCP tool.
15. Invoke a configured remote MCP tool through direct mode and through a
    required-agentgateway route without adding a guest NIC. Tests supply a
    local fixture gateway or a documented pre-existing endpoint. ABox does
    not install the gateway.
16. In direct mode, fetch a package through origin rewrite and the typed
    broker; prove required-agentgateway mode, unconfigured destinations, and
    `https_proxy`/CONNECT paths refuse that fetch.
17. Run the same tested agent flow through `abox exec` with fail-closed
    approvals.
18. Stop and resume a persisted session and inspect or delete its memory.
19. Create a cold checkpoint, mutate the repository and continue the
    conversation, then roll back to the checkpointed disk and host agent
    state. Later turns remain in the audit log only.
20. Fork from a checkpoint and prove parent and sibling independence.
21. Destroy or preserve the private VM disk according to session settings.
22. Meet the documented default-path resource budgets on the named baseline
    machine. Compile or test work in the demonstration uses an explicit raised
    profile and is reported separately.

The demonstration must include the security acceptance report for the exact
runtime and image used.

## 24. Explicit First-Milestone Non-Goals

Do not implement yet:

- Multiple simultaneous agents
- Browser automation
- OBO or enterprise identity
- Semantic authorization policies
- Cross-platform runtime support
- Rich IDE integrations
- A cloud control plane
- Production deployment
- A custom VMM
- Guest direct network access
- Live memory snapshots and live migration
- Automatic patch merge or conflict resolution
- Submodule support
- Git-based package dependencies
- `write_file` or an auto-approved read-only command allowlist

Interfaces may leave room for these capabilities, but the project must not add
placeholder methods or configuration that falsely suggests a security feature
works.

## 25. Post-Milestone Roadmap

### Phase A: Harness Quality

- Better model routing and fallback
- Richer approval policy
- Improved image profiles and toolchains
- More efficient compaction and memory retrieval
- MCP resources, prompts, elicitation, and sampling extensions as they mature

### Phase B: Controlled Connectivity

- Richer package and MCP fetch policy, provenance, and audit controls
- agentgateway routing for API and A2A traffic
- Expanded non-bypass connectivity for additional protocols
- Auditable credential sourcing and injection

Direct guest networking must not be enabled until an enforcement design exists
that cannot be bypassed by guest root. Proxy environment variables are not an
enforcement mechanism.

### Phase C: Advanced Runtime

- Live memory snapshots where supported and verifiable
- Faster incremental checkpoints
- Concurrent opt-in fork execution with explicit resource budgets
- Alternative Linux/KVM backend validation
- Windows backend research

### Phase D: Multi-Agent and Enterprise Features

- Multiple isolated agents
- Delegation
- Enterprise identity
- Semantic policy
- Organization-level audit and configuration

These phases require separate ADRs and threat-model updates.

## 26. Assumptions

- The first host is Apple Silicon running a supported modern macOS release.
- The host supports Hypervisor.framework and permits hardware virtualization.
- The first guest can be ARM64 Linux.
- Repositories use Git and may begin clean, dirty, or without a commit;
  non-Git directories are not currently supported.
- Provider HTTPS originates from the trusted host broker; the model loop and
  request construction remain in the guest.
- The guest has no NIC in every first-milestone connectivity mode.
- Offline is a broker and provider-routing mode. Today direct mode allows the
  host provider and configured remote MCP brokers; agentgateway mode restricts
  remote MCP to configured gateway origins while model providers still use
  their selected `base_url`. Package acquisition remains Planned. None of
  these modes changes the guest device plan.
- Planned package adapters will use origin rewrite rather than HTTP(S) proxy
  variables; no package adapter exists today.
- Users accept that untracked ignored local files are not present in an
  ephemeral snapshot. Tracked files remain included.
- Users accept that the initial image has a limited toolchain set.
- Users accept that a successful patch import leaves a dirty host worktree.
- The host and local administrator are trusted.
- The guest, model output, generated code, repository content, and
  repo-sourced instruction files are untrusted. Host configuration is the
  only source of approval, connectivity, limit, and tool-allowlist policy.
- Phase 0.5, Phase 6, and Phase 18 require a dedicated Apple Silicon host
  that can use Hypervisor.framework. Nested cloud macOS runners are not that
  host.

## 27. Resolved Decisions and Open Work

Resolved decisions:

- Go module: `github.com/AdminTurnedDevOps/ABox`
- License: Apache-2.0
- Go language version: 1.25
- Primary storage root: `~/.abox`, overridable with `ABOX_HOME`
- Current host-guest protocol: 4
- Current documented runtime: libkrun 1.19.4-style API
- Current guest launch: `krun_set_exec`
- Current remote MCP path: host Streamable HTTP broker
- Current repository path: clean `HEAD` archive or dirty/unborn ephemeral Git
  snapshot

Still open or incomplete:

- Minimum supported macOS version
- Exact pinned libkrun and libkrunfw versions, including whether the pin is
  `stable-1.19.x` or a main-line commit after the implicit-API removal
- Recorded Phase 0.5 evidence for the current `krun_set_exec`,
  `krun_add_vsock(ctx, 0)`, two-disk boot and vsock direction
- Guest-side rewrite rules for pip, npm, and cargo absolute follow-up URLs
- Runtime artifact distribution and code-signing approach
- Reproducible guest image build environment
- Image update and vulnerability-response policy
- Named demonstration repository and toolchain set for the image budget
- Named Apple Silicon baseline machine for resource budgets
- Dedicated non-nested Apple Silicon hardware runner, decided in Phase 0
- Validation or evidence-based adjustment of the initial resource budgets
- Exact explicit-confirmation interaction for patch import
- Go sum-database policy for origin-rewritten `GOPROXY`
- Context accounting and compaction
- Scoped `AGENTS.md`, global instructions, and skills
- Full append-only session events and inspectable memory
- Correct latest-for-repository resume association for ephemeral snapshots
- MCP approval and guest-local stdio MCP
- Patch review and host import, including safe dirty-baseline handling
- Cold checkpoint, rollback, fork, lineage, and lifecycle UI
- OpenAI/xAI Responses adapters and broader provider fixtures
- Dedicated LLM agentgateway adapter

None of these blockers justifies falling back to host command execution,
container-only isolation, a read-write workspace mount, or unenforced guest
networking.

## 28. Security Claim Policy

ABox must not describe itself as secure, sandboxed, isolated, offline, or
non-bypassable based only on architecture intent.

Every claim must identify:

- The enforcing component
- The relevant configuration
- The corresponding automated or manually reproducible test
- The runtime and image version tested
- Known residual risks

Statuses should use precise language:

- Planned
- Implemented but unverified
- Verified on a named environment
- Failed
- Not implemented

This policy applies to README text, release notes, documentation, UI status,
and external project descriptions.

## 29. Reference Sources for the Runtime ADR

- libkrun repository and security model:
  <https://github.com/containers/libkrun>
- libkrun C API on main:
  <https://github.com/containers/libkrun/blob/main/include/libkrun.h>
- libkrun `stable-1.19.x` C API, transitional implicit-disable functions:
  <https://github.com/containers/libkrun/blob/stable-1.19.x/include/libkrun.h>
- vfkit repository:
  <https://github.com/crc-org/vfkit>
- vfkit usage and device configuration:
  <https://github.com/crc-org/vfkit/blob/main/doc/usage.md>
- Apple Virtualization framework:
  <https://developer.apple.com/documentation/virtualization>
- Bubble Tea:
  <https://github.com/charmbracelet/bubbletea>
- xAI function calling:
  <https://docs.x.ai/docs/guides/function-calling>
- Anthropic tool use:
  <https://platform.claude.com/docs/en/agents-and-tools/tool-use/overview>
- OpenAI function calling:
  <https://developers.openai.com/api/docs/guides/function-calling>
- agentgateway documentation:
  <https://agentgateway.dev/docs/>
