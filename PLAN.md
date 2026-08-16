# ABox Implementation Plan

## 1. Purpose

ABox is a terminal-native agent harness comparable to OpenCode, Grok Build,
Claude Code, and Codex CLI. It is its own harness and does not wrap or launch
another coding-agent harness.

Its defining property is microVM-native execution:

> The agent runs inside the microVM. Prompts, model calls, tools, and
> generated code are guest work. The host is a TUI, VMM, and reviewed
> patch import. The answer to "host or sandbox?" is always "sandbox."

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
| Implementation language | Go |
| Initial host | macOS on Apple Silicon |
| Initial guest | ARM64 Linux |
| Initial microVM backend | libkrun over Apple Hypervisor.framework |
| Runtime integration | Dedicated `abox-vmm` Go helper with a narrow cgo boundary |
| Guest network | No NIC and no libkrun TSI in milestone one |
| Model traffic | Guest agent calls providers; TSI inet for HTTPS only |
| Providers | OpenAI, Anthropic, and Grok through xAI |
| Repository state | Clean Git worktree only |
| Host workspace sharing | Prohibited |
| Repository transfer | Private copy into a writable guest disk |
| Change return | Reviewed patch only |
| TUI framework | Bubble Tea v2, Bubbles, and Lip Gloss v2 |
| TUI style | Full-screen near-black interface with restrained status colors |
| agentgateway | Optional adapter; never required for basic operation |
| Connectivity broker | Host-owned, typed, endpoint-bound package and MCP broker |
| Package-manager compatibility | Origin rewrite to a guest loopback adapter, not HTTP(S) proxy |
| Instruction loading | Supervisor reads the captured host snapshot and host configuration |
| Repo instruction authority | Repo text cannot change policy, limits, connectivity, or tools |
| libkrun isolation profile | Intended allowlist on `krun_add_vsock(ctx, 0)`; composed boot unverified until Phase 0.5 |
| Model-visible tools | Five built-in ABox tools plus dynamically discovered MCP tools. No `write_file` |
| Headless operation | `abox exec` uses the same agent and approval paths without the TUI |
| Default VM resources | 1 vCPU and 768 MiB RAM, configurable with explicit limits |
| VM concurrency | One running VM by default; forks remain cold until selected |
| Background services | None after ABox exits |

The Go module path and open-source license remain to be selected. Apache-2.0
is the provisional license recommendation.

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

Provider credentials may be entered on the host (`/provider`) and are
copied into the guest agent so the model client runs inside the
microVM. The host must not run the agent loop or call provider APIs.

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

The guest owns the agent: the prompt, the model client, tools, and
everything the model starts.

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

The guest receives no model-provider credentials, host home-directory access,
cloud credentials, SSH keys, Docker socket, or read-write host mount.

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

Write the device plan against the current C API on `containers/libkrun`
main. The `stable-1.19.x` header still documents
`krun_disable_implicit_{init,console,vsock}`. On main,
`krun_disable_implicit_init` is a stub that returns `-ENOTSUP`, and
`krun_disable_implicit_console` and `krun_disable_implicit_vsock` are
removed. Pinning 1.19.x therefore pins a transitional API that upstream has
already deleted. That pin-versus-main choice is an ADR-0002 input, not a
settled fact.

The isolation intent, expressed with the current API, is:

- Call `krun_add_vsock(ctx, 0)` once. Zero TSI flags means no
  `KRUN_TSI_HIJACK_INET` and no `KRUN_TSI_HIJACK_UNIX`. Only one vsock
  device is supported.
- Add only the one RPC port required by ABox, using `krun_add_vsock_port`
  or `krun_add_vsock_port2`. Record which side listens.
- Call no `krun_add_net_*` function, including `krun_add_net_tap`. If no
  net device is added, libkrun automatically enables the TSI backend.
  `krun_add_vsock(..., 0)` is what keeps vsock without TSI. Omitting net
  devices is not isolation.
- Add no console in the production profile. A debug profile may call
  `krun_add_virtio_console_default` or `krun_add_serial_console_default`
  for output only.
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
- Set bounded CPU, memory, disk, output, and wall-clock limits.

Guest process configuration is the Phase 0.5 decision. On main,
`krun_set_exec`, `krun_set_env`, `krun_set_workdir`,
`krun_set_console_output`, and `krun_set_rlimits` return `-ENOTSUP` on
non-nitro builds and direct callers to libkrun_init `Config::apply()` with
`.krun_config.json`. The spike must choose one:

