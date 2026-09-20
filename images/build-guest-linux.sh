#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ARCH="${ABOX_GUEST_ARCH:-}"
ALPINE_RELEASE="v3.21"
APK_TOOLS_VERSION="2.14.6-r3"
ALPINE_KEYS_VERSION="2.5-r0"
# Keep the complete dependency closure constrained. A repository update may
# make this build fail, but it cannot silently change a release image.
ALPINE_PACKAGES="
alpine-base=3.21.8-r0
alpine-baselayout=3.6.8-r1
alpine-baselayout-data=3.6.8-r1
alpine-conf=3.19.2-r0
alpine-keys=2.5-r0
alpine-release=3.21.8-r0
apk-tools=2.14.6-r3
brotli-libs=1.1.0-r2
busybox=1.37.0-r14
busybox-binsh=1.37.0-r14
busybox-mdev-openrc=1.37.0-r14
busybox-openrc=1.37.0-r14
busybox-suid=1.37.0-r14
c-ares=1.34.8-r0
ca-certificates-bundle=20260909-r0
git=2.47.3-r0
git-init-template=2.47.3-r0
ifupdown-ng=0.12.1-r6
libcap2=2.78-r0
libcrypto3=3.3.7-r1
libcurl=8.14.1-r2
libexpat=2.8.4-r0
libidn2=2.3.7-r0
libpsl=0.21.5-r3
libssl3=3.3.7-r1
libunistring=1.2-r0
mdev-conf=4.7-r0
musl=1.2.5-r11
musl-utils=1.2.5-r11
nghttp2-libs=1.69.0-r0
openrc=0.55.1-r2
patch=2.7.6-r10
pcre2=10.43-r0
scanelf=1.3.8-r1
ssl_client=1.37.0-r14
zlib=1.3.2-r0
zstd-libs=1.5.6-r2
"
PROTOCOL_VERSION=4
IMAGE_ID="${ABOX_IMAGE_ID:-abox-guest-dev}"

if [ -z "$ARCH" ]; then
  case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) echo "unsupported host architecture: $(uname -m)" >&2; exit 1 ;;
  esac
fi

case "$ARCH" in
  amd64) APK_ARCH=x86_64; ELF_MACHINE=3e00 ;;
  arm64) APK_ARCH=aarch64; ELF_MACHINE=b700 ;;
  *) echo "unsupported guest architecture: $ARCH" >&2; exit 1 ;;
esac

OUT="${ABOX_IMAGE:-$HOME/.abox/images/abox-guest-linux-$ARCH.raw}"

case "$(uname -m)" in
  x86_64|amd64)
    HOST_APK_ARCH=x86_64
    APK_TOOLS_SHA256=f0e0d34d6a8f1f9d8704bae6612b4627b96f13cd20db759e9b43085135cd234f
    ALPINE_KEYS_SHA256=f68a8cf46058b77d2ebccc33fec2a645d8da9e3791ffc385cf80403ec4810512
    ;;
  aarch64|arm64)
    HOST_APK_ARCH=aarch64
    APK_TOOLS_SHA256=910015ebcdb11f92966f5590cf5b538b3e4dddbc1b56bc441fae3a622d05dd0e
    ALPINE_KEYS_SHA256=a70d3c55ee676d7d670714aa729285d5ab6fcf18146eb03e05319098bcb715c0
    ;;
  *) echo "apk.static is not pinned for host architecture $(uname -m)" >&2; exit 1 ;;
esac

GUEST_BIN="$ROOT/bin/abox-guest-linux-$ARCH"
if [ ! -x "$GUEST_BIN" ]; then
  echo "missing $GUEST_BIN; run: make guest GUEST_ARCH=$ARCH" >&2
  exit 1
fi

for tool in fakeroot mke2fs e2fsck debugfs sha256sum tar od awk flock; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "Linux image builds require $tool" >&2
    exit 1
  fi
done
if command -v curl >/dev/null 2>&1; then
  DOWNLOADER=curl
