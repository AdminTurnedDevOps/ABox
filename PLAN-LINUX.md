# ABox Linux Support — libkrun/KVM Host Port

## Context

This plan began when ABox ran only on Apple Silicon: the VMM binding was tagged
`darwin && arm64`, the stub refused every other platform, and the runtime used
the Darwin-only `unix.Clonefile`. Those porting blockers have now been removed.
The shared libkrun binding, Linux clone path, architecture manifests, rootless
image builder, Linux lifecycle signals, and Secret Service keystore are
implemented in the working tree.

Implementation is not support evidence. Linux VMM execution, Linux release
support, and every Linux/KVM isolation claim remain **Planned** until separate
Phase 0.5 and Phase 18 records pass on both pinned x86_64 baselines against the
exact artifacts under test. macOS/HVF evidence cannot cover KVM, and Arch
evidence cannot cover Fedora.

Current phase status:

| Area | Status |
| --- | --- |
| Portable compile/binding, lifecycle, image identity/builder, keystore | Implemented; unit/build verification required |
| Linux preflight and mapped KVM/loader/SELinux diagnostics | Implemented; real KVM/SELinux behavior remains part of the hardware gate |
| Arch/Fedora build-only CI and gated Linux release workflow | Implemented plumbing; no checked-in passing hardware evidence and no support claim |
| Linux Phase 0.5 and Phase 18 hardware evidence | Planned; runtime/isolation support gate is closed |

libkrun's primary Linux backend is KVM, while macOS uses Hypervisor.framework.
Inspection of the Arch `libkrun 1.19.4` x86_64 package (Sept 2026) shows that all
libkrun symbols currently used by the shared `start_libkrun.go` binding exist, including
`krun_add_vsock(ctx, 0)`, whose header specifies no TSI hijacking. Symbol and
header compatibility establish that a spike is possible; they do **not** prove
identical KVM behavior. Phase 0 must verify that the intended device plan — no
NIC, no TSI inet/Unix, no host-path virtio-fs, two raw disks, one vsock port —
actually has those semantics on each pinned Linux baseline.

The isolation design and agent protocol remain unchanged, but this is more than
a cgo/syscall port: image identity/building, session compatibility metadata,
credential persistence, lifecycle handling, CI/release plumbing, and platform
documentation all change. The agent loop, host LLM/MCP brokers, typed RPC, and
guest rootfs package/tool set remain common across hosts.

**Goal:** `abox` runs natively on Arch and Fedora (x86_64) with the security
posture preserved, and `make image` builds the guest disk with no Docker, no
root, and no privileged container.

### User decisions (Sept 2026)

- **Distros:** Arch and Fedora are the pinned x86_64 build and future runtime
  baselines. Runtime support remains Planned until both hardware gates pass.
  Debian/Ubuntu is a source build explicitly marked untested; neither ships the
  pinned prebuilt libkrun/libkrunfw package pair.
- **Image builder:** rootless native on Linux; macOS keeps its Docker packer.
- **CPU arch:** x86_64 first, parameterized by `GOARCH` so linux/arm64
  (Fedora Asahi) is a configuration change rather than a rewrite.
- **Local keystore:** Secret Service via libsecret (`secret-tool`), covering
  GNOME Keyring, KWallet (`org.kde.secretservicecompat`) and KeePassXC.
- **Config naming:** new portable `keystore` source name, with `keychain` and
  `secretservice` accepted as aliases so existing configs keep working.
- **Headless policy:** plaintext fallback is preserved so nothing breaks, but it
  now warns rather than degrading silently.

### Credential parity (in scope)

At the start of the port, four of the five credential sources already worked on
Linux unchanged.
`env`, `vault`, `azure`, and `aws` contain no `GOOS`/darwin/macOS-path
references — a direct consequence of PLAN-CRED.md global decision 2 (stdlib
HTTP or CLI subprocess only, no SDKs, no cgo). Only the local `keychain`
implementation was macOS-only. It is now the portable `keystore` source,
dispatched to macOS Keychain or Linux Secret Service.

The gap was therefore the *local, no-infrastructure* keystore. The implemented
Linux path probes Secret Service and uses a warned 0600 `credentials.env`
fallback when it is absent, locked, loses its provider, or times out.

Credential sources are siblings, not a stack: `Resolver.Resolve` performs a
single `r.sources[ref.Source]` lookup with no chaining, so each model or MCP
server names exactly one source and different entries may name different ones.
Adding a local keystore does not alter the cloud sources in any way.

**Deferred:** `systemd-creds` (TPM2-sealed, `--user` scoped; systemd 261 and
/dev/tpm0 confirmed on the dev host) and `pass`. Cloud sources are the
documented headless answer instead.

## Global decisions

1. **No protocol change.** `protocol.Version` stays 4. Host/guest framing, the
   `vmmconfig.Config` stdin contract, `session.WritePaddedConfig`, and the guest
   image contents are platform-independent and untouched.
2. **cgo flags split from cgo logic.** cgo accumulates `#cgo` directives
   package-wide across files in a package, so the ~90-line `startVM` body is
   shared and only the preamble files are platform-tagged. No duplicated binding.
3. **Linux links `-lkrun` only, via pkg-config.** The verified Arch libkrun
   1.19.4 package `dlopen`s `libkrunfw.so.5` at runtime (and has no undefined
   `krunfw_*` symbols), so `-lkrunfw` is omitted. The exact firmware SONAME is a
   property of the pinned distro libkrun package, not an ABox-wide constant;
   Fedora support must record and test its compatible pair. `#cgo pkg-config:
   libkrun` resolves at link time under pacman, dnf, and a source install when
   `PKG_CONFIG_PATH` includes `/usr/local/lib64/pkgconfig`. Runtime discovery is
   separate and covered by Phase 5; macOS keeps its explicit Homebrew paths and
   `-lkrunfw`.
4. **`cloneFile` becomes platform-split, and gains a Linux fast path.**
   `unix.IoctlFileClone` (FICLONE reflink; btrfs, XFS `reflink=1`, bcachefs) then
   `unix.CopyFileRange` (in-kernel copy, works on ext4) then the existing
   `io.Copy` fallback. Fedora defaults to btrfs, so the 768 MiB per-session
   golden-image clone is near-instant there.
5. **The golden image path and manifest carry its architecture.** An arm64
   rootfs booted under an x86_64 kernel is an unexplained guest panic; a silent
   failure mode is unacceptable. `config.GuestImageName`
   (internal/config/config.go:17) becomes a function of `runtime.GOARCH`. Every
   image, including a configured custom path, has an adjacent manifest with a
   schema version, guest architecture, image ID, guest protocol, and SHA-256.
   Published names and the local current-generation pointer are architecture
   tagged. The bare `abox-guest.raw` is discovery-only compatibility data, not
   trusted architecture evidence. On darwin/arm64, where all previously runnable
   images were arm64, a successful one-time validation may write its manifest.
   Linux refuses a metadata-free legacy image with a rebuild/migration
   instruction; it never infers architecture from the host.
