#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

case "${ABOX_HOST_OS:-$(uname -s)}" in
  Linux)
    exec sh "$ROOT/images/build-guest-linux.sh"
    ;;
  Darwin)
    exec sh "$ROOT/images/build-guest-darwin.sh"
    ;;
  *)
    echo "guest image builds are supported on Linux and macOS" >&2
    exit 1
    ;;
esac
