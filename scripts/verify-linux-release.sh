#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)

if [ "$#" -ne 3 ]; then
  echo "usage: $0 <archive> <expected-sha256> <exact-libkrun-version>" >&2
  exit 2
fi
ARCHIVE=$1
EXPECTED=$2
LIBKRUN_VERSION=$3

ACTUAL=$(sha256sum "$ARCHIVE")
ACTUAL=${ACTUAL%% *}
if [ "$ACTUAL" != "$EXPECTED" ]; then
  echo "candidate digest mismatch: got $ACTUAL, expected $EXPECTED" >&2
  exit 1
fi

WORK=$(mktemp -d "${TMPDIR:-/tmp}/abox-release-verify.XXXXXX")
trap 'rm -rf "$WORK"' EXIT HUP INT TERM
PAYLOAD_NAME=$(python3 - "$ARCHIVE" <<'PY'
import pathlib
import re
import sys
import tarfile

with tarfile.open(sys.argv[1], "r:gz") as archive:
    members = archive.getmembers()
    files = set()
    names = set()
    roots = set()
    for member in members:
        path = pathlib.PurePosixPath(member.name)
        if path.is_absolute() or ".." in path.parts or not path.parts:
            raise SystemExit(f"unsafe archive member: {member.name!r}")
        if member.name in names:
            raise SystemExit(f"duplicate archive member: {member.name!r}")
        names.add(member.name)
        if not (member.isdir() or member.isfile()):
            raise SystemExit(f"unsupported archive member type: {member.name!r}")
        if member.size > 1024 * 1024 * 1024:
            raise SystemExit(f"archive member is too large: {member.name!r}")
        roots.add(path.parts[0])
        if member.isfile():
            files.add(member.name)

if len(roots) != 1:
    raise SystemExit(f"candidate archive has unexpected roots: {sorted(roots)!r}")
root = roots.pop()
if not re.fullmatch(r"abox_v[0-9]+\.[0-9]+\.[0-9]+_linux_amd64", root):
    raise SystemExit(f"candidate archive has unexpected root: {root!r}")
expected = {
    f"{root}/SHA256SUMS",
    f"{root}/INSTALL",
    f"{root}/abox",
    f"{root}/abox-vmm",
    f"{root}/abox-guest-linux-amd64",
    f"{root}/abox-guest-linux-amd64.raw.manifest.json",
    f"{root}/abox-guest-linux-amd64.raw.zst",
}
if files != expected:
    raise SystemExit(
        f"candidate archive file set differs: missing={sorted(expected - files)!r}, "
        f"extra={sorted(files - expected)!r}"
    )
directories = {member.name.rstrip("/") for member in members if member.isdir()}
if directories != {root}:
    raise SystemExit(f"candidate archive has unexpected directories: {sorted(directories)!r}")
expected_modes = {
    f"{root}/INSTALL": 0o644,
    f"{root}/SHA256SUMS": 0o644,
    f"{root}/abox": 0o755,
    f"{root}/abox-vmm": 0o755,
    f"{root}/abox-guest-linux-amd64": 0o755,
    f"{root}/abox-guest-linux-amd64.raw.manifest.json": 0o444,
    f"{root}/abox-guest-linux-amd64.raw.zst": 0o644,
}
for member in members:
    name = member.name.rstrip("/")
    expected_mode = 0o755 if member.isdir() else expected_modes.get(name)
    if expected_mode is None or member.mode & 0o777 != expected_mode:
        raise SystemExit(f"candidate archive mode is invalid: {member.name!r} {member.mode & 0o777:o}")
print(root)
PY
)
tar -xzf "$ARCHIVE" -C "$WORK"
PAYLOAD="$WORK/$PAYLOAD_NAME"

(
  cd "$PAYLOAD"
  sha256sum -c SHA256SUMS
)

for binary in abox abox-vmm abox-guest-linux-amd64; do
  readelf -h "$PAYLOAD/$binary" | grep -Eq 'Machine:[[:space:]]+Advanced Micro Devices X86-64'
done
readelf -d "$PAYLOAD/abox-vmm" | grep -Eq 'NEEDED.*\[libkrun\.so\.1\]'

(ulimit -f 2097152; zstd -q -d "$PAYLOAD/abox-guest-linux-amd64.raw.zst" -o "$WORK/guest.raw")
[ "$(stat -c %s "$WORK/guest.raw")" -eq 805306368 ] || {
  echo "release guest image has an unexpected uncompressed size" >&2
  exit 1
}
python3 - "$WORK/guest.raw" "$PAYLOAD/abox-guest-linux-amd64.raw.manifest.json" "$PAYLOAD_NAME" <<'PY'
import hashlib
import json
import pathlib
import sys

image = pathlib.Path(sys.argv[1])
manifest = json.loads(pathlib.Path(sys.argv[2]).read_text(encoding="utf-8"))
payload_name = sys.argv[3]
hasher = hashlib.sha256()
with image.open("rb") as stream:
    for chunk in iter(lambda: stream.read(1024 * 1024), b""):
        hasher.update(chunk)
digest = hasher.hexdigest()
if manifest.get("schema") != 1 or manifest.get("arch") != "amd64":
    raise SystemExit(f"invalid release image manifest: {manifest!r}")
if manifest.get("protocol") != 4 or manifest.get("sha256") != digest:
    raise SystemExit("release image digest/protocol does not match its manifest")
expected_image_id = payload_name.removeprefix("abox_").removesuffix("_linux_amd64")
if manifest.get("image_id") != expected_image_id:
    raise SystemExit("release image ID does not match the release tag")
PY
e2fsck -fn "$WORK/guest.raw"
debugfs -R "dump /usr/local/bin/abox-guest $WORK/image-guest" "$WORK/guest.raw" >/dev/null 2>&1
cmp "$WORK/image-guest" "$PAYLOAD/abox-guest-linux-amd64"

sh "$ROOT/scripts/check-libkrun.sh" "$LIBKRUN_VERSION" libkrunfw.so.5
echo "verified exact Linux candidate $ACTUAL (packaging/link checks only; no isolation claim)"