6. **`/dev/vhost-vsock` is not a requirement.** libkrun implements virtio-vsock
   in userspace and maps guest ports onto host Unix sockets via
   `krun_add_vsock_port` — exactly how `sess.RPCSocket()` already works. There
   are zero `vhost` or `/dev/vsock` references in the library. No vsock kernel
   module, no `CONFIG_VHOST_VSOCK`, no CID allocation.
7. **Linux isolation claims start at Planned, independently of macOS.** Per
   `AGENTS.md`, claims stay Planned until the hardware suite passes. macOS
   evidence must not implicitly cover a KVM backend.
8. **The OS keystore is selected at runtime by `runtime.GOOS`, not by build
   tag**, matching the existing `securityToolAvailable(goos, mode)` pattern
   (internal/credsource/keychain.go:42) so unit tests stay platform-independent
   and table-driven.
9. **Secrets never appear in argv.** `secret-tool store` takes the value on
   stdin, written with **no trailing newline** — the man page warns that a piped
   newline is stored as part of the password. Attributes reuse the existing
   `KeychainService = "abox"` constant: `service abox account <ENV_NAME>`,
   mirroring the macOS `-s`/`-a` pair exactly.
10. **`ErrNotFound` vs `ErrLocked` needs a separate availability probe.**
   `secret-tool` returns non-zero for both "no such item" and "no service", so
   exit code alone cannot distinguish them the way macOS exit code 44 does.
    Availability = trusted `/usr/bin/secret-tool` plus a reachable session bus and a
    provider that owns or can activate `org.freedesktop.secrets`. Merely finding
    `DBUS_SESSION_BUS_ADDRESS` is not sufficient. Use a bounded, non-mutating
    D-Bus probe before lookup/store so a bus with no provider is classified as
    unavailable. This preserves the distinction `SavePreferred` depends on for
    its fallback.
11. **Linux release support has a hardware gate.** Generic GitHub-hosted CI proves
    compile and unit behavior only. Publishing a supported Linux artifact or
    changing a KVM isolation claim from Planned requires every applicable
    §21.4/§22 test on named x86_64 KVM hosts for both Arch and Fedora, against the
    exact commit and artifacts being released. A boot probe or symbol inspection
    is not a substitute.
12. **`keystore` is the canonical persisted credential source.** `keychain` and
    `secretservice` remain accepted input aliases. Config load canonicalizes all
    three in memory, config save writes `keystore`, and resolver, save, migration,
    MCP OAuth, and TUI paths all use one shared canonicalization helper. Validation
    itself does not pretend to mutate a value receiver.
13. **Support names exact host baselines.** The initial baselines are Arch's
    2026-09-17 x86_64 snapshot with libkrun 1.19.4-1/libkrunfw 5.5.0-1, and
    Fedora 44 x86_64 updates with libkrun/libkrun-devel 1.19.0-1.fc44 and
    libkrunfw 5.5.0-1.fc44 (`libkrun.so.1`, `libkrunfw.so.5`). Each baseline
    records the required exported symbols and pkg-config metadata. Evidence from
    one baseline is not evidence for the other.
14. **WSL2 and containerized ABox runtimes are unsupported initially.** Containers
    remain valid compile/image-build environments, but running the VMM inside a
    container or WSL2 adds device, seccomp, namespace, and lifecycle surfaces not
    covered by this port. Detect those environments and fail clearly even when
    `/dev/kvm` happens to exist. A conventional VM with explicitly enabled nested
    KVM remains supported only after the same hardware suite passes there.
15. **Approved guest commands do not inherit guest-supervisor privilege.** The
    root-owned guest supervisor drops shell and Git subprocesses to UID/GID 1000,
    owns `/work/repo` with that identity, strips setuid/setgid bits from the
    image, and kills the command process group on completion or cancellation.
    This keeps `allow_once` from replacing the guest agent or leaving a daemon
    that bypasses a later approval.

## Verified findings (Sept 2026)

Checked against `extra/libkrun 1.19.4-1` and `extra/libkrunfw 5.5.0-1`, and
against `golang.org/x/sys@v0.47.0` as pinned in `go.mod`.

These package and SONAME findings are Arch-only. They justify the initial spike,
not a Fedora behavior claim. Fedora package metadata confirms the pinned versions
and SONAMEs in Global decision 13; Phase 0 still must prove their runtime behavior.

| Claim | Evidence |
| --- | --- |
| All libkrun symbols currently called by ABox exist on Arch Linux x86_64 | `libkrun.h` + `nm -D` on `libkrun.so.1.19.4` |
| `KRUN_FEATURE_BLK` is compiled in | `RegisterBlockDevice` / `OpenBlockDevice` / `AttachBlockDevice` present in the `.so` |
| `-lkrunfw` unnecessary on Linux | libkrun `dlopen`s `libkrunfw.so.5`; no undefined `krunfw_*` |
| vsock is userspace; `/dev/vhost-vsock` unused | No `vhost` / `/dev/vsock` strings |
| KVM is the backend | 3 `/dev/kvm` refs; reads `/sys/module/kvm_*/parameters/nested` |
| `mke2fs -d` builds ext4 unprivileged | Ran as uid 1000, exit 0, no loop mount, no Docker |
| `fakeroot` gives correct `0:0` ownership | `debugfs -R "ls -l"` shows `0 0`, versus `1000 1000` without it |
| `unix.Clonefile` is darwin-only | Defined only in `zsyscall_darwin_{amd64,arm64}.go` |
| Linux reflink helpers available | `IoctlFileClone` ioctl_linux.go:184; `CopyFileRange` zsyscall_linux.go:635 |
| `vault` / `azure` / `aws` are platform-clean | No `GOOS`/darwin/macOS-path references in those three files |
| `azure` CLI fallback avoids repository PATH shadowing | Resolves only fixed system/Homebrew locations and uses a minimal environment |
| `secret-tool store` takes the secret on stdin | `man secret-tool` STORE; warns a piped newline becomes part of the secret |
| Missing key exits 1 with empty stdout | Probed `secret-tool lookup` directly on the dev host |
| Exit codes cannot separate not-found from unavailable | `man secret-tool` EXIT STATUS: "0 on success, a non-zero failure code otherwise" |
| Secret Service is live on the dev host | `gnome-keyring-daemon --components=pkcs11,secrets` owns `org.freedesktop.secrets` |
| KWallet / KeePassXC use the same API | `org.kde.secretservicecompat` activatable on the session bus |
| Fedora 44 libkrun baseline | Fedora package metadata: `libkrun-1.19.0-1.fc44`, provides `libkrun.so.1` |
| Fedora 44 firmware baseline | Fedora package metadata: `libkrunfw-5.5.0-1.fc44`, provides `libkrunfw.so.5` |

## Original platform surface

At the start of this plan, the host runtime, VMM binding, session metadata,
credential path, two image scripts, Makefile, and CI all needed changes. This
table is retained as the historical implementation inventory; the status table
above is authoritative now. Already portable and untouched were:
`protocol/`, the LLM/MCP brokers, `internal/agent`, `internal/vmmconfig`, and
`config.Dir()` (`~/.abox`, with `ABOX_HOME` override — the `~/Library/...`
lookups are legacy read-only fallbacks that no-op on Linux).

