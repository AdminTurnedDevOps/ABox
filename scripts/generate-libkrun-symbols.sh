#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
BINDING=${1:-"$ROOT/cmd/abox-vmm/start_libkrun.go"}

if [ ! -f "$BINDING" ]; then
  echo "libkrun binding not found: $BINDING" >&2
  exit 1
fi

SYMBOLS=$(sed -n 's/.*C\.\(krun_[[:alnum:]_]*\).*/\1/p' "$BINDING" | LC_ALL=C sort -u)
if [ -z "$SYMBOLS" ]; then
  echo "no C.krun_* calls found in $BINDING" >&2
  exit 1
fi

printf '%s\n' "$SYMBOLS"
