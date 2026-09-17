# ABox Linux Support — libkrun/KVM Host Port

## Context

ABox runs only on Apple Silicon today. `cmd/abox-vmm/start_darwin_arm64.go` is
tagged `darwin && arm64`, `start_stub.go` refuses every other platform, and
`internal/runtime/runtime.go:930` calls `unix.Clonefile`, which exists only in
`zsyscall_darwin_{amd64,arm64}.go` — so `internal/runtime` does not compile on
Linux at all. `go build ./cmd/abox` and `make test` fail before reaching any
VMM concern.

Every isolation primitive ABox depends on is, however, *more* native on Linux
than on macOS: libkrun's primary backend is KVM, and Hypervisor.framework is the
newer target. Verification against the real `libkrun 1.19.4` x86_64 package
(Sept 2026) shows **all fifteen libkrun symbols `start_darwin_arm64.go` calls
exist in the Linux build with identical semantics**, including
`krun_add_vsock(ctx, 0)`, whose header documents "Use 0 to add vsock without any
TSI hijacking." The device plan — no NIC, no TSI inet, no host-path virtio-fs,
two raw disks, one vsock port — transfers verbatim.

This is therefore a port of build plumbing and host-side syscalls, **not** of the
isolation design. The agent loop, protocol, brokers, credential resolution,
session layout, and guest image contents are unchanged.

**Goal:** `abox` runs natively on Arch and Fedora (x86_64) with the security
posture preserved, and `make image` builds the guest disk with no Docker, no
root, and no privileged container.

### User decisions (Sept 2026)

- **Distros:** Arch and Fedora supported and tested. Debian/Ubuntu documented as
  a source build with exact commands, explicitly marked untested — neither ships
  prebuilt libkrun/libkrunfw packages.
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

Of the five credential sources, **four already work on Linux unchanged**.
`env`, `vault`, `azure`, and `aws` contain no `GOOS`/darwin/macOS-path
references — a direct consequence of PLAN-CRED.md global decision 2 (stdlib
HTTP or CLI subprocess only, no SDKs, no cgo). Only `keychain`
(internal/credsource/keychain.go:23, hardcoded `/usr/bin/security`) is
macOS-only.

The gap is therefore the *local, no-infrastructure* keystore — exactly ABox's
laptop-local thesis. Standing up Vault to hold one API key is absurd for that
user, yet on Linux today `SavePreferred` (internal/credsource/save.go:41) sees
`KeychainEnabled() == false` and writes every `/provider` and `/mcp` key to
plaintext `credentials.env`. Closing that is in scope.

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
3. **Linux links `-lkrun` only, via pkg-config.** libkrun `dlopen`s
   `libkrunfw.so.5` at runtime on Linux (confirmed: no undefined `krunfw_*`
   symbols in `libkrun.so.1.19.4`), so `-lkrunfw` is omitted. `#cgo pkg-config:
   libkrun` resolves correctly under pacman, dnf, and a `make install` into
   `/usr/local`; macOS keeps its explicit Homebrew paths and `-lkrunfw`.
4. **`cloneFile` becomes platform-split, and gains a Linux fast path.**
   `unix.IoctlFileClone` (FICLONE reflink; btrfs, XFS `reflink=1`, bcachefs) then
   `unix.CopyFileRange` (in-kernel copy, works on ext4) then the existing
   `io.Copy` fallback. Fedora defaults to btrfs, so the 768 MiB per-session
   golden-image clone is near-instant there.
5. **The golden image filename carries its architecture.** An arm64 rootfs booted
   under an x86_64 kernel is an unexplained guest panic; a silent failure mode is
   unacceptable. `config.GuestImageName` (internal/config/config.go:17) becomes a
   function of `runtime.GOARCH`, with the bare `abox-guest.raw` kept as a legacy
   fallback in `ImageDir()`, mirroring the existing `~/Library/Caches` lookup.
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
   Availability = `secret-tool` on PATH plus a reachable session bus. This
   preserves the distinction `SavePreferred` depends on for its fallback.