| Location | Issue |
| --- | --- |
| `internal/runtime/runtime.go:930` | `unix.Clonefile` — **compile break**, darwin-only |
| `cmd/abox-vmm/start_darwin_arm64.go` | Body portable; only `#cgo` paths are Homebrew-specific |
| `cmd/abox-vmm/start_stub.go:1` | Tag `!darwin \|\| !arm64` swallows Linux |
| `internal/runtime/runtime.go:288` | `DYLD_LIBRARY_PATH` in the fixed `cmd.Env` |
| `internal/runtime/runtime.go:301-329,902-925` | Failure/stop paths must reap the helper and remove stale sockets |
| `internal/session/session.go:16-22` | Resume metadata has no guest architecture, image identity, or backend |
| `internal/credsource/keychain.go:23,43` | Hardcoded `/usr/bin/security` — the only credential-source gap on Linux |
| `internal/mcpauth/oauth.go:122-125` | OAuth persistence rejects a canonical `keystore` source |
| `internal/mcpauth/oauth.go:666-668` | Browser launch is hardcoded to macOS `open` |
| `cmd/abox/creds.go:35` | macOS-only error copy |
| `cmd/abox/main.go:30-37,127-199,270-283` | Signal ownership is not above boot, TUI, and exec; exec alone watches `os.Interrupt` |
| `internal/guest/tools/freeze_linux.go:1` | Linux host tests compile the guest-only FIFREEZE/FITHAW implementation |
| `images/build-guest.sh`, `images/update-guest-bin.sh` | Hardcoded `-linux-arm64`; `--privileged` loop mount |
| `Makefile` | `codesign` unconditional; guest pinned to `GOARCH=arm64` |
| `.github/workflows/test.yml` | `macos-latest` only |

---

## Phase 0 — Spike: boot a VM before changing anything

Throwaway code. Nothing downstream matters if this fails. Run the complete spike
independently on both pinned baselines: Arch libkrun 1.19.4-1/libkrunfw 5.5.0-1
and Fedora 44 libkrun 1.19.0-1.fc44/libkrunfw 5.5.0-1.fc44. Do not treat an Arch
success or Fedora package metadata as Fedora runtime evidence.

```bash
# Arch uses the 2026/09/17 Arch Linux Archive repository snapshot.
sudo pacman -S --needed base-devel pkgconf libkrun libkrunfw fakeroot e2fsprogs curl
# Fedora runs on Fedora 44 and installs the exact NEVRAs from Global decision 13.
sudo dnf install gcc make pkgconf-pkg-config libkrun-1.19.0-1.fc44 \
  libkrun-devel-1.19.0-1.fc44 libkrunfw-5.5.0-1.fc44 fakeroot e2fsprogs curl
```

The workflow configures the archived Arch repository URL and checksum-pinned
Fedora RPM/repository metadata; a moving mirror is not a baseline.

1. Copy `start_darwin_arm64.go` to `start_linux.go`, tag the initial x86_64 spike
   `//go:build cgo && linux && amd64`, replace the preamble with
   `#cgo pkg-config: libkrun`, and temporarily retag `start_stub.go` so exactly
   one `startVM` is selected. Alternatively keep the spike outside the product
   package. Do not combine the Linux implementation with the current broad stub;
   both would define `startVM`.
2. Temporarily reduce `cloneFile` to its `io.Copy` path so `internal/runtime`
   compiles. Phase 1 does this properly.
3. Define and check in the required-symbol manifest that the shared cgo binding is
   allowed to use, derived from the copied Phase 0 body plus the planned Phase 1
   cleanup. The current implementation calls eleven unique libkrun functions and
   Phase 1 adds `krun_free_ctx`, so do not preserve a stale hard-coded symbol
   count; Phase 1 and CI update/check the manifest whenever the binding changes.
   On **both** baselines, compare the installed header and `nm -D` output against
   every manifest entry, explicitly including
   `krun_disable_implicit_vsock`, `krun_add_vsock`, `krun_add_disk3`,
   `krun_set_root_disk_remount`, and `krun_start_enter`. Stop before shared-binding
   implementation if either package is missing an entry.
4. Build an amd64 guest, hand-pack a rootfs with the Phase 3 recipe, and run a
   deterministic probe guest. Do not use an LLM/model-authored command as security
   evidence.
5. Record distro/kernel/CPU, KVM modules, libkrun/libkrunfw package versions and
   SONAMEs, `krun_has_feature(KRUN_FEATURE_BLK) == 1`, the exact libkrun call
   sequence, root remount result, and the working vsock listen direction.
6. Inspect `/sys/class/net`, `/sys/bus/virtio/devices`, mounts, `/dev/dri`, and
   `/dev/snd`: loopback exists, but no NIC, GPU, sound, or host-path filesystem is
   attached. GPU/sound may be compiled into a distro libkrun without appearing in
   the device plan.
7. Prove guest-local loopback and Unix sockets work, then prove deterministic
   guest probes cannot reach host TCP/UDP canary listeners, host Unix-socket
   canaries, LAN, or external IPv4/IPv6. This is the early proof that
   `krun_disable_implicit_vsock` plus `krun_add_vsock(ctx, 0)` disabled both TSI
   inet and Unix hijacking on KVM.

**Exit criterion:** on **both** Arch 1.19.4-1 and Fedora 1.19.0-1.fc44, the
header/`nm -D` required-symbol check passes and `--probe-vm` lists guest files;
the recorded device/network probe proves BLK is enabled, vsock RPC works, no
unintended device is attached, and TSI inet/Unix reachability fails. Keep separate
evidence records for each baseline. This is still spike evidence, not a substitute
for the complete release hardware matrix.

---

## Phase 1 — Compile on Linux, and a portable libkrun binding

**1a. Fix the `cloneFile` compile break** (internal/runtime/runtime.go:928).
Split by platform, keeping the existing `io.Copy` tail shared:

- `internal/runtime/clone_darwin.go` — `unix.Clonefile` fast path
- `internal/runtime/clone_linux.go` — `unix.IoctlFileClone`, then
  `unix.CopyFileRange`, then fallback
- `internal/runtime/clone_other.go` — fallback only

`Prepare` (internal/runtime/runtime.go:224) keeps its current signature and
semantics; only the copy mechanism changes. Each failed fast path must leave the
destination at offset zero and truncated before the next path starts. A failed
clone removes the partial destination. Unit tests cover unsupported reflink,
cross-filesystem errors, a partial `CopyFileRange`, source read failure, cleanup,
and final mode 0600.

**1b. Portable cgo binding** in `cmd/abox-vmm/`:

- `cgoflags_darwin_arm64.go` — `//go:build cgo && darwin && arm64`; preamble only,
  carrying today's Homebrew include/lib paths, `-lkrun -lkrunfw`, and rpath