1. ABox owns init: ship init on the raw root disk and set the kernel
   cmdline with `krun_append_kernel_cmdline`.
2. Adopt libkrun_init plus an in-guest `.krun_config.json`.

Do not assume `krun_set_exec` works on the macOS Hypervisor.framework
build. Do not adopt libkrun-efi or another flavor in this plan unless
Phase 0.5 records that the intended sequence failed and ADR-0002 is
updated.

The composed boot remains unverified until Phase 0.5. Documentation must
label this profile Planned until that spike passes. The effective device
configuration is an allowlist.

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

## 7. Proposed Repository Layout

Use one Go module with three binaries and strongly separated packages:

```text
.
├── cmd/
│   ├── abox/                  # Trusted host supervisor and TUI
│   ├── abox-guest/            # Linux guest worker
│   └── abox-vmm/              # Narrow libkrun/cgo helper
├── protocol/                  # Versioned host/guest RPC types
├── internal/
│   ├── agent/                 # Model and tool-call loop
│   ├── audit/                 # Session and control records
│   ├── config/                # Validated configuration
│   ├── connectivity/          # Host-owned endpoint-bound broker and routing
│   ├── guest/
│   │   └── tools/             # Effectful tool implementations
│   ├── patch/                 # Review, validation, and host import
│   ├── provider/
│   │   ├── anthropic/
│   │   ├── openai/
│   │   └── xai/
│   ├── repository/            # Clean-tree validation and transfer
│   ├── runtime/
│   │   └── libkrun/           # SandboxRuntime implementation
│   ├── session/
│   └── tui/
├── images/                    # Reproducible ARM64 guest image definition
├── test/
│   ├── integration/
│   └── security/
├── docs/
│   ├── adr/
│   ├── architecture.md
│   ├── roadmap.md
│   └── threat-model.md
├── AGENTS.md
├── README.md
├── PLAN.md
├── go.mod
└── go.sum
```

The Go module path remains a bootstrap decision. A placeholder module path
should not be committed if the intended hosting organization is known before
implementation begins.

### 7.1 Host Storage Layout

On macOS, use platform-appropriate user directories:

```text
~/Library/Application Support/ABox/config.yaml
~/Library/Application Support/ABox/sessions/<session-id>/
~/Library/Application Support/ABox/memory/
~/Library/Caches/ABox/images/<digest>/
```

The configuration file stores credential references, never credential values.
The image cache stores the image, signed or checksummed manifest, and verified
digest metadata. A digest is verified before every session clone.

The ABox application-support root, every session directory, and every preserved
disk directory must be mode `0700`. Cleanup resolves and validates every target
beneath the configured ABox root and must never follow a symlink or remove a
path outside that root.

## 8. Guest Image

The initial guest is a reproducibly built ARM64 Linux image containing:

- The statically compiled `abox-guest` worker
- A POSIX-compatible shell
- Git
- Patch tooling
- Standard file utilities
- A minimal set of build tools needed by the demonstration repository
- No systemd, SSH server, Docker engine, graphical stack, or idle package
  daemon

The image pipeline must:

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

Package installation is unavailable in offline mode. In direct mode, package
acquisition must use the configured, policy-bound connectivity broker defined
in section 14.4 rather than an unrestricted guest NIC. Required agentgateway
mode refuses package acquisition in the first milestone because only LLM and
MCP gateway routes are implemented.

Guest package tools must use origin rewrite to a loopback adapter inside
`abox-guest`, not `http_proxy`/`https_proxy`. The host broker terminates
HTTPS. The guest never issues CONNECT and never needs a CA bundle for
brokered fetches.

## 9. Repository Provisioning

Milestone one supports clean Git repositories only.

### 9.1 Preconditions

Before booting a guest, the host must:

- Confirm the current directory is inside a Git worktree.
- Capture the repository root and current `HEAD` object ID.
- Confirm there are no tracked modifications.
- Confirm there are no non-ignored untracked files.
- Reject unsupported submodules for the first milestone.
- Record the baseline in session metadata.

Ignored files are not copied. This reduces the risk of copying `.env` files,
local credentials, caches, and build artifacts into the guest.

### 9.2 Transfer

The host creates a deterministic archive of the captured Git tree using Go
code or a narrowly constrained Git operation. It then streams bounded chunks
over the authenticated RPC connection.

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

### 9.4 Concurrent Host Changes

Before patch import, ABox must recheck:

- The host repository is still at the captured `HEAD`.
- The host worktree remains clean.
- The patch applies cleanly to that baseline.

If any check fails, import stops. ABox must not attempt an automatic merge in
the first milestone.

