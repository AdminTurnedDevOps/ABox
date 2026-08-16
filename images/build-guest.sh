#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${ABOX_IMAGE:-$HOME/Library/Caches/ABox/images/abox-guest.raw}"
GUEST_BIN="${ROOT}/bin/abox-guest-linux-arm64"

if [ ! -x "$GUEST_BIN" ]; then
  echo "missing $GUEST_BIN; run: make guest" >&2
  exit 1
fi
if ! command -v docker >/dev/null; then
  echo "docker is required to pack the ARM64 guest disk" >&2
  exit 1
fi

mkdir -p "$(dirname "$OUT")"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT
cp "$GUEST_BIN" "$WORKDIR/abox-guest"
chmod +x "$WORKDIR/abox-guest"

docker run --rm --privileged \
  -v "$WORKDIR:/in:ro" \
  -v "$(dirname "$OUT"):/out" \
  alpine:3.21 \
  sh -c '
    set -eu
    apk add --no-cache e2fsprogs
    mkdir -p /rootfs/etc/apk
    cp /etc/apk/repositories /rootfs/etc/apk/repositories
    apk add --no-cache --root /rootfs --initdb --keys-dir /etc/apk/keys alpine-base git patch
    mkdir -p /rootfs/usr/local/bin /rootfs/work/repo /rootfs/tmp /rootfs/abox-config
    cp /in/abox-guest /rootfs/usr/local/bin/abox-guest
    chmod 0755 /rootfs/usr/local/bin/abox-guest
    printf 'nameserver 1.1.1.1\nnameserver 8.8.8.8\noptions ndots:1\n' > /rootfs/etc/resolv.conf
    rm -f /out/abox-guest.raw
    dd if=/dev/zero of=/out/abox-guest.raw bs=1M count=768 status=none
    mkfs.ext4 -F -q /out/abox-guest.raw
    mkdir -p /mnt/root
    mount -o loop /out/abox-guest.raw /mnt/root
    tar -C /rootfs -cf - . | tar -C /mnt/root -xf -
    umount /mnt/root
  '

echo "wrote $OUT"
ls -lh "$OUT"