- `cgoflags_linux.go` — `//go:build cgo && linux && (amd64 || arm64)`; preamble only:
  `#cgo pkg-config: libkrun`
- `start_libkrun.go` — the shared `startVM`, tagged
  `cgo && ((darwin && arm64) || (linux && (amd64 || arm64)))`; it keeps its own
  `#include <libkrun.h>` and `import "C"`, because C names are file-local even
  though `#cgo` flags accumulate package-wide
- `start_stub.go` — the exact complement, including `!cgo`; message rewritten to
  name the actual per-OS requirement rather than asserting macOS

Test both `CGO_ENABLED=1` and `CGO_ENABLED=0` on supported architectures. The
second must compile the explicit diagnostic stub rather than fail with an
undefined `startVM`.

**1c.** Make the fixed `cmd.Env` at internal/runtime/runtime.go:288
platform-conditional; drop `DYLD_LIBRARY_PATH` on Linux, where `/usr/lib` is
already on the default search path.

**1d.** Reword cmd/abox/creds.go:35 so the message names the platform's actual
keystore situation instead of asserting macOS.

**1e. Close and reap every VMM resource path.** Confirm `krun_free_ctx` in the
pinned ABI and release the configuration context on every setup error before
ownership is transferred to `krun_start_enter`. Mark it consumed before that
call; `krun_start_enter` consumes the context, so do not free it if the call
returns. Check the currently ignored console-output return. In `runtime.Start`,
every post-`cmd.Start` failure must kill and `Wait` for the helper, close the
listener, and unlink the socket. `Sandbox.Stop` keeps graceful guest shutdown
followed by a platform-specific helper signal and bounded kill, but always waits
for the final process state and removes the socket. Linux tests observe SIGTERM
(never `os.Interrupt`), then forced SIGKILL when required; tests also cover failed
boot, repeated stop, and no zombie or stale socket.

`krun_get_shutdown_eventfd` is available only in the libkrun EFI variant and is
not part of the generic Linux path. Linux orderly stop has one exact sequence:
guest shutdown RPC with a deadline, SIGTERM to `abox-vmm`, a bounded wait, then
SIGKILL and `Wait` if it has not exited. Do not use `os.Interrupt`/SIGINT as the
Linux helper fallback. Supervisor death closes the liveness pipe and the helper
exits immediately because no supervisor remains to coordinate guest RPC.

**1f. Supervisor liveness is an inherited capability.** Implement the liveness
pipe required by `PLAN.md` §4.2: the supervisor retains the write end, passes
only the read end to `abox-vmm` as a fixed inherited descriptor, and the helper
terminates the VM when it observes EOF. No unrelated child inherits the write
end. Record a helper PID only for validated stale cleanup; verify uid, executable,
session ownership, and process start identity before signaling it. Test abrupt
supervisor death without `Sandbox.Stop` and prove no helper or VM remains.

The helper's inherited descriptor contract is exactly stdin for the validated
config, stdout/stderr for diagnostics, and liveness read fd 3. Go `os/exec`
normally closes descriptors not listed in `ExtraFiles`; retain that behavior and
test it with sentinel parent descriptors so D-Bus, systemd activation, terminal,
and unrelated sockets cannot become ambient helper capabilities. Any descriptors
opened later by libkrun must arise from the validated device plan.

**1g. Linux signal handling.** `main` creates one signal context before calling
`run(ctx)`; it watches `os.Interrupt`, SIGTERM, and SIGHUP on Linux. Thread that
context through VM preparation/start, exec turns, and a context-aware `tui.Run`
so both TUI and headless paths return through the existing deferred
`Sandbox.Stop`. Remove the inner signal owner from `runExec`; it consumes the
top-level context instead. A second termination signal may force exit. Tests send
all three signals while booting and while each TUI/exec path is ready;
SIGTERM/SIGHUP do not skip cleanup merely because the liveness pipe would
eventually kill the helper. SDK examples may keep their own `os.Interrupt`
contexts; SDK lifecycle remains the caller's `Session.Close` contract.

**1h. Keep guest filesystem freeze out of host tests.** The Linux build currently
selects `internal/guest/tools/freeze_linux.go`, whose `Freeze()` targets `/`, for
ordinary host `go test`. Put FIFREEZE/FITHAW behind an explicit guest-only build
tag (for example `linux && abox_guest`), compile `abox-guest` with that tag, and
select the non-freezing stub for Linux host unit tests. Add a build/test assertion
that an untagged host test can never issue the root-filesystem ioctls.

---

## Phase 2 — Multi-arch guest and image identity

- `Makefile`: derive `GUEST_ARCH` from `go env GOARCH` (overridable); build
  `bin/abox-guest-linux-$(GUEST_ARCH)`.
- `Makefile`: guard the `vmm` target's `$(MAKE) sign` with
  `$(filter darwin,...)` so `codesign` is a no-op off darwin.
- Arch-tag the golden image and create/verify its manifest per Global decision 5.
- `runtime.Prepare`: for a new session, verify the manifest SHA-256 and refuse an
  architecture or protocol mismatch before cloning. Resolve the current-generation
  pointer once, retain a shared generation lock through manifest read and clone,
  and hash the resulting session `root.raw` before accepting it. The builder and
  garbage collector take the exclusive side of that lock, so a pointer swap or GC
  cannot change/remove the selected generation mid-prepare. Custom images are
  opened once and the cloned destination is hashed against their manifest, which
  detects concurrent source mutation. A failed check removes the session disk.
  Persist manifest schema, guest arch, image ID/digest, guest protocol, and VMM
  backend (`hvf` or `kvm`) in `session.json`.
- Resume validates the stored guest architecture against the running host before
  starting `abox-vmm` and requires the recorded guest protocol to be in the host's
  supported range. It does not compare the mutable `root.raw` with the current
  golden image. Existing sessions are concrete compatibility data: darwin/arm64
  may backfill `guest_arch=arm64` because it was the only old runnable backend,
  then records protocol only after a successful compatible hello; Linux rejects
  metadata-free sessions with an actionable migration error rather than attempting
  an unknown disk.
- Normal Open/Resume requires manifest/session guest protocol to equal the current
  supported protocol range (initially protocol 4), rejects a future protocol
  rather than silently capping it, and confirms the hello value matches recorded
  metadata. `--probe-vm` is the one diagnostic exception: it may boot an older
  protocol image to test liveness, but must not resolve/push credentials, install
  host brokers, resume a real session, or make isolation claims from that result.
- Add session round-trip, cross-architecture rejection, custom-image manifest,
  digest mismatch, pointer-swap/GC race, concurrent custom-image mutation,
  protocol mismatch/future protocol, credential-free probe, legacy-image, and
  legacy-session tests. This means `internal/session` is a Phase 2 touchpoint,
  not an untouched package.

---

## Phase 3 — Rootless native image builder (Linux)

Replaces Docker, `--privileged`, and `mount -o loop` entirely on Linux. Verified
working. `images/build-guest.sh` selects on `uname`; the macOS path is unchanged,
because nothing else can build a Linux ARM64 tree there. Linux writes an immutable
generation directory containing the image and manifest, validates the complete
pair, then atomically swaps one architecture-tagged `current` symlink to that
generation. `GuestImagePath` resolves through the pointer. A crash before the
single pointer rename leaves the prior generation selected; old generations can
be garbage-collected only when no session/build references them.