## Verified findings (Sept 2026)

Checked against `extra/libkrun 1.19.4-1` and `extra/libkrunfw 5.5.0-1`, and
against `golang.org/x/sys@v0.47.0` as pinned in `go.mod`.

| Claim | Evidence |
| --- | --- |
| All 15 libkrun symbols ABox calls exist on Linux x86_64 | `libkrun.h` + `nm -D` on `libkrun.so.1.19.4` |
| `KRUN_FEATURE_BLK` is compiled in | `RegisterBlockDevice` / `OpenBlockDevice` / `AttachBlockDevice` present in the `.so` |
| `-lkrunfw` unnecessary on Linux | libkrun `dlopen`s `libkrunfw.so.5`; no undefined `krunfw_*` |
| vsock is userspace; `/dev/vhost-vsock` unused | No `vhost` / `/dev/vsock` strings |
| KVM is the backend | 3 `/dev/kvm` refs; reads `/sys/module/kvm_*/parameters/nested` |
| `mke2fs -d` builds ext4 unprivileged | Ran as uid 1000, exit 0, no loop mount, no Docker |
| `fakeroot` gives correct `0:0` ownership | `debugfs -R "ls -l"` shows `0 0`, versus `1000 1000` without it |
| `unix.Clonefile` is darwin-only | Defined only in `zsyscall_darwin_{amd64,arm64}.go` |
| Linux reflink helpers available | `IoctlFileClone` ioctl_linux.go:184; `CopyFileRange` zsyscall_linux.go:635 |
| `vault` / `azure` / `aws` are platform-clean | No `GOOS`/darwin/macOS-path references in those three files |
| `azure` CLI fallback is portable | `exec.LookPath("az")`, not a hardcoded macOS path |
| `secret-tool store` takes the secret on stdin | `man secret-tool` STORE; warns a piped newline becomes part of the secret |
| Missing key exits 1 with empty stdout | Probed `secret-tool lookup` directly on the dev host |
| Exit codes cannot separate not-found from unavailable | `man secret-tool` EXIT STATUS: "0 on success, a non-zero failure code otherwise" |
| Secret Service is live on the dev host | `gnome-keyring-daemon --components=pkcs11,secrets` owns `org.freedesktop.secrets` |
| KWallet / KeePassXC use the same API | `org.kde.secretservicecompat` activatable on the session bus |

## Platform surface

Six Go touchpoints, two scripts, the Makefile, and CI. Already portable and
untouched: `protocol/`, all brokers, `internal/agent`, `internal/session`,
`internal/vmmconfig`, and `config.Dir()` (`~/.abox`, with `ABOX_HOME` override —
the `~/Library/...` lookups are legacy read-only fallbacks that no-op on Linux).

| Location | Issue |
| --- | --- |
| `internal/runtime/runtime.go:930` | `unix.Clonefile` — **compile break**, darwin-only |
| `cmd/abox-vmm/start_darwin_arm64.go` | Body portable; only `#cgo` paths are Homebrew-specific |
| `cmd/abox-vmm/start_stub.go:1` | Tag `!darwin \|\| !arm64` swallows Linux |
| `internal/runtime/runtime.go:288` | `DYLD_LIBRARY_PATH` in the fixed `cmd.Env` |
| `internal/credsource/keychain.go:23,43` | Hardcoded `/usr/bin/security` — the only credential-source gap on Linux |
| `cmd/abox/creds.go:35` | macOS-only error copy |
| `images/build-guest.sh`, `images/update-guest-bin.sh` | Hardcoded `-linux-arm64`; `--privileged` loop mount |
| `Makefile` | `codesign` unconditional; guest pinned to `GOARCH=arm64` |
| `.github/workflows/test.yml` | `macos-latest` only |

---

## Phase 0 — Spike: boot a VM before changing anything

Throwaway code. Nothing downstream matters if this fails.

