#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
MANIFEST="$ROOT/packaging/libkrun-required-symbols.txt"

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <exact-libkrun-version> <firmware-soname>" >&2
  exit 2
fi
EXPECTED_VERSION=$1
FIRMWARE_SONAME=$2

for tool in pkg-config cc nm strings cmp; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "required compatibility-check tool is missing: $tool" >&2
    exit 1
  fi
done

WORK=$(mktemp -d "${TMPDIR:-/tmp}/abox-libkrun-check.XXXXXX")
trap 'rm -rf "$WORK"' EXIT HUP INT TERM

sh "$ROOT/scripts/generate-libkrun-symbols.sh" > "$WORK/generated-symbols.txt"
if ! cmp -s "$MANIFEST" "$WORK/generated-symbols.txt"; then
  echo "libkrun symbol manifest is stale; regenerate it with:" >&2
  echo "  sh scripts/generate-libkrun-symbols.sh > packaging/libkrun-required-symbols.txt" >&2
  diff -u "$MANIFEST" "$WORK/generated-symbols.txt" >&2 || true
  exit 1
fi

ACTUAL_VERSION=$(pkg-config --modversion libkrun)
if [ "$ACTUAL_VERSION" != "$EXPECTED_VERSION" ]; then
  echo "libkrun pkg-config version is $ACTUAL_VERSION; expected $EXPECTED_VERSION" >&2
  exit 1
fi

{
  echo '#include <libkrun.h>'
  echo 'static void abox_check_header(void) {'
  while IFS= read -r symbol; do
    [ -n "$symbol" ] || continue
    printf '  (void)&%s;\n' "$symbol"
  done < "$MANIFEST"
  echo '}'
} > "$WORK/header-check.c"
# shellcheck disable=SC2046
cc -Werror $(pkg-config --cflags libkrun) -c "$WORK/header-check.c" -o "$WORK/header-check.o"

LIBDIR=$(pkg-config --variable=libdir libkrun)
LIBRARY=
for candidate in "$LIBDIR/libkrun.so" "$LIBDIR"/libkrun.so.*; do
  if [ -f "$candidate" ]; then
    LIBRARY=$(readlink -f "$candidate")
    break
  fi
done
if [ -z "$LIBRARY" ]; then
  echo "pkg-config libdir contains no libkrun shared library: $LIBDIR" >&2
  exit 1
fi

nm -D --defined-only "$LIBRARY" | while IFS= read -r line; do
  set -- $line
  symbol=${3:-}
  printf '%s\n' "${symbol%%@*}"
done | LC_ALL=C sort -u > "$WORK/exported-symbols.txt"

while IFS= read -r symbol; do
  [ -n "$symbol" ] || continue
  if ! grep -Fxq "$symbol" "$WORK/exported-symbols.txt"; then
    echo "$LIBRARY does not export required symbol $symbol" >&2
    exit 1
  fi
done < "$MANIFEST"

if ! strings "$LIBRARY" | grep -Fq "$FIRMWARE_SONAME"; then
  echo "$LIBRARY does not reference expected firmware SONAME $FIRMWARE_SONAME" >&2
  exit 1
fi

echo "verified libkrun $ACTUAL_VERSION, $FIRMWARE_SONAME, and all required binding symbols"
