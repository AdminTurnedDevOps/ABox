---
layout: default
title: Platforms
nav_order: 14
permalink: /platforms/
---

# Platforms
{: .no_toc }

1. TOC
{:toc}

## Support status

The macOS/Apple Silicon runtime is implemented and runnable. The Linux host
port, KVM binding, rootless image builder, lifecycle handling, and Secret
Service integration are implemented, but Linux VMM execution and Linux/KVM
isolation remain **Planned** until separate Phase 0.5 and Phase 18 hardware
evidence passes on both pinned x86_64 Linux baselines. A successful build or VM
boot is not that evidence.

| Environment | Build | `make image` | VMM execution |
| --- | --- | --- | --- |
| macOS/arm64 | Implemented | Docker packer | Implemented; isolation evidence still Planned |
| Arch Linux x86_64 baseline | Implemented | Rootless native builder | Planned pending Phase 0.5/18 evidence |
| Fedora 44 x86_64 baseline | Implemented | Rootless native builder | Planned pending Phase 0.5/18 evidence |
| Linux/arm64 | Host and guest builds are parameterized | Rootless native builder | Not a supported runtime/release target yet |
| Debian/Ubuntu | Source build path only, untested | Rootless native builder when dependencies are available | Unsupported |
| Linux container or WSL2 | Compile and image-build use only | Supported as a build job when unprivileged tools work | Unsupported, even when `/dev/kvm` is visible |
| Windows | Unsupported | Unsupported | Unsupported |

Running ABox in a conventional VM also requires explicitly exposed nested KVM
and the same hardware acceptance suite. It is not covered merely because
`/dev/kvm` exists.

## Linux build baselines

The documented, pinned x86_64 package baselines are:

| Distribution | Repository baseline | libkrun | libkrunfw | ABI |
| --- | --- | --- | --- | --- |
| Arch Linux | 2026-09-17 Arch Linux Archive snapshot | `1.19.4-1` | `5.5.0-1` | `libkrun.so.1`, firmware loaded as `libkrunfw.so.5` by this package |
| Fedora | Fedora 44 updates | `libkrun-1.19.0-1.fc44`, `libkrun-devel-1.19.0-1.fc44` | `5.5.0-1.fc44` | `libkrun.so.1`, `libkrunfw.so.5` |

Configure Arch to use the dated archive before installing packages; a moving
mirror is not the pinned baseline.

```bash
# Arch Linux, after selecting the 2026-09-17 archive snapshot
sudo pacman -S --needed base-devel git curl ca-certificates pkgconf \
  libkrun=1.19.4-1 libkrunfw=5.5.0-1 fakeroot e2fsprogs zstd python \
  shadow util-linux binutils file

# Fedora 44
sudo dnf install git curl ca-certificates gcc gcc-c++ make pkgconf-pkg-config \
  fakeroot e2fsprogs zstd python3 shadow-utils \
  util-linux binutils file tar gzip findutils diffutils
sudo sh scripts/install-fedora-libkrun.sh  # verifies exact RPM SHA-256 and signatures
```

Both builds require Go 1.25+ and cgo. Linux links libkrun through
`pkg-config`; the accepted build API range is libkrun 1.19.x. libkrun loads its
matching firmware library at runtime, so a successful `pkg-config` or link
check does not prove that libkrunfw is discoverable.

Arch and Fedora containers are valid build-only environments for compilation,
cgo/pkg-config checks, and rootless image creation. They are not supported VMM
runtimes. Generic Ubuntu CI can test pure Go code and the `CGO_ENABLED=0`
diagnostic VMM stub, but does not establish Linux VMM compatibility.

The release workflow can build a Linux amd64 candidate and is wired to require
distinct protected Arch and Fedora KVM evidence before publication. Workflow
plumbing, an acceptance manifest, or an unexecuted gate is not passing evidence;
no checked-in report currently opens the Linux support gate.
Runner provisioning must pin `ABOX_KVM_ACCEPTANCE_SHA256` to the reviewed
`.github/acceptance/linux-kvm-v1.json` bytes as well as the per-distro runner
identity and root-owned harness digest variables.

Debian and Ubuntu do not provide the pinned package pair used by this project.
Their source-build route is explicitly untested: build checksum-pinned
libkrunfw 5.5.0 first, then libkrun 1.19.4 with block support
(`make BLK=1`), configure `/usr/local/lib64` for the runtime loader and
`/usr/local/lib64/pkgconfig` for pkg-config, and run `ldconfig`. Do not treat a
featureless `make && make install` as an ABox-compatible build.

## Linux image builder

On Linux, `make image` runs as an ordinary user and requires no Docker daemon,
root, loop mount, or privileged container. In addition to the compiler
requirements above, it needs:

- `fakeroot`
- e2fsprogs 1.43 or newer (`mke2fs -d`, `e2fsck`, and `debugfs`)
- `curl` or `wget` with HTTPS
- `sha256sum`, `tar`, `od`, `awk`, and `flock`
- at least 896 MiB free in the image output directory

The builder downloads checksum-pinned `apk.static` and Alpine keys, installs a
pinned package closure, checks ext4 ownership/modes and the guest ELF machine,
and publishes an immutable generation through one atomic current-image symlink.

For optional desktop keystore integration, install `libsecret` (for
`secret-tool`) and GLib (for `gdbus`), then provide GNOME Keyring, KWallet's
Secret Service compatibility service, or KeePassXC on the session bus.

## Images and manifests

Image names include the guest architecture:

```text
~/.abox/images/abox-guest-linux-amd64.raw
~/.abox/images/abox-guest-linux-arm64.raw
```

`make image GUEST_ARCH=amd64` or `make image GUEST_ARCH=arm64` selects the
guest architecture. The default is the host `GOARCH`. Each resolved image has
an adjacent `<image>.manifest.json` containing schema, architecture, image ID,
guest protocol, and SHA-256. A custom `runtime.image` or SDK `Options.Image`
also requires that adjacent manifest.

Development images use image ID `abox-guest-dev`. Release builds set `IMAGE_ID`
to the release tag and inject that same value into the guest binary and manifest;
custom builders must preserve that identity match.

New sessions verify the manifest architecture/protocol and the cloned image
digest before boot. Session metadata records the image identity, guest
architecture/protocol, and backend (`hvf` or `kvm`). Linux refuses old
metadata-free images and sessions. macOS/arm64 alone retains a narrow legacy
fallback for the former `abox-guest.raw` image and old arm64 sessions.

## Linux runtime prerequisites

These are prerequisites for development probes, not a declaration of supported
Linux VMM execution:

- Intel VT-x or AMD-V enabled in firmware
- `kvm_intel` or `kvm_amd` loaded
- read/write access to `/dev/kvm`
- the pinned libkrun/libkrunfw pair on the runtime loader path
- x86_64 host hardware for the initial acceptance target

The Fedora acceptance run must keep SELinux enforcing and record any AVCs; it
must not disable SELinux to make the VMM run.

WSL2 and containerized VMM execution are unsupported. Do not work around that
policy by passing `/dev/kvm`, weakening seccomp, or adding privileges. Use a
native host for hardware evidence.

## Evidence gate

Linux/KVM has its own Phase 0.5 boot/device/network record and Phase 18 security
acceptance record. macOS Hypervisor.framework results cannot be reused for KVM,
and Arch results cannot be reused for Fedora. Until both pinned Linux baselines
pass against the exact release artifacts, Linux runtime/release support and all
Linux isolation claims stay **Planned**.