```bash
sudo pacman -S libkrun libkrunfw fakeroot      # Arch
sudo dnf install libkrun libkrunfw fakeroot    # Fedora
```

1. Copy `start_darwin_arm64.go` to `start_linux.go`, tag `//go:build linux`,
   replace the preamble with `#cgo pkg-config: libkrun`.
2. Temporarily reduce `cloneFile` to its `io.Copy` path so `internal/runtime`
   compiles. Phase 1 does this properly.
3. Build an amd64 guest, hand-pack a rootfs with the Phase 3 recipe, run
   `abox --probe-vm`.

**Exit criterion:** `--probe-vm` prints the guest file listing.

Watch for: `krun_has_feature(KRUN_FEATURE_BLK) == 1`; `krun_add_vsock_port`
connecting to the host's listening Unix socket in the same direction as macOS;
`krun_set_root_disk_remount` behaving identically.

---

## Phase 1 — Compile on Linux, and a portable libkrun binding

**1a. Fix the `cloneFile` compile break** (internal/runtime/runtime.go:928).
Split by platform, keeping the existing `io.Copy` tail shared:

- `internal/runtime/clone_darwin.go` — `unix.Clonefile` fast path
- `internal/runtime/clone_linux.go` — `unix.IoctlFileClone`, then
  `unix.CopyFileRange`, then fallback
- `internal/runtime/clone_other.go` — fallback only

`Prepare` (internal/runtime/runtime.go:224) keeps its current signature and
semantics; only the copy mechanism changes.

**1b. Portable cgo binding** in `cmd/abox-vmm/`:

- `cgoflags_darwin_arm64.go` — `//go:build darwin && arm64`; preamble only,
  carrying today's Homebrew include/lib paths, `-lkrun -lkrunfw`, and rpath
- `cgoflags_linux.go` — `//go:build linux && (amd64 || arm64)`; preamble only:
  `#cgo pkg-config: libkrun`
- `start_libkrun.go` — the shared `startVM`, tagged for both platforms
- `start_stub.go` — retagged to exclude both; message rewritten to name the
  actual per-OS requirement rather than asserting macOS

**1c.** Make the fixed `cmd.Env` at internal/runtime/runtime.go:288
platform-conditional; drop `DYLD_LIBRARY_PATH` on Linux, where `/usr/lib` is
already on the default search path.

**1d.** Reword cmd/abox/creds.go:35 so the message names the platform's actual
keystore situation instead of asserting macOS.

---

## Phase 2 — Multi-arch guest and image identity

- `Makefile`: derive `GUEST_ARCH` from `go env GOARCH` (overridable); build
  `bin/abox-guest-linux-$(GUEST_ARCH)`.
- `Makefile`: guard the `vmm` target's `$(MAKE) sign` with
  `$(filter darwin,...)` so `codesign` is a no-op off darwin.
- Arch-tag the golden image per Global decision 5.
- `runtime.Prepare`: refuse an architecture mismatch with an explicit error
  rather than booting into a kernel panic. This is the natural insertion point
  for the existing roadmap item *"Image manifest + SHA-256 verify"* — the
  manifest should carry the architecture.

---

## Phase 3 — Rootless native image builder (Linux)

Replaces Docker, `--privileged`, and `mount -o loop` entirely on Linux. Verified
working. `images/build-guest.sh` selects on `uname`; the macOS path is unchanged,
because nothing else can build a Linux ARM64 tree there.

```sh
fakeroot sh -c '
  apk.static --root "$ROOTFS" --initdb --keys-dir "$KEYS" add alpine-base git patch
  install -Dm0755 bin/abox-guest-linux-$ARCH "$ROOTFS/usr/local/bin/abox-guest"
  mkdir -p "$ROOTFS/work/repo" "$ROOTFS/abox-config" "$ROOTFS/tmp"
  printf "nameserver 1.1.1.1\nnameserver 8.8.8.8\noptions ndots:1\n" > "$ROOTFS/etc/resolv.conf"
  mke2fs -q -F -t ext4 -d "$ROOTFS" -b 4096 "$OUT" 768M
'
```