```sh
fakeroot sh -c '
  apk.static --arch "$APK_ARCH" --root "$ROOTFS" --initdb --keys-dir "$KEYS" add alpine-base git patch
  install -Dm0755 bin/abox-guest-linux-$ARCH "$ROOTFS/usr/local/bin/abox-guest"
  mkdir -p "$ROOTFS/work/repo" "$ROOTFS/abox-config" "$ROOTFS/tmp"
  printf "nameserver 1.1.1.1\nnameserver 8.8.8.8\noptions ndots:1\n" > "$ROOTFS/etc/resolv.conf"
  mke2fs -q -F -t ext4 -d "$ROOTFS" -b 4096 "$OUT" 768M
'
```

`apk.static` is a static binary that runs on any distribution; fetch the host
architecture binary from a versioned Alpine URL pinned by SHA-256, while
`--arch` selects the target rootfs architecture (`amd64` maps to `x86_64`,
`arm64` to `aarch64`). Pin the Alpine release, repositories, keys, and package
versions used for release images; do not resolve an unversioned moving repository
during a release. `fakeroot` is required — without it `mke2fs -d` stamps the
invoking uid, producing a rootfs owned by `1000:1000`. Rootfs contents must stay
byte-identical in spirit to the Docker path: alpine-base, git, patch,
`/usr/local/bin/abox-guest`, `/work/repo`, `/abox-config`, resolv.conf.

Before publishing, run `e2fsck -fn`, use `debugfs` to verify `/` and the guest
binary are owned by `0:0`, verify mode 0755, and extract/read the guest ELF header
to prove it matches the manifest architecture. Check free space before allocating
the 768 MiB image and clean every temporary file on failure.

`images/update-guest-bin.sh`: prefer a full rebuild on Linux. If the debugfs fast
path is retained, create a new immutable generation, explicitly set the new inode
to uid 0, gid 0, and mode 0755, run `e2fsck -fn`, verify the ELF architecture,
write the new manifest, then atomically swap the generation pointer. A raw
`debugfs write` of a user-owned host binary or two independent renames is not
sufficient.

This advances the existing README note that replacing the packer is follow-up
work, and removes `--privileged` from the flow of a project whose thesis is
isolation. Worth stating plainly in the README.

---

## Phase 4 — Credential parity: OS keystore on Linux

Goal: `/provider` and `/mcp` stop writing plaintext on a Linux desktop. Scope is
the local keystore only — `vault`, `azure`, and `aws` already work untouched.

**4a. Generalize the keystore abstraction.** `internal/credsource/keychain.go`
becomes an OS-keystore dispatch selected at runtime by GOOS (Global decision 8).
The `Source` interface and `Resolver` wiring are unchanged:

- an `osKeystore` interface — `Available() bool`, `Get`/`Set`/`Delete(ctx, name)`
- `keychainStore` — today's `security(1)` implementation, moved intact
- `secretServiceStore` — new, `secret-tool` subprocess
- package-level `KeychainAvailable`/`SetKeychain`/`DeleteKeychain` become
  `OSKeystoreAvailable`/`SetOSKeystore`/`DeleteOSKeystore`; update the call sites
  at save.go:14,26,36 and cmd/abox/creds.go:17-18

**4b. Secret Service implementation.** Reuses `KeychainService = "abox"`:

- resolve — `secret-tool lookup service abox account <NAME>`; capture stdout raw
  and **do not** `TrimSpace`, since the stored value is byte-exact and no tty
  newline is appended when piped (this differs from the macOS path, where
  `security -w` does append one)
- set — `secret-tool store --label "abox: <NAME>" service abox account <NAME>`
  with the value on stdin and no trailing newline
- delete — `secret-tool clear service abox account <NAME>`
- validate with `config.ValidEnvName(ref.Name)` before shelling out, exactly as
  `keychainSource.Resolve` (keychain.go:47) already does — this is what makes
  attribute injection impossible
- map errors per Global decision 10; never include a secret value in an error,
  per the `Source` contract
- availability invokes `gdbus` with a context timeout to call
  `org.freedesktop.DBus.StartServiceByName` for `org.freedesktop.secrets`, then
  `NameHasOwner`; this is a non-mutating provider activation/ownership probe and
  keeps the implementation subprocess-only
- map a missing bus/provider and provider loss during an operation to `ErrLocked`
  so `SavePreferred` follows the warned plaintext fallback; map only a confirmed
  missing item to `ErrNotFound`
- confirm item existence through the Secret Service
  `org.freedesktop.Secret.Service.SearchItems` D-Bus method using the fixed
  `service=abox` and validated `account=<NAME>` attributes. No returned locked or
  unlocked object paths means `ErrNotFound`; an existing locked item, prompt
  denial/timeout, transport failure, or `secret-tool` failure is not not-found and
  maps to `ErrLocked` or a typed operational error as appropriate
- `secret-tool store` and the provider activation probe run under the existing
  ten-second save context. Resolve, store, delete, item search, and provider
  activation all use bounded contexts. A timeout, including a blocked graphical
  unlock prompt, kills/reaps the subprocess and maps to `ErrLocked`, producing the
  explicit plaintext-fallback warning rather than a generic failure
- test missing `gdbus`, a missing bus, a reachable bus without a provider, a
  locked collection, a missing item, blocked unlock timeout, subprocess cleanup,
  and a provider disappearing during save

**4c. Config surface.** `credentialSources` (internal/config/config.go:304) gains
`keystore` and `secretservice`. Add a pure `CanonicalCredentialSource` helper;
config load uses it to canonicalize all three spellings to `keystore`, validation
uses the canonical value without relying on mutation, and config save emits the
canonical spelling. `NewResolver` (internal/credsource/credsource.go) resolves
accepted aliases through the same helper so `source: keychain` keeps working on
both platforms.

**4d. Fallback warning.** `SaveResult.Note` already carries a human-readable
string (save.go:34,40,46). Add an explicit warning when falling back to plaintext
so a Linux user learns the key did not reach a keystore, surfaced in the TUI and
by `abox creds migrate`.

**4e. Migration.** `abox creds migrate` needs no logic change once the keystore
dispatches; reword the macOS-only error at cmd/abox/creds.go:35.

**4f. Shared save contract.**
`SavePreferred` returns canonical source `keystore`, and
`internal/mcpauth/oauth.go:persistCredentialReference` must accept and persist it.
Rename keychain-specific result fields where needed so `/provider` and `abox mcp
login` share exactly the same save contract. Add tests for loading each alias,
canonical save output, MCP OAuth persistence, and mixed cloud/keystore configs.

**4g. Wording.** save.go:40 hardcodes "key saved to macOS keychain (service
abox)". Make it platform-accurate, along with `credStatusLabel`
(internal/tui/tui.go:154) and the `/provider`, `/credential`, `/mcp` copy.