After a successful import the host worktree is dirty. The next session cannot
start from that tree until the user commits, stashes, or otherwise restores a
clean worktree. Document this in the README and in the TUI after import. It is
the expected first-milestone workflow, not an error.

## 10. Host-Guest Protocol

Use a narrow, versioned, typed protocol over virtio-vsock. On macOS, libkrun
maps the selected vsock port to a protected Unix socket.

The initial protocol may use length-prefixed JSON-RPC messages. It must not use
unbounded newline-delimited reads.

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
- Archive and patch streaming
- `Quiesce`
- `SetTime`
- Shutdown / cancel

Guest may call only:

- `FetchPackage`
- MCP stream methods defined in section 14.4
- Readiness and bounded log or status notifications

The guest must not invoke host tool, import, shell, or arbitrary-fetch
methods. Phase 3 tests both directions.

The host sets the guest clock from the trusted host clock at ready and after
every resume. The host requests quiesce before every first-milestone
checkpoint.

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

Guest-initiated package-fetch and MCP stream methods are defined in section
14.4. Those methods accept configured identifiers and typed relative request
data. They never accept a raw URL, TCP destination, CONNECT target, or
arbitrary HTTP request from the guest.

Large repository and patch payloads should be transferred through bounded
chunks rather than one unbounded base64 value.

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
provider tool set is therefore "five ABox tools plus approved MCP tools."

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

The host supervisor owns the model interaction loop:

1. Receive the user's prompt from the TUI or `abox exec`.
2. Build the model request using configured instructions, the five ABox
   tool schemas, and any approved discovered MCP tool schemas.
3. Stream model output into normalized host events.
4. When the model requests a tool, validate the tool name and arguments.
5. Send a typed tool request to `abox-guest` over RPC.
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
host-captured clean repository snapshot and trusted host configuration. This
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

MCP client functionality is required in the first milestone. The guest owns
MCP server discovery, tool invocation, and any stdio server process.

Required transports:

- Stdio MCP servers launched inside the guest
- Streamable HTTP MCP servers through the policy-bound connectivity broker
- SSE compatibility where required by configured servers

Remote transports use only the connectivity broker contract in section 14.4.

The host exposes no generic TCP proxy. For remote MCP, the guest sends a typed
transport request over vsock and the trusted connectivity broker can contact
only an exact configured endpoint. In agentgateway mode, required MCP traffic
is sent only to the configured agentgateway endpoint. In offline mode, only
guest-local stdio MCP servers are available.

The MCP implementation must support:

- Initialization and capability negotiation
- Tool discovery and schema translation
- Tool invocation and structured errors
- Cancellation and timeouts
- Session lifecycle
- User approval rules per server or tool
- Output and schema size limits
- Clear provenance in the TUI

MCP resources and prompts may be added after the core tool path works, but they
are still required before the first milestone is complete. MCP server-provided
instructions and tool descriptions are untrusted model input.

## 13. Model Providers

Milestone one supports OpenAI, Anthropic, and Grok through xAI.

| Provider | Initial API | Default credential environment variable |
| --- | --- | --- |
| OpenAI | Responses API | `OPENAI_API_KEY` |
| Anthropic | Messages API | `ANTHROPIC_API_KEY` |
| Grok/xAI | xAI Responses API | `XAI_API_KEY` |

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

xAI supports an OpenAI-compatible Responses flow. OpenAI and xAI can share a
carefully tested wire implementation while remaining separate provider types
with different default base URLs, authentication settings, model names, and
compatibility tests.

### 13.3 Anthropic

Anthropic requires a native Messages and content-block adapter. It must map
ABox tool schemas to `tool_use` blocks and guest results to `tool_result`
blocks while preserving the assistant content needed for subsequent turns.

### 13.4 Credentials

- Credentials remain only in host memory.
- Credentials are resolved by the host credential source from environment
  variables or the operating system credential store.
- Credentials are never written to session logs.
- Credentials are never copied into the guest.
- Configuration stores credential references, not secret values.

The credential source is distinct from the connectivity broker. It resolves
and injects host-held credentials but does not choose or open network routes.

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

- The trusted host supervisor may contact explicitly configured model-provider
  endpoints.
- The connectivity broker may contact exact configured remote MCP endpoints on
  behalf of the guest MCP client.
- The connectivity broker may fetch from exact configured package indexes on
  behalf of guest package tooling.
- The guest remains without a NIC and without TSI.
- Direct mode does not imply unrestricted guest egress.

### 14.3 `agentgateway`