`apk.static` is a static binary that runs on any distribution; fetch it from the
Alpine CDN pinned by SHA-256. `fakeroot` is required — without it `mke2fs -d`
stamps the invoking uid, producing a rootfs owned by `1000:1000`. Rootfs contents
must stay byte-identical in spirit to the Docker path: alpine-base, git, patch,
`/usr/local/bin/abox-guest`, `/work/repo`, `/abox-config`, resolv.conf.

`images/update-guest-bin.sh`: on Linux, replace the binary in place with
`debugfs -w -R "rm /usr/local/bin/abox-guest"` followed by `-R "write ..."`
(unprivileged), or simply rebuild — it is fast.

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

**4c. Config surface.** `credentialSources` (internal/config/config.go:304) gains
`keystore` and `secretservice`; `CredentialRef.validate` (config.go:312)
normalizes all three spellings to one canonical source. `NewResolver`
(internal/credsource/credsource.go) registers the OS keystore under each accepted
alias so `source: keychain` keeps resolving on both platforms.

**4d. Fallback warning.** `SaveResult.Note` already carries a human-readable
string (save.go:34,40,46). Add an explicit warning when falling back to plaintext
so a Linux user learns the key did not reach a keystore, surfaced in the TUI and
by `abox creds migrate`.

**4e. Migration.** `abox creds migrate` needs no logic change once the keystore
dispatches; reword the macOS-only error at cmd/abox/creds.go:35.

**4f. Wording.** save.go:40 hardcodes "key saved to macOS keychain (service
abox)". Make it platform-accurate, along with `credStatusLabel`
(internal/tui/tui.go:154) and the `/provider`, `/credential`, `/mcp` copy.

---

## Phase 5 — Preflight and diagnostics

Expected to be the dominant support burden. Fail with actionable text, never a
raw libkrun return code. In `startVM` (Linux) or `runtime.Start`:

- `/dev/kvm` missing — load `kvm_intel`/`kvm_amd`, or enable virtualization in
  firmware.
- `/dev/kvm` `EACCES` — `sudo usermod -aG kvm $USER`, then log out and back in.
  Arch ships a udev rule granting 0666; Fedora and Debian use `root:kvm 0660`,
  so this will be the most common first-run failure.
- Host is itself a VM without nested virt — surface `krun_check_nested_virt()`.
- `libkrunfw.so.5` not found — name the package for the detected distribution.

---

## Phase 6 — CI and release

- `.github/workflows/test.yml`: matrix `[macos-latest, ubuntu-latest]`. GitHub
  runners have no `/dev/kvm`, so the Linux job stays unit tests, `go vet`, and
  compile — the same limitation macOS already has. Either install libkrun from
  source for the cgo build or skip the `abox-vmm` build on that job.
- Matrix the guest cross-build over `amd64` and `arm64`.
- `release.yml`: add Linux artifacts.

---

## Phase 7 — Documentation and claim hygiene

- README: per-platform prerequisites; Linux quickstart; state that `make image`
  on Linux needs no Docker, no root, and no privileged container.
- Per-distro install: Arch (`pacman`), Fedora (`dnf`), Debian/Ubuntu source build
  (`apt install python3-pyelftools build-essential flex bison libelf-dev`, then
  `make && sudo make install`; libkrunfw compiles a Linux kernel, so it is slow),
  marked **untested**.
- `docs/troubleshooting.md`: `/dev/kvm` permissions, missing libkrunfw,
  architecture mismatch, nested virtualization, and no Secret Service provider.
- `docs/credentials.md` and the README credentials table: rename the row to
  `keystore`, note it resolves to macOS Keychain or Secret Service per platform,
  and record `keychain`/`secretservice` as accepted aliases. State plainly that
  `vault`, `azure`, and `aws` are cross-platform and are the supported headless
  answer on Linux.