**4h. Linux OAuth browser launch.** Replace the hardcoded `open` in
`internal/mcpauth/oauth.go` with runtime dispatch: `open` on macOS and `xdg-open`
on Linux. If no graphical launcher is available, print the already validated
authorization URL and continue waiting for the loopback callback so headless
users can open it manually. Add launcher selection, launch failure, manual flow,
and callback timeout tests; never execute a shell command string.

---

## Phase 5 — Preflight and diagnostics

Expected to be the dominant support burden. Fail with actionable text, never a
raw libkrun return code. In `startVM` (Linux) or `runtime.Start`:

- `/dev/kvm` missing — load `kvm_intel`/`kvm_amd`, or enable virtualization in
  firmware.
- `/dev/kvm` `EACCES` — `sudo usermod -aG kvm $USER`, then log out and back in.
  Arch ships a udev rule granting 0666; Fedora and Debian use `root:kvm 0660`,
  so this will be the most common first-run failure.
- `/dev/kvm` opens but the KVM API/version or VM-creation capability check fails
  — report the actual errno. If the host is itself virtualized, explain that its
  administrator must expose nested virtualization. Do not use
  `krun_check_nested_virt()` for this: that API reports whether nested
  virtualization can be offered to the ABox guest, which ABox does not request.
- compatible libkrunfw SONAME not found — report the SONAME required by the
  installed, supported libkrun package and name the package for the pinned distro
  baseline; do not hardcode `.so.5` into Fedora diagnostics.
- `libkrun.so` linked at build time but not loadable at runtime — report the
  resolved binary dependency and loader search-path remediation.
- libkrun version/API mismatch — enforce the supported version range at build
  time and verify every required symbol, including the transitional
  `krun_disable_implicit_vsock`, in package/release checks. Map an early loader
  `undefined symbol` failure to an incompatible libkrun package rather than an
  RPC timeout. A future libkrun release that removes the transitional API needs a
  deliberate binding migration, not optimistic SemVer acceptance.
- Fedora permissions look correct but KVM, executable mapping, or `root.raw`
  access still returns `EACCES` — identify SELinux enforcing mode and point to the
  relevant `ausearch`/journal AVC inspection. Never suggest disabling SELinux;
  add or package a narrow policy only if the pinned Fedora hardware test proves
  one is required.
- WSL2 or container runtime detected — report it as unsupported for VMM execution
  even if `/dev/kvm` is visible; container jobs remain build-only.

Do not assume `/usr/local/lib64` is on the runtime loader path. Debian/Ubuntu source
installation instructions must include the required loader configuration and
`ldconfig` step (or an explicit reviewed rpath policy), and verification runs
`ldd`/`readelf` before the boot probe. Because libkrun dlopens libkrunfw, the boot
probe must also prove the firmware library can actually be found.

`runtime.Start` must race guest socket accept against helper exit. Capture a
bounded, redaction-safe helper diagnostic stream while still making it available
for debugging, reap immediately on early exit, and return the mapped KVM/loader
error instead of waiting for a generic RPC accept timeout. SDK callers receive
the same actionable error as the CLI.

Before invoking Go/cgo, the Makefile's Linux VMM target checks `pkg-config` for
the supported libkrun range and prints a short install/source-build instruction.
If cgo is disabled, explain that the resulting `abox-vmm` is the diagnostic stub
and cannot boot a VM. Do not expose raw pkg-config output as the primary guidance.

---

## Phase 6 — CI and release

**Entry criterion:** first land the Phase 7 changes to `PLAN.md` §21.4/§22 and
the Linux support matrix so the hardware workflow consumes named KVM criteria,
not the current Apple-Silicon-only text. Generate a versioned Linux acceptance
manifest from those criteria and pin that manifest digest in each evidence record.

- `.github/workflows/test.yml`: matrix `[macos-latest, ubuntu-latest]`. The Ubuntu
  runner has no KVM and no packaged libkrun/libkrunfw, so it runs unit tests,
  `go vet`, builds `abox`, and compiles the explicit VMM stub with
  `CGO_ENABLED=0`. It does not build libkrunfw from source or claim real VMM link
  coverage.
- Add package/build jobs in Arch and Fedora containers. These require no KVM but
  prove the documented package names, pkg-config data, cgo link, image-builder
  prerequisites, required libkrun symbols/firmware SONAME, and both cgo-enabled
  and cgo-disabled build-tag paths. These are the real Linux cgo/pkg-config jobs.
- Pin the build containers by digest: the initial x86_64 baselines are
  `archlinux:base-devel@sha256:4894f5a268c696fad671966f383175a13faf433c9d9c88cdd4e32eaa2d18838b`
  and
  `fedora:44@sha256:43b29f65a41eb9c35e1cd5323e3bdf3b655c2357a9f4f1ff2f9c2798e5045d80`.
  Package installation is also pinned as described in Phase 0; image tags or
  moving distro repositories alone are not reproducible CI.
- In both distro containers, create an unprivileged user and run `make image`
  with no Docker daemon and no KVM. Verify manifest/digest, ELF architecture,
  root ownership/mode, clean `e2fsck`, and that an injected failure leaves the
  prior current-generation pointer unchanged.
- Matrix the guest cross-build over `amd64` and `arm64`.
- Add a separate hardware workflow on named physical x86_64 KVM hosts for Arch
  and Fedora. It runs the complete `PLAN.md` §21.4/§22 suite, lifecycle tests, and
  packaged-artifact smoke test. Record host CPU, distro/kernel, KVM modules,
  libkrun/libkrunfw versions and SONAME, commit, artifact digest, SELinux mode,
  runner identity, and results. Use a restricted self-hosted runner group bound
  only to the protected release environment/reusable workflow, plus dedicated
  routing labels such as
  `[self-hosted, linux, x64, abox-kvm, arch]` and the Fedora equivalent; the
  workflow refuses an unrecognized runner/baseline. Labels alone are not a trust
  boundary. Prefer ephemeral runner registration and require environment approval
  for release execution.
- Refactor `release.yml` into version resolution, macOS build, Linux candidate
  build, Arch/Fedora hardware acceptance, and publish jobs. Build jobs upload
  immutable candidates; hardware jobs download and test the exact Linux digest;
  the final publish job depends on both distro results and is the only job that
  calls `gh release create`. It downloads both platform artifacts rather than
  relying on one `macos-15` workspace.
- Linux release publication depends on hardware evidence for the exact commit and
  candidate digest. Each Arch and Fedora hardware job emits its own in-toto
  statement with subject = candidate SHA-256 and a versioned custom predicate
  (for example
  `https://github.com/AdminTurnedDevOps/ABox/attestations/kvm-test/v1`) containing runner
  identity, pinned baseline, acceptance-manifest digest, and test results. Sign it
  through GitHub OIDC from the protected workflow. The publish job verifies two
  distinct valid distro predicates plus build provenance, and attaches both
  evidence statements to the release; ordinary build provenance alone is not
  evidence that hardware tests passed.