- ABox is a standalone client of a pre-existing agentgateway endpoint.
- ABox does not install a local gateway, Kubernetes CRDs, Helm charts, or an
  agentgateway control plane.
- Selected host-side traffic routes through a configured agentgateway
  endpoint.
- LLM traffic uses a dedicated gateway provider adapter, not the direct
  OpenAI, xAI, or Anthropic clients. agentgateway's documented frontend is
  OpenAI-compatible. The gateway adapter speaks that frontend, maps ABox
  model profile names to gateway model aliases, and authenticates with
  gateway session credentials.
- Direct provider credentials and direct provider base URLs are not used
  in required-gateway mode.
- A required route fails closed if the gateway is unavailable or
  misconfigured.
- Required gateway routing must not silently instantiate or fall back to a
  direct provider client.
- The guest runtime configuration remains identical to the offline profile.
- Configured LLM and remote MCP traffic can be marked as required gateway
  routes in the first milestone.

The first adapter targets LLM and MCP traffic. API and A2A route values must be
rejected until corresponding first-class clients and enforcement paths exist.

Example configuration:

```yaml
runtime:
  isolation: microvm
  backend: libkrun
  network: deny-by-default

connectivity:
  mode: agentgateway
  endpoint: https://agentgateway.example.com
  enforcement: required
  route:
    - llm
    - mcp
```

The parser may understand only implemented values initially. It must reject or
clearly report unimplemented route types instead of pretending they are
enforced.

### 14.4 Connectivity Broker Contract

The first milestone includes a typed, allowlisted host broker for configured
package indexes and remote MCP servers. The broker is implemented by
`internal/connectivity` inside the trusted supervisor and does not run as a
separate daemon.

The broker exposes distinct protocol methods rather than a generic proxy.
A unary `MCPExchange` body is not enough for streamable HTTP or SSE.

```text
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

MCPStreamOpen {
  server_id
  operation
  bounded_headers
  bounded_body
}

MCPStreamRead {
  stream_id
}

MCPStreamCancel {
  stream_id
}
```

The host may also push bounded MCP stream frames. Every stream has a
lifetime, byte budget, and cancellation path.

The contract enforces:

- The guest sends a configured server or index identifier, never a URL.
- The host maps that identifier to exact HTTPS endpoint origins and allowed
  path prefixes from trusted configuration.
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
- Provider, gateway, package-index, MCP, and host credentials remain on the
  host and are never returned to the guest.
- In offline mode, all remote broker methods are refused.
- With required agentgateway enforcement, the broker may open only the
  configured agentgateway endpoint and never a direct backend or package-index
  endpoint. Package acquisition therefore fails closed in this mode in the
  first milestone.
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
- After import, a notice that the host worktree is now dirty and the next
  session requires a commit or another clean tree

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
- Baseline `HEAD` still matches
- Host worktree is still clean
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
8. Transfer the clean repository snapshot.
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

Create these before implementing the vertical slice:

### 19.1 `README.md`

- Concise project vision
- Current status
- Supported platform and backend
- Clear security disclaimer
- Explicit statement that the project is experimental
- Clean-worktree requirement and dirty-tree-after-import workflow
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
- ADR-0008: Require clean Git repositories for milestone one
- ADR-0009: Enforce lightweight default resource budgets
- ADR-0010: Use cold disk checkpoints for rollback and fork
- ADR-0011: Keep MCP execution in the guest with endpoint-bound remote transport
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

### Phase 0: Project Decisions

- Select the final Go module path.
- Select the open-source license.
- Confirm the minimum macOS version.
- Pin a maintained stable libkrun release and compatible libkrunfw artifact.
- Decide whether runtime artifacts are downloaded, bundled, or discovered from
  an installation.
- Confirm the session and image-cache layout and name the Apple Silicon
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

This phase is disposable research, not product implementation. Put the
spike in a throwaway tree or `research/` directory. Do not start
`cmd/abox-vmm` or treat spike code as the runtime. Record the working
call sequence, then throw the spike away or isolate it from the module
that ships.

Perform this spike on the dedicated Apple Silicon host. Isolation claims
stay Planned until the recorded sequence is copied into documentation
and later product code.

- Link the pinned libkrun and libkrunfw from a narrow cgo helper.
- Compare that pin to `containers/libkrun` main. Record whether the pin still
  has `krun_disable_implicit_*` and whether `krun_set_exec` returns
  `-ENOTSUP`.
- Build the intended release device plan from section 5.2:
  `krun_add_vsock(ctx, 0)`, no net, no host-path virtio-fs, two raw disks.