- **`PLAN.md` §22**: the acceptance matrix is written against
  Hypervisor.framework. Give it a per-platform column so KVM enforcement is
  tracked separately, per Global decision 7.

---

## Prerequisites (Linux hosts)

**Runtime:** Intel VT-x or AMD-V enabled in firmware; `kvm_intel`/`kvm_amd`
loaded so `/dev/kvm` exists; read/write access to `/dev/kvm`; libkrun and
libkrunfw on the library path. Nested virtualization only if the host is itself a
VM. Default budget is 1 vCPU / 768 MiB per session
(`vmmconfig.DefaultVCPU`, `DefaultRAMMiB`).

**Credentials (optional):** libsecret (`secret-tool`) plus a Secret Service
provider — GNOME Keyring, KWallet, or KeePassXC — for the `keystore` source.
Without one, ABox falls back to `credentials.env` at 0600 with a warning, or use
`vault`/`azure`/`aws`, which need nothing platform-specific.

**Build:** Go 1.25+, gcc, pkg-config, libkrun headers. Image builds additionally
need e2fsprogs ≥ 1.43 (for `mke2fs -d`) and fakeroot. No codesign, no Docker.

Note: Arch's libkrun pulls `libvirglrenderer` and `libpipewire` because GPU and
sound are compiled in. ABox uses neither; this is dependency weight only.

## Risks / notes

1. Architecture-mismatched image produces an opaque guest panic. Mitigated by
   arch-tagged filenames plus the `Prepare` check (Phase 2). Highest-severity
   item in this plan.
2. `/dev/kvm` permissions differ across distributions. Mitigated by Phase 4
   preflight.
3. Host running inside a VM without nested virtualization. Surfaced via
   `krun_check_nested_virt()`.
4. On ext4 there is no reflink, so each session clone is a real 768 MiB copy.
   `CopyFileRange` keeps it in-kernel; btrfs and XFS get true reflink.
5. libkrun version skew across distributions. `krun_has_feature` already guards
   at runtime; add a version gate if a symbol gap appears.
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

## Verification

1. **Compile and unit:** `make test` and `go build ./cmd/abox ./cmd/abox-vmm`
   succeed on Linux. They currently do **not**, because of `unix.Clonefile`; this
   is the first regression to clear.
2. **VM liveness:** `abox --probe-vm` lists guest files.
3. **Isolation spot-checks** (not the full §22 suite, which requires the hardware
   matrix):
   - `abox exec --prompt "run: ip addr"` shows loopback only — no NIC.
   - A host canary file outside the snapshot directory is unreachable from the
     guest.
   - `~/.abox/sessions/<id>/` contains only `root.raw` and `config.raw`;
     `config.raw` is mode 0400 and carries no credential material.
4. **Agent loop:** set a provider key via `/provider`, run a prompt requiring
   `read_file` and `run_command`; confirm the approval prompt appears and
   defaults to deny.
5. **Resume:** `abox --resume <id>` reloads transcript and conversation.
6. **Image builder:** `make image` completes as a non-root user with the Docker
   daemon stopped; `debugfs -R "ls -l /usr/local/bin" <img>` reports `0 0`.
7. **Credential round-trip:** `/provider` stores a key; `secret-tool lookup
   service abox account ANTHROPIC_API_KEY` returns it **byte-identical** (no
   trailing newline); `~/.abox/credentials.env` does not contain it; a turn
   authenticates against the provider.
8. **Fallback path:** with `DBUS_SESSION_BUS_ADDRESS` unset, the same flow warns,
   writes to `credentials.env` at 0600, and still completes a turn.
9. **Alias compatibility:** existing `source: keychain` config resolves on Linux;
   `keystore` and `secretservice` resolve identically; an unknown source is still
   rejected by `config.Validate`.
10. **Cloud sources unaffected:** a model pinned to `source: vault` and another to
   `source: keystore` both resolve in the same session.
11. **Cross-platform regression:** the same suite still passes on Apple Silicon,
   including `security(1)` keychain storage.