- Publish `abox_<tag>_linux_amd64.tar.gz` containing `abox` and `abox-vmm`, an
  amd64 guest binary, a compressed amd64 golden image plus its manifest, and
  SHA-256 checksums. Do not publish a Linux arm64 host artifact until that
  architecture has its own hardware gate.
- Linux archives do not silently bundle system libkrun/libkrunfw. Document the
  supported ABI/package versions and installation paths, then run `ldd`, checksum
  verification, image-manifest verification, and a VM boot from the unpacked
  release archive before upload.

---

## Phase 7 — Documentation and claim hygiene

- README: per-platform prerequisites; Linux quickstart; state that `make image`
  on Linux needs no Docker, no root, and no privileged container.
- README: require Go 1.25+ consistently with `go.mod`; remove the current Go 1.24
  prerequisite and do not list Linux as runnable until the hardware gate passes.
- Per-distro install: Arch (`pacman`), Fedora (`dnf`), Debian/Ubuntu source build
  with pinned and checksum-verified libkrunfw 5.5.0 and libkrun 1.19.4 sources,
  marked **untested**. The exact Debian/Ubuntu sequence installs the kernel build
  toolchain, `python3-pyelftools`, Rust/Cargo, static libc development files,
  `patchelf`, and pkg-config; builds and installs libkrunfw first; then runs
  `make BLK=1 && sudo make BLK=1 install` for libkrun. It configures both
  `/usr/local/lib64` for the loader and `/usr/local/lib64/pkgconfig` for
  pkg-config, runs `ldconfig`, and verifies `KRUN_FEATURE_BLK` before ABox build.
  A plain featureless `make && make install` is not sufficient.
- `docs/troubleshooting.md`: `/dev/kvm` missing/permissions/API failures,
  incompatible libkrun symbols or firmware SONAME, loader/pkg-config failures,
  architecture mismatch, nested virtualization, Fedora SELinux AVC diagnosis,
  Secret Service absence/timeout, and the explicit unsupported status of WSL2 or
  containerized VMM execution. Never recommend disabling SELinux.
- `docs/credentials.md` and the README credentials table: rename the row to
  `keystore`, note it resolves to macOS Keychain or Secret Service per platform,
  and record `keychain`/`secretservice` as accepted aliases. State plainly that
  `vault`, `azure`, and `aws` are cross-platform and are the supported headless
  answer on Linux.
- **`PLAN.md` §22**: the acceptance matrix is written against
  Hypervisor.framework. Give it a per-platform column so KVM enforcement is
  tracked separately, per Global decision 7. Add a named Linux Phase 0.5 and
  Phase 18 evidence record rather than reusing Apple Silicon results.
- Complete a repository-wide platform consistency pass, not only §22. Update
  `PLAN.md` §2/§5/§20/§24/§26 and Phase C so Linux/KVM is a current explicit
  target rather than a first-milestone non-goal or future alternative backend;
  retain Windows and other unimplemented backends as deferred.
- Update `docs/index.md`, `docs/quickstart.md`, `docs/cli.md`,
  `docs/troubleshooting.md`, `pkg/abox/doc.go`, and any generated docs-site copy
  that carries stale platform restrictions, a global `kern.hv_support`
  requirement, a platform-independent Docker requirement, or no Linux VMM path.
- Update `docs/sessions.md` and shutdown/lifecycle prose explicitly: `Close`
  starts with guest shutdown RPC, then macOS uses its existing interrupt fallback
  while Linux uses SIGTERM, followed by bounded kill/`Wait`. Document top-level
  SIGINT/SIGTERM/SIGHUP cleanup for the Linux CLI. Do not rely on the generic
  platform-wording search to catch signal-specific behavior.
- Reconcile `PLAN-CRED.md` and `PLAN-ABOX-SDK.md` with the portable keystore and
  Linux runtime. Preserve historical decisions where useful, but add an explicit
  superseded/current-platform note so those plans do not contradict shipped
  behavior.

---

## Prerequisites (Linux hosts)

**Runtime:** Intel VT-x or AMD-V enabled in firmware; `kvm_intel`/`kvm_amd`
loaded so `/dev/kvm` exists; read/write access to `/dev/kvm`; libkrun and
libkrunfw on the library path. Nested virtualization only if the host is itself a
VM. Default budget is 1 vCPU / 768 MiB per session
(`vmmconfig.DefaultVCPU`, `DefaultRAMMiB`).

The acceptance records must name the Arch repository snapshot and Fedora
release, kernel/update level, libkrun package, required symbols, and matching
libkrunfw SONAME. Fedora must be tested with SELinux enforcing. Those hardware
records do not yet exist. WSL2 and containerized VMM execution are explicitly
outside the initial support table.

**Credentials (optional):** libsecret (`secret-tool`), GLib's `gdbus`, plus a
Secret Service provider — GNOME Keyring, KWallet, or KeePassXC — for the
`keystore` source. Without one, ABox falls back to `credentials.env` at 0600 with
a warning, or use `vault`/`azure`/`aws`, which need nothing platform-specific.
Desktop MCP OAuth additionally uses `xdg-open`; headless users receive a manual
authorization URL.

**Build:** Go 1.25+, gcc, pkg-config, libkrun headers. Image builds additionally
need e2fsprogs ≥ 1.43 (for `mke2fs -d` and `debugfs`), fakeroot, a TLS downloader,
and a SHA-256 utility. No codesign, no Docker. A source install under
`/usr/local/lib64` also needs loader configuration plus `ldconfig` unless the
project adopts and tests an explicit rpath.

Note: Arch's libkrun pulls `libvirglrenderer` and `libpipewire` because GPU and
sound are compiled in. ABox uses neither; this is dependency weight only.

## Risks / notes

1. Architecture-mismatched image produces an opaque guest panic. Mitigated by
   arch-tagged filenames plus the `Prepare` check (Phase 2). Highest-severity
   item in this plan.
2. `/dev/kvm` permissions differ across distributions. Phase 5 preflight is
   still incomplete; manual troubleshooting is documented meanwhile.
3. Host running inside a VM without usable KVM passthrough. This must be
   surfaced by `/dev/kvm` API/version and VM-creation capability checks; do not
   confuse it with offering nested virtualization to the ABox guest.
4. On ext4 there is no reflink, so each session clone is a real 768 MiB copy.
   `CopyFileRange` keeps it in-kernel; btrfs and XFS get true reflink.
5. libkrun version/API/firmware-SONAME skew across distributions. Pinned host
   baselines, build-time version checks, and `krun_has_feature` are implemented;
   required-symbol release checks, mapped early-loader diagnostics, and
   packaged boot tests remain part of the gate.
6. `apk.static` is fetched from the Alpine CDN at image-build time. Pin by
   SHA-256.
7. Debian/Ubuntu source builds of libkrunfw compile a kernel. Documented, not
   supported, not tested.
8. `secret-tool` cannot distinguish "not found" from "no service" by exit code.
   Mitigated by the availability probe (Global decision 10); a wrong mapping
   would either suppress the plaintext fallback or mask a real lookup failure.