elif command -v wget >/dev/null 2>&1; then
  DOWNLOADER=wget
else
  echo "Linux image builds require curl or wget with HTTPS support" >&2
  exit 1
fi

elf_machine() {
  set -- $(od -An -tx1 -j18 -N2 "$1")
  printf '%s%s\n' "${1:-}" "${2:-}"
}

verify_elf() {
  actual="$(elf_machine "$1")"
  if [ "$actual" != "$ELF_MACHINE" ]; then
    echo "$1 has ELF machine $actual; expected $ARCH ($ELF_MACHINE)" >&2
    exit 1
  fi
}

download() {
  url=$1
  destination=$2
  if [ "$DOWNLOADER" = curl ]; then
    curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 \
      --output "$destination" "$url"
  else
    wget -q --https-only -O "$destination" "$url"
  fi
}

verify_download() {
  expected=$1
  file=$2
  printf '%s  %s\n' "$expected" "$file" | sha256sum -c - >/dev/null
}

verify_elf "$GUEST_BIN"

OUT_DIR="$(dirname "$OUT")"
mkdir -p "$OUT_DIR"
AVAILABLE_KB="$(df -Pk "$OUT_DIR" | awk 'NR == 2 { print $4 }')"
REQUIRED_KB=917504
if [ -z "$AVAILABLE_KB" ] || [ "$AVAILABLE_KB" -lt "$REQUIRED_KB" ]; then
  echo "at least 896 MiB free is required in $OUT_DIR" >&2
  exit 1
fi

WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/abox-image.XXXXXX")"
BUILD_DIR=""
UNPUBLISHED_GENERATION=""
POINTER_TMP=""
cleanup() {
  chmod -R u+w "$WORKDIR" 2>/dev/null || true
  rm -rf "$WORKDIR"
  if [ -n "$POINTER_TMP" ]; then
    rm -f "$POINTER_TMP"
  fi
  if [ -n "$BUILD_DIR" ] && [ -d "$BUILD_DIR" ]; then
    chmod -R u+w "$BUILD_DIR" 2>/dev/null || true
    rm -rf "$BUILD_DIR"
  fi
  if [ -n "$UNPUBLISHED_GENERATION" ] && [ -d "$UNPUBLISHED_GENERATION" ]; then
    chmod -R u+w "$UNPUBLISHED_GENERATION" 2>/dev/null || true
    rm -rf "$UNPUBLISHED_GENERATION"
  fi
}
trap cleanup EXIT HUP INT TERM

TOOLS="$WORKDIR/tools"
ROOTFS="$WORKDIR/rootfs"
KEYS="$WORKDIR/keys"
mkdir -p "$TOOLS" "$ROOTFS" "$KEYS"

REPO_BASE="https://dl-cdn.alpinelinux.org/alpine/$ALPINE_RELEASE"
APK_PACKAGE="$WORKDIR/apk-tools-static.apk"
KEYS_PACKAGE="$WORKDIR/alpine-keys.apk"
download "$REPO_BASE/main/$HOST_APK_ARCH/apk-tools-static-$APK_TOOLS_VERSION.apk" "$APK_PACKAGE"
download "$REPO_BASE/main/$HOST_APK_ARCH/alpine-keys-$ALPINE_KEYS_VERSION.apk" "$KEYS_PACKAGE"
verify_download "$APK_TOOLS_SHA256" "$APK_PACKAGE"
verify_download "$ALPINE_KEYS_SHA256" "$KEYS_PACKAGE"

