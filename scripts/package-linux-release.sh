#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)

if [ "$#" -ne 3 ]; then
  echo "usage: $0 <tag> <image-pointer> <output-directory>" >&2
  exit 2
fi
TAG=$1
IMAGE_POINTER=$2
OUT=$3

case "$TAG" in
  v[0-9]*.[0-9]*.[0-9]*) ;;
  *) echo "release tag must look like v1.2.3" >&2; exit 1 ;;
esac

for tool in sha256sum tar zstd readlink; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "release packaging requires $tool" >&2
    exit 1
  }
done
for file in "$ROOT/bin/abox" "$ROOT/bin/abox-vmm" "$ROOT/bin/abox-guest-linux-amd64"; do
  if [ ! -x "$file" ]; then
    echo "release binary missing or not executable: $file" >&2
    exit 1
  fi
done

IMAGE=$(readlink -f "$IMAGE_POINTER")
MANIFEST="$IMAGE.manifest.json"
if [ ! -f "$IMAGE" ] || [ ! -f "$MANIFEST" ]; then
  echo "release image or adjacent manifest is missing: $IMAGE" >&2
  exit 1
fi

mkdir -p "$OUT"
STAGE=$(mktemp -d "${TMPDIR:-/tmp}/abox-linux-package.XXXXXX")
trap 'rm -rf "$STAGE"' EXIT HUP INT TERM
NAME="abox_${TAG}_linux_amd64"
PAYLOAD="$STAGE/$NAME"
mkdir "$PAYLOAD"

install -m 0755 "$ROOT/bin/abox" "$PAYLOAD/abox"
install -m 0755 "$ROOT/bin/abox-vmm" "$PAYLOAD/abox-vmm"
install -m 0755 "$ROOT/bin/abox-guest-linux-amd64" "$PAYLOAD/abox-guest-linux-amd64"
install -m 0444 "$MANIFEST" "$PAYLOAD/abox-guest-linux-amd64.raw.manifest.json"
zstd -q -19 -T0 "$IMAGE" -o "$PAYLOAD/abox-guest-linux-amd64.raw.zst"
cat > "$PAYLOAD/INSTALL" <<'EOF'
Install abox and abox-vmm on PATH. Decompress abox-guest-linux-amd64.raw.zst
to ~/.abox/images/abox-guest-linux-amd64.raw and place its adjacent manifest at
~/.abox/images/abox-guest-linux-amd64.raw.manifest.json. Install the exact
supported distro libkrun/libkrunfw package pair documented in docs/platforms.md.
Verify SHA256SUMS before installation.
EOF

(
  cd "$PAYLOAD"
  sha256sum INSTALL abox abox-vmm abox-guest-linux-amd64 \
    abox-guest-linux-amd64.raw.manifest.json abox-guest-linux-amd64.raw.zst > SHA256SUMS
)

SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-0}
ARCHIVE="$OUT/$NAME.tar.gz"
tar --sort=name --mtime="@$SOURCE_DATE_EPOCH" --owner=0 --group=0 --numeric-owner \
  -C "$STAGE" -czf "$ARCHIVE" "$NAME"
(
  cd "$OUT"
  sha256sum "$(basename "$ARCHIVE")" > "$(basename "$ARCHIVE").sha256"
)

echo "$ARCHIVE"
