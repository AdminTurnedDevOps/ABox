#!/bin/sh
set -eu
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
IMG="${ABOX_IMAGE:-$HOME/.abox/images/abox-guest.raw}"
BIN="${ROOT}/bin/abox-guest-linux-arm64"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT
cp "$BIN" "$WORKDIR/abox-guest"
docker run --rm --privileged \
  -v "$WORKDIR:/in:ro" \
  -v "$(dirname "$IMG"):/out" \
  alpine:3.21 \
  sh -c 'apk add --no-cache e2fsprogs >/dev/null && mkdir -p /mnt && mount -o loop /out/abox-guest.raw /mnt && cp /in/abox-guest /mnt/usr/local/bin/abox-guest && chmod 0755 /mnt/usr/local/bin/abox-guest && umount /mnt'
echo "updated $IMG"