- Decide guest process configuration: ABox-owned init plus
  `krun_append_kernel_cmdline`, or libkrun_init plus `.krun_config.json`.
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
- Init ownership (ABox init vs libkrun_init) is written down.
- Failures change ADR-0002 and section 5.2; they do not silently add
  host-path virtio-fs, TSI, or a guest NIC.

### Phase 1: Documentation First

- Complete README, architecture, threat model, roadmap, ADRs, and AGENTS.md
  from the Phase 0 drafts and the Phase 0.5 research record.
- Mark all implementation and security controls as planned, not implemented,
  unless Phase 0.5 already verified them on the named host.
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
- Create provider, runtime, protocol, agent, repository, patch, and TUI package
  boundaries.
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
- Add typed package-fetch and MCP stream methods with no raw URL or
  destination fields.
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

- Validate a clean Git repository.
- Create the deterministic snapshot.
- Stream and safely extract it in the guest.
- Initialize the private guest baseline.
- Verify guest changes do not change host files.

Exit criteria:

- Clean repositories transfer correctly.
- Dirty repositories and submodules fail clearly.
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

Implement the broker before any remote MCP client code.

- Implement offline and direct host routing for the broker.
- Implement `FetchPackage` and MCP stream-open, stream-read, cancel, and
  host-push frames.
- Implement guest origin rewrite, including response-body rewrite and
  secondary `index_id` entries.
- Reject raw URLs, CONNECT, proxy-variable, and unconfigured origins.
- Then implement guest-side MCP initialization, discovery, and tool
  invocation on top of that broker.
- Launch stdio MCP servers only inside the guest.
- Add per-server and per-tool approvals.
- Translate discovered MCP tools into the provider tool set with provenance.
- Add malicious schema, oversized output, cancellation, timeout, and server
  crash tests.
- Add SSRF, raw-URL, redirect, header-injection, and offline-refusal tests.

Exit criteria:

- A local stdio MCP tool runs entirely in the guest.
- A remote MCP tool in direct mode reaches only its configured endpoint
  through stream methods.
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
- Bidirectional RPC allowlists and MCP stream framing
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

1. Start `abox` in a clean Git repository on Apple Silicon.
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
    import. After import the host worktree is dirty; the next session requires
    a commit or another clean tree.
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
- Dirty-worktree transfer
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
- Repositories use Git and can begin from a clean worktree.
- Model calls originate from the trusted host supervisor.
- The guest has no NIC in every first-milestone connectivity mode.
- Offline is a broker and provider-routing mode. Direct mode can acquire
  configured packages and reach configured remote MCP servers. Required
  agentgateway mode can reach configured remote MCP servers only through the
  gateway and refuses package acquisition in the first milestone. None of
  these modes changes the guest device plan.
- Package tools in the guest use origin rewrite. They do not use HTTP(S)
  proxy variables to reach HTTPS indexes.
- Users accept that ignored local files are not present in the guest.
- Users accept that the initial image has a limited toolchain set.
- Users accept that a successful patch import leaves a dirty host worktree.
- The host and local administrator are trusted.
- The guest, model output, generated code, repository content, and
  repo-sourced instruction files are untrusted. Host configuration is the
  only source of approval, connectivity, limit, and tool-allowlist policy.
- Phase 0.5, Phase 6, and Phase 18 require a dedicated Apple Silicon host
  that can use Hypervisor.framework. Nested cloud macOS runners are not that
  host.

## 27. Blockers and Open Questions

The following must be resolved during Phase 0 or early implementation:

- Final Go module path
- Open-source license
- Minimum supported macOS version
- Exact pinned libkrun and libkrunfw versions, including whether the pin is
  `stable-1.19.x` or a main-line commit after the implicit-API removal
- Phase 0.5 result: `krun_add_vsock(ctx, 0)` boot, two-disk config, vsock
  listen direction, and ABox-init versus libkrun_init
- Guest-side rewrite rules for pip, npm, and cargo absolute follow-up URLs
- Runtime artifact distribution and code-signing approach
- Reproducible guest image build environment
- Image update and vulnerability-response policy
- Final macOS session and image-cache layout
- Named demonstration repository and toolchain set for the image budget
- Named Apple Silicon baseline machine for resource budgets
- Dedicated non-nested Apple Silicon hardware runner, decided in Phase 0
- Validation or evidence-based adjustment of the initial resource budgets
- Exact explicit-confirmation interaction for patch import
- Go sum-database policy for origin-rewritten `GOPROXY`

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
