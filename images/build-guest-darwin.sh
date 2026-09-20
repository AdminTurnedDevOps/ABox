#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ARCH="${ABOX_GUEST_ARCH:-arm64}"
OUT="${ABOX_IMAGE:-$HOME/.abox/images/abox-guest-linux-$ARCH.raw}"
GUEST_BIN="$ROOT/bin/abox-guest-linux-$ARCH"
PROTOCOL_VERSION=4
IMAGE_ID="${ABOX_IMAGE_ID:-abox-guest-dev}"

case "$ARCH" in
  amd64|arm64) ;;
  *) echo "unsupported guest architecture: $ARCH" >&2; exit 1 ;;
esac
if [ ! -x "$GUEST_BIN" ]; then
  echo "missing $GUEST_BIN; run: make guest GUEST_ARCH=$ARCH" >&2
  exit 1
fi
if ! command -v docker >/dev/null 2>&1; then
  echo "Docker is required to pack the guest disk on macOS" >&2
  exit 1
fi

mkdir -p "$(dirname "$OUT")"
WORKDIR="$(mktemp -d)"
POINTER_TMP=""
BUILD_DIR=""
cleanup() {
  rm -rf "$WORKDIR"
  if [ -n "$POINTER_TMP" ]; then rm -f "$POINTER_TMP"; fi
  if [ -n "$BUILD_DIR" ] && [ -d "$BUILD_DIR" ]; then rm -rf "$BUILD_DIR"; fi
}
trap cleanup EXIT HUP INT TERM
cp "$GUEST_BIN" "$WORKDIR/abox-guest"
chmod 0755 "$WORKDIR/abox-guest"

# Keep the existing macOS builder: Docker supplies the target-architecture
# Alpine userspace and the privileged loop mount.
docker run --rm --privileged --platform "linux/$ARCH" \
  -v "$WORKDIR:/work" \
  alpine:3.21 \
  sh -c '
    set -eu
    apk add --no-cache e2fsprogs
    mkdir -p /rootfs/etc/apk
    cp /etc/apk/repositories /rootfs/etc/apk/repositories
    apk add --no-cache --root /rootfs --initdb --keys-dir /etc/apk/keys alpine-base git patch
    mkdir -p /rootfs/usr/local/bin /rootfs/work/repo /rootfs/tmp /rootfs/abox-config /rootfs/home/abox
    printf "abox:x:1000:1000:ABox guest:/home/abox:/bin/sh\n" >> /rootfs/etc/passwd
    printf "abox:x:1000:\n" >> /rootfs/etc/group
    chown 1000:1000 /rootfs/work/repo /rootfs/tmp /rootfs/home/abox
    find /rootfs -xdev -perm /6000 -exec chmod a-s {} +
    cp /work/abox-guest /rootfs/usr/local/bin/abox-guest
    chmod 0755 /rootfs/usr/local/bin/abox-guest
    printf "nameserver 1.1.1.1\nnameserver 8.8.8.8\noptions ndots:1\n" > /rootfs/etc/resolv.conf
    dd if=/dev/zero of=/work/abox-guest.raw bs=1M count=768 status=none
    mkfs.ext4 -F -q /work/abox-guest.raw
    mkdir -p /mnt/root
    mount -o loop /work/abox-guest.raw /mnt/root
    tar -C /rootfs -cf - . | tar -C /mnt/root -xf -
    umount /mnt/root
  '

IMAGE_SHA256="$(shasum -a 256 "$WORKDIR/abox-guest.raw")"
IMAGE_SHA256=${IMAGE_SHA256%% *}
OUT_DIR="$(dirname "$OUT")"
OUT_BASE="$(basename "$OUT")"
STORE="$OUT_DIR/.$OUT_BASE.generations/$ARCH"
mkdir -p "$STORE"
BUILD_DIR="$(mktemp -d "$STORE/.build.XXXXXX")"
IMAGE="$BUILD_DIR/abox-guest-$ARCH.raw"
mv "$WORKDIR/abox-guest.raw" "$IMAGE"
cat > "$IMAGE.manifest.json" <<EOF
{
  "schema": 1,
  "arch": "$ARCH",
  "image_id": "$IMAGE_ID",
  "protocol": $PROTOCOL_VERSION,
  "sha256": "$IMAGE_SHA256"
}
EOF
chmod 0444 "$IMAGE" "$IMAGE.manifest.json"
GENERATION="$STORE/$IMAGE_SHA256"
if [ -d "$GENERATION" ]; then
  printf '%s  %s\n' "$IMAGE_SHA256" "$GENERATION/abox-guest-$ARCH.raw" | shasum -a 256 -c -
  cmp "$IMAGE.manifest.json" "$GENERATION/abox-guest-$ARCH.raw.manifest.json"
  rm -rf "$BUILD_DIR"
else
  mv "$BUILD_DIR" "$GENERATION"
  chmod 0555 "$GENERATION"
fi
BUILD_DIR=""

POINTER_TMP="$OUT_DIR/.$OUT_BASE.current.$$"
TARGET=".$OUT_BASE.generations/$ARCH/$IMAGE_SHA256/abox-guest-$ARCH.raw"
ln -s "$TARGET" "$POINTER_TMP"
mv -f "$POINTER_TMP" "$OUT"
POINTER_TMP=""
echo "updated current image pointer $OUT"
ls -lh "$OUT" "$GENERATION/abox-guest-$ARCH.raw.manifest.json"