tar --warning=no-unknown-keyword -xzf "$APK_PACKAGE" -C "$TOOLS" sbin/apk.static
tar --warning=no-unknown-keyword -xzf "$KEYS_PACKAGE" -C "$TOOLS" etc/apk/keys usr/share/apk/keys
cp "$TOOLS"/etc/apk/keys/*.pub "$KEYS/"
cp "$TOOLS"/usr/share/apk/keys/"$APK_ARCH"/*.pub "$KEYS/"
APK="$TOOLS/sbin/apk.static"
chmod 0755 "$APK"

# Keep one architecture-specific atomic pointer and architecture-tag every
# immutable generation. Manifest readers resolve this symlink once and append
# .manifest.json to the immutable target.
OUT_BASE="$(basename "$OUT")"
STORE="$OUT_DIR/.$OUT_BASE.generations/$ARCH"
mkdir -p "$STORE"
BUILD_DIR="$(mktemp -d "$STORE/.build.XXXXXX")"
IMAGE="$BUILD_DIR/abox-guest-$ARCH.raw"
MANIFEST="$IMAGE.manifest.json"
APK_LOG="$WORKDIR/apk-install.log"

export APK ROOTFS KEYS APK_ARCH GUEST_BIN IMAGE REPO_BASE APK_LOG ALPINE_PACKAGES
fakeroot sh -c '
  set -eu
  chown 0:0 "$ROOTFS"
  chmod 0755 "$ROOTFS"
  if ! "$APK" --arch "$APK_ARCH" --root "$ROOTFS" --initdb --no-cache \
      --keys-dir "$KEYS" \
      --repository "$REPO_BASE/main" \
      --repository "$REPO_BASE/community" \
      add --no-scripts --no-chown $ALPINE_PACKAGES >"$APK_LOG" 2>&1; then
    cat "$APK_LOG" >&2
    exit 1
  fi
  # apk-tools 2.x calls fchownat for package directories even with --no-chown;
  # fakeroot does not interpose that call on every distro. Ignore only that
  # summary: the recursive fake chown and debugfs checks below are authoritative.
  grep -Ev "^ERROR: [0-9]+ errors updating directory permissions$" "$APK_LOG" || true
  chown -R 0:0 "$ROOTFS"
  mkdir -p "$ROOTFS/etc/apk" "$ROOTFS/usr/local/bin" \
    "$ROOTFS/work/repo" "$ROOTFS/tmp" "$ROOTFS/abox-config" "$ROOTFS/home/abox"
  printf "abox:x:1000:1000:ABox guest:/home/abox:/bin/sh\n" >> "$ROOTFS/etc/passwd"
  printf "abox:x:1000:\n" >> "$ROOTFS/etc/group"
  printf "%s\n%s\n" "$REPO_BASE/main" "$REPO_BASE/community" > "$ROOTFS/etc/apk/repositories"
  install -o 0 -g 0 -m 0755 "$GUEST_BIN" "$ROOTFS/usr/local/bin/abox-guest"
  printf "nameserver 1.1.1.1\nnameserver 8.8.8.8\noptions ndots:1\n" > "$ROOTFS/etc/resolv.conf"
  chown 0:0 "$ROOTFS/etc/apk/repositories" "$ROOTFS/etc/resolv.conf" "$ROOTFS/abox-config"
  chown 1000:1000 "$ROOTFS/work/repo" "$ROOTFS/tmp" "$ROOTFS/home/abox"
  chmod 0644 "$ROOTFS/etc/apk/repositories" "$ROOTFS/etc/resolv.conf"
  chmod 0755 "$ROOTFS/work/repo" "$ROOTFS/tmp" "$ROOTFS/abox-config" "$ROOTFS/home/abox"
  # Model-authored commands run as UID 1000 and must not regain guest root.
  find "$ROOTFS" -xdev -perm /6000 -exec chmod a-s {} +
  # Some package payloads intentionally have no owner-read bit. fakeroot
  # records their guest metadata, while this real chmod lets unprivileged
  # mke2fs read the payload without changing the metadata mke2fs observes.
  env -u LD_PRELOAD -u LD_LIBRARY_PATH chmod -R u+rwX "$ROOTFS"
  mke2fs -q -F -t ext4 -d "$ROOTFS" -b 4096 "$IMAGE" 768M
'

e2fsck -fn "$IMAGE"
ROOT_STAT="$(debugfs -R 'stat /' "$IMAGE" 2>&1)"
GUEST_STAT="$(debugfs -R 'stat /usr/local/bin/abox-guest' "$IMAGE" 2>&1)"
REPO_STAT="$(debugfs -R 'stat /work/repo' "$IMAGE" 2>&1)"
BUSYBOX_STAT="$(debugfs -R 'stat /bin/busybox' "$IMAGE" 2>&1)"
if ! printf '%s\n' "$ROOT_STAT" | grep -Eq 'User:[[:space:]]+0[[:space:]]+Group:[[:space:]]+0'; then
  echo "image root is not owned by 0:0" >&2
  exit 1
fi
if ! printf '%s\n' "$ROOT_STAT" | grep -Eq 'Mode:[[:space:]]+0755'; then
  echo "image root mode is not 0755" >&2
  exit 1
fi
if ! printf '%s\n' "$GUEST_STAT" | grep -Eq 'User:[[:space:]]+0[[:space:]]+Group:[[:space:]]+0'; then
  echo "guest binary is not owned by 0:0" >&2
  exit 1
fi
if ! printf '%s\n' "$GUEST_STAT" | grep -Eq 'Mode:[[:space:]]+0755'; then
  echo "guest binary mode is not 0755" >&2
  exit 1
fi
if ! printf '%s\n' "$REPO_STAT" | grep -Eq 'User:[[:space:]]+1000[[:space:]]+Group:[[:space:]]+1000'; then
  echo "guest repository is not owned by 1000:1000" >&2
  exit 1
fi
if ! printf '%s\n' "$BUSYBOX_STAT" | grep -Eq 'Mode:[[:space:]]+0755'; then
  echo "busybox retained elevated mode bits" >&2
  exit 1
fi
EXTRACTED_GUEST="$WORKDIR/abox-guest"
debugfs -R "dump /usr/local/bin/abox-guest $EXTRACTED_GUEST" "$IMAGE" >/dev/null 2>&1
verify_elf "$EXTRACTED_GUEST"

IMAGE_SHA256="$(sha256sum "$IMAGE")"
IMAGE_SHA256=${IMAGE_SHA256%% *}
cat > "$MANIFEST" <<EOF
{
  "schema": 1,
  "arch": "$ARCH",
  "image_id": "$IMAGE_ID",
  "protocol": $PROTOCOL_VERSION,
  "sha256": "$IMAGE_SHA256"
}
EOF
verify_download "$IMAGE_SHA256" "$IMAGE"
chmod 0444 "$IMAGE" "$MANIFEST"

GENERATION="$STORE/$IMAGE_SHA256"
# Serialize generation adoption, cleanup ownership, and pointer publication.
# Runtime readers take the shared side only for manifest read and clone.
exec 9>"$OUT_DIR/.abox-images.lock"
flock -x 9
if [ -e "$GENERATION" ]; then
  EXISTING_IMAGE="$GENERATION/abox-guest-$ARCH.raw"
  verify_download "$IMAGE_SHA256" "$EXISTING_IMAGE"
  chmod -R u+w "$BUILD_DIR"
  rm -rf "$BUILD_DIR"
else
  mv "$BUILD_DIR" "$GENERATION"
  chmod 0555 "$GENERATION"
  UNPUBLISHED_GENERATION="$GENERATION"
fi
BUILD_DIR=""

if [ "${ABOX_IMAGE_FAIL_BEFORE_PUBLISH:-0}" = 1 ]; then
  echo "injected failure before image publication" >&2
  exit 1
fi

POINTER_TMP="$OUT_DIR/.$OUT_BASE.current.$$"
TARGET=".$OUT_BASE.generations/$ARCH/$IMAGE_SHA256/abox-guest-$ARCH.raw"
rm -f "$POINTER_TMP"
ln -s "$TARGET" "$POINTER_TMP"
# From this point an interrupted publish may leave an unreferenced generation,
# but cleanup must never remove a generation that the pointer could select.
UNPUBLISHED_GENERATION=""
mv -Tf "$POINTER_TMP" "$OUT"
POINTER_TMP=""
flock -u 9

echo "wrote immutable generation $GENERATION"
echo "updated current image pointer $OUT"
