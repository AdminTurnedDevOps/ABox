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

## `guest image missing ... (run: make image)`

The default image is architecture-specific:

```bash
make image
ls -l ~/.abox/images/abox-guest-linux-$(go env GOARCH).raw
```

The path is a 768 MiB image (or, on Linux, an architecture-specific symlink to
an immutable generation). Its resolved regular file must have an adjacent
`.manifest.json`. macOS image creation needs Docker. Linux image creation is
rootless and needs `fakeroot`, e2fsprogs 1.43+, an HTTPS `curl` or `wget`,
`sha256sum`, `tar`, `od`, `awk`, `flock`, and at least 896 MiB free; it needs no
Docker daemon, root, loop mount, KVM, or privileged container.

## Image architecture, protocol, manifest, or digest mismatch

Do not rename an arm64 image to look like amd64, or vice versa. Rebuild for the
host architecture:

```bash
make guest GUEST_ARCH=$(go env GOARCH)
make image GUEST_ARCH=$(go env GOARCH)
```

The image and manifest must agree on `arch`, protocol, and SHA-256. A custom
`runtime.image` or SDK `Options.Image` requires `<image>.manifest.json` next to
the resolved image. Linux intentionally refuses metadata-free legacy images
and sessions. Start a new session after rebuilding; `Resume` never replaces an
old session's disk.

## `abox-vmm not found; build with make build`

The SDK looks on `PATH`, then next to the current executable.

```bash
export PATH="/path/to/ABox/bin:$PATH"
# or
abox.Open(ctx, abox.Options{VMMPath: "/path/to/bin/abox-vmm"})
```

On Apple Silicon, `abox-vmm` must be codesigned (`make vmm` / `make sign`).
Unsigned helpers fail Hypervisor.framework.

## macOS `start vm` / libkrun errors

- `kern.hv_support` must be `1`
- `brew install libkrun libkrunfw`
- Ad-hoc sign: `codesign --entitlements assets/entitlements.plist --force -s - bin/abox-vmm`

## Linux support boundary

Linux VMM execution and Linux/KVM isolation remain **Planned** pending separate
Phase 0.5 and Phase 18 hardware evidence on the pinned Arch and Fedora x86_64
baselines. Build/link/image success or an ad hoc boot is not support evidence.

WSL2 and containerized VMM execution are unsupported even if `/dev/kvm` is
visible. Containers are valid compile and rootless image-build environments
only. A conventional VM needs explicitly enabled nested KVM and is not covered
until the same hardware suite passes there. See
[Platforms]({{ '/platforms' | relative_url }}).

## Linux `/dev/kvm` missing or denied

For a native-host development probe:

```bash
test -e /dev/kvm && ls -l /dev/kvm
test -r /dev/kvm && test -w /dev/kvm
lsmod | grep '^kvm'
```

If the device is absent, enable VT-x/AMD-V in firmware and load `kvm_intel` or
`kvm_amd`. If it is `root:kvm` mode 0660, add the user to the `kvm` group, then
log out and back in so the new group applies:

```bash
sudo usermod -aG kvm "$USER"
```

Do not change the device to world-writable as a workaround. If `/dev/kvm`
opens but VM creation or the KVM API check fails, inspect the kernel log and
host virtualization policy. On a VM, ask the administrator to expose nested
virtualization; this is different from offering nested virtualization to the
ABox guest.

## Linux libkrun pkg-config, loader, or API failure

The build supports libkrun 1.19.x. Verify the selected package metadata and
dynamic dependencies:

```bash
pkg-config --modversion libkrun
pkg-config --cflags --libs libkrun
ldd bin/abox-vmm
ldconfig -p | grep -E 'libkrun\.so|libkrunfw\.so'
```

Pinned pairs are Arch `libkrun 1.19.4-1` with `libkrunfw 5.5.0-1`, and Fedora
44 `libkrun/libkrun-devel 1.19.0-1.fc44` with `libkrunfw 5.5.0-1.fc44`.
Linux links `libkrun`; that library loads its matching libkrunfw SONAME at
runtime. Therefore, `ldd` on `abox-vmm` alone may not reveal missing firmware.

For a source install, make both paths visible, then refresh the loader cache:

```bash
export PKG_CONFIG_PATH=/usr/local/lib64/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}
printf '%s\n' /usr/local/lib64 | sudo tee /etc/ld.so.conf.d/libkrun.conf
sudo ldconfig
```

An `undefined symbol` such as `krun_disable_implicit_vsock` means the headers
and loaded library are incompatible, not that the guest boot timed out. A
negative `krun_*` result can also mean the installed libkrun lacks block-device
support; ABox requires `KRUN_FEATURE_BLK`. Reinstall one complete pinned pair
instead of mixing packages or accepting an arbitrary newer ABI.

## Fedora SELinux denial

Correct Unix permissions do not rule out SELinux. Keep SELinux enforcing and
inspect recent AVCs instead of disabling it:

```bash
getenforce
sudo ausearch -m AVC,USER_AVC -ts recent -c abox-vmm
sudo journalctl -k -b | grep -i 'avc.*denied'
```

Correlate the denied operation and path with `/dev/kvm`, executable mappings,
the loader, or `root.raw`. Do not run `setenforce 0` and do not install a broad
allow policy. A narrow packaged policy should be considered only if the pinned
Fedora hardware acceptance run demonstrates that it is required.

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
`~/.abox/credentials.env` (mode `0600`). OS-keystore entries use service
`abox`. Empty env vars are not used. `abox creds migrate` moves the file into
macOS Keychain or Linux Secret Service.

`abox --probe-vm` does not need a key. A missing key fails the turn, not
VM boot.

## Linux Secret Service unavailable, locked, or timed out

Linux `keystore` needs `secret-tool`, `gdbus`, a session D-Bus, and a provider
such as GNOME Keyring, KWallet Secret Service compatibility, or KeePassXC.
Check provider ownership without storing a secret:

```bash
command -v secret-tool
command -v gdbus
gdbus call --session --dest org.freedesktop.DBus \
  --object-path /org/freedesktop/DBus \
  --method org.freedesktop.DBus.NameHasOwner org.freedesktop.secrets
```

A missing provider/session bus, provider loss, locked collection, or blocked
unlock prompt causes a visible warning and a plaintext fallback to
`~/.abox/credentials.env` at mode 0600. It does not silently claim the key was
stored in Secret Service. On a headless host, configure `vault`, `azure`, or
`aws` if that fallback is unacceptable.

Linux MCP OAuth uses `xdg-open`. If no graphical launcher exists or launch
fails, ABox prints the validated authorization URL and continues waiting for
the browser callback.

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

## Boot hangs or helper exits before the deadline

Default `BootTimeout` is 45s. First boot after `make image` can be slower.
Raise `Options.BootTimeout`. Check `sessions/<id>/console.log` and the helper
stderr. On Linux, check KVM access, the loader, libkrunfw runtime discovery,
API compatibility, and SELinux before treating this as a generic timeout.

## macOS Docker packer 404 / daemon down

The session path does not need Docker. On macOS only, `make image` /
`make image-update` uses Docker. Start Docker Desktop to refresh the golden
disk. On Linux, install the native image dependencies instead; do not start a
privileged container.

## ABox reports "not a git worktree"

Start ABox from any directory inside the Git worktree you want to use. ABox
discovers the repository root automatically. Clean worktrees use committed
`HEAD`; dirty or commitless worktrees use tracked files plus non-ignored
untracked files. Ordinary non-Git directories are not accepted.

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
