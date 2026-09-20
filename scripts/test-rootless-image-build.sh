#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
ARCH=${1:-amd64}
WORK=$(mktemp -d "${TMPDIR:-/tmp}/abox-rootless-image.XXXXXX")
trap 'chmod -R u+w "$WORK" 2>/dev/null || true; rm -rf "$WORK"' EXIT HUP INT TERM
IMAGE="$WORK/abox-guest-linux-$ARCH.raw"

if [ "$(id -u)" -eq 0 ]; then
  echo "rootless image test must run as an unprivileged user" >&2
  exit 1
fi
if [ -e /dev/kvm ]; then
  echo "build-only image test requires a runner/container without /dev/kvm" >&2
  exit 1
fi
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  echo "build-only image test requires no reachable Docker daemon" >&2
  exit 1
fi

make -C "$ROOT" guest GUEST_ARCH="$ARCH"
make -C "$ROOT" image GUEST_ARCH="$ARCH" IMAGE="$IMAGE"
FIRST_TARGET=$(readlink "$IMAGE")
if [ -z "$FIRST_TARGET" ]; then
  echo "image builder did not publish an immutable-generation pointer" >&2
  exit 1
fi

if ABOX_IMAGE_FAIL_BEFORE_PUBLISH=1 make -C "$ROOT" image GUEST_ARCH="$ARCH" IMAGE="$IMAGE"; then
  echo "injected image publication failure unexpectedly succeeded" >&2
  exit 1
fi
SECOND_TARGET=$(readlink "$IMAGE")
if [ "$FIRST_TARGET" != "$SECOND_TARGET" ]; then
  echo "failed image build changed the current-generation pointer" >&2
  exit 1
fi

RESOLVED=$(readlink -f "$IMAGE")
MANIFEST="$RESOLVED.manifest.json"
python3 - "$RESOLVED" "$MANIFEST" "$ARCH" <<'PY'
import hashlib
import json
import pathlib
import sys

image = pathlib.Path(sys.argv[1])
manifest_path = pathlib.Path(sys.argv[2])
arch = sys.argv[3]
manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
digest = hashlib.sha256(image.read_bytes()).hexdigest()
if manifest != {
    "schema": 1,
    "arch": arch,
    "image_id": "abox-guest-dev",
    "protocol": 4,
    "sha256": digest,
}:
    raise SystemExit(f"unexpected image manifest: {manifest!r}")
PY

e2fsck -fn "$RESOLVED"
ROOT_STAT=$(debugfs -R 'stat /' "$RESOLVED" 2>&1)
GUEST_STAT=$(debugfs -R 'stat /usr/local/bin/abox-guest' "$RESOLVED" 2>&1)
REPO_STAT=$(debugfs -R 'stat /work/repo' "$RESOLVED" 2>&1)
BUSYBOX_STAT=$(debugfs -R 'stat /bin/busybox' "$RESOLVED" 2>&1)
printf '%s\n' "$ROOT_STAT" | grep -Eq 'User:[[:space:]]+0[[:space:]]+Group:[[:space:]]+0'
printf '%s\n' "$ROOT_STAT" | grep -Eq 'Mode:[[:space:]]+0755'
printf '%s\n' "$GUEST_STAT" | grep -Eq 'User:[[:space:]]+0[[:space:]]+Group:[[:space:]]+0'
printf '%s\n' "$GUEST_STAT" | grep -Eq 'Mode:[[:space:]]+0755'
printf '%s\n' "$REPO_STAT" | grep -Eq 'User:[[:space:]]+1000[[:space:]]+Group:[[:space:]]+1000'
printf '%s\n' "$BUSYBOX_STAT" | grep -Eq 'Mode:[[:space:]]+0755'

echo "rootless $ARCH image build and atomic publication checks passed (build-only; no KVM evidence)"