9. A trailing newline piped into `secret-tool store` silently becomes part of the
   secret, producing a credential that fails authentication with no visible
   cause. Covered by a round-trip test in Verification.
10. Headless Linux with no session bus still lands keys in plaintext. Accepted
    and now warned; `vault`/`azure`/`aws` are the documented alternative.
11. A reachable D-Bus session without a Secret Service owner looks superficially
    usable. Mitigated by the bounded provider activation/ownership probe and
    unavailable-provider tests.
12. Reflink/copy fallbacks can leave a partial session disk if offsets and
    truncation are mishandled. Mitigated by failure injection and cleanup tests.
13. A release binary can link successfully while libkrunfw remains undiscoverable
    through libkrun's runtime `dlopen`. Mitigated by packaged-artifact boot tests,
    not `ldd` alone.
14. Supervisor death can bypass normal `Sandbox.Stop` cleanup. Mitigated by the
    inherited liveness pipe, validated stale-PID cleanup, and abrupt-death tests.
15. A two-file image/manifest update cannot be atomic. Mitigated by immutable
    generation directories and one atomically replaced current-generation pointer.
16. Fedora SELinux denials can resemble ordinary KVM permission failures.
    Mitigated by enforcing-mode hardware tests and AVC-aware diagnostics without
    recommending that SELinux be disabled.
17. Linux SIGTERM/SIGHUP or supervisor death can bypass a Ctrl-C-only graceful
    path. Mitigated by top-level signal handling plus the liveness pipe.
18. Guest-only FIFREEZE code selected during Linux host tests could freeze the
    developer's root filesystem if invoked. Mitigated by an explicit guest build
    tag and an untagged non-freezing host stub.
19. A Secret Service unlock prompt can outlive the save deadline. Mitigated by
    context-bound subprocess cleanup and `ErrLocked` fallback mapping.

## Verification

1. **Compile and unit:** `make test`, `go vet ./...`, and
   `go build ./cmd/abox ./cmd/abox-vmm` succeed on Linux with cgo enabled against
   libkrun. `CGO_ENABLED=0 go build ./cmd/abox-vmm` also succeeds with the
   diagnostic stub.
2. **VM liveness and API parity:** on both pinned Arch and Fedora baselines, the
   required-symbol header/`nm -D` check passes and `abox --probe-vm` lists guest
   files. Store separate versioned evidence for each package pair.
3. **Pre-gate isolation spot-checks** (useful during development, but not release
   evidence):
   - A deterministic guest probe, not an LLM command, shows loopback but no NIC,
     GPU, sound, or unexpected virtio device.
   - Deterministic TCP/UDP, IPv4/IPv6, and Unix-socket probes cannot reach host
     canaries, LAN, or external endpoints; guest-local loopback/Unix sockets work.
   - A host canary file outside the snapshot directory is unreachable from the guest.
   - VMM configuration attaches only `root.raw` and `config.raw`; no session
     metadata, transcript, console log, socket, or host path is attached.
     `config.raw` is mode 0400 and carries no credential material.
4. **Agent loop:** set a provider key via `/provider`, run a prompt requiring
   `read_file` and `run_command`; confirm the approval prompt appears and
   defaults to deny.
5. **Resume:** `abox --resume <id>` reloads transcript and conversation on a
   matching architecture and protocol; an incompatible or metadata-free Linux
   session fails before VMM start with an actionable error. A future protocol is
   not silently capped, and the hello protocol matches session metadata.
6. **Image builder:** `make image` completes as a non-root user with the Docker
   daemon stopped; `e2fsck -fn` is clean; `debugfs` reports `0 0` and mode 0755
   for the guest; ELF architecture, manifest architecture, and image SHA-256
   agree. An injected failure leaves the prior golden image unchanged.
7. **Credential round-trip:** `/provider` stores a key; `secret-tool lookup
   service abox account ANTHROPIC_API_KEY` returns it **byte-identical** (no
   trailing newline); `~/.abox/credentials.env` does not contain it; a turn
   authenticates against the provider.
8. **Fallback path:** with `DBUS_SESSION_BUS_ADDRESS` unset, a provider absent, or
   an unlock prompt blocked past the save deadline, the same flow warns, reaps
   helper subprocesses, writes to `credentials.env` at 0600, and still completes
   a turn.
9. **Alias compatibility:** existing `source: keychain` config resolves on Linux;
   `keystore` and `secretservice` resolve identically; save emits `keystore`; MCP
   OAuth persists it; an unknown source is still rejected by `config.Validate`.
10. **Cloud sources unaffected:** a model pinned to `source: vault` and another to
   `source: keystore` both resolve in the same session.
11. **Cross-platform regression:** the same suite still passes on Apple Silicon,
    including `security(1)` keychain storage.
12. **Source snapshot regression:** start from clean, dirty, and commitless Git
    worktrees; repository-root discovery, tracked modifications, non-ignored
    untracked files, ignored-file omission, executable bits, and `.git`/ABox
    state exclusion match the macOS behavior.
13. **Lifecycle:** cancel during boot, graceful stop, forced stop, and a failed
    VMM setup leave no helper process, listener, open KVM descriptor, or stale RPC
    socket. Killing the supervisor without calling `Stop` produces the same result
    through the inherited liveness pipe. SIGINT, SIGTERM, and SIGHUP take the
    bounded shutdown path, and sentinel parent descriptors are absent from the
    helper except for the fixed liveness fd 3 contract.
14. **Packaged artifact:** on clean supported Arch and Fedora hosts, verify
    checksums, unpack the release archive, resolve dynamic libraries, validate the
    image manifest, and boot that exact artifact without the source tree. Fedora
    runs with SELinux enforcing and the recorded libkrun/libkrunfw SONAME pair.
15. **Mandatory KVM security gate:** run every applicable `PLAN.md` §21.4/§22
    hardware test on named Arch and Fedora x86_64 KVM hosts. Guest canaries cannot
    reach host home, SSH/cloud/Docker-socket canaries, host listeners, LAN,
    external IPv4/IPv6, or hijacked Unix sockets; mount/device inspection shows no
    NIC, TSI, or host-path filesystem; destructive guest operations leave host
    state unchanged. Capture the evidence against the release digest. Until this
    passes, Linux isolation and Linux release support remain Planned.
16. **Build-only CI:** Ubuntu proves cgo-disabled compilation, tests, and vet;
    unprivileged Arch/Fedora container jobs prove real cgo/pkg-config linking and
    the rootless image builder without KVM or Docker.
17. **Release evidence:** the publish job accepts only the candidate digest tested
    on both protected hardware runners, verifies its provenance/evidence
    attestation, and attaches that evidence to the GitHub release.
18. **Host-test safety:** ordinary Linux `go test` selects the non-freezing stub;
    only the explicitly tagged guest build contains FIFREEZE/FITHAW.
19. **Documentation consistency:** repository/docs search finds no current claim
    that Linux is out of scope, that only Apple Silicon/HVF is supported, or that
    Linux image creation requires Docker. Historical plans carry a clear
    superseded/current-platform note.
