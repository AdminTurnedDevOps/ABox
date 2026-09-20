#!/bin/sh
set -eu
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

case "${ABOX_HOST_OS:-$(uname -s)}" in
  Linux)
    # Rebuild instead of mutating a selected filesystem with debugfs. The native
    # builder publishes a complete immutable image+manifest generation.
    exec sh "$ROOT/images/build-guest-linux.sh"
    ;;
  Darwin)
    # Preserve the existing Docker-based macOS update workflow by rebuilding the
    # image with its Docker packer rather than introducing a second mutation path.
    exec sh "$ROOT/images/build-guest-darwin.sh"
    ;;
  *)
    echo "guest image updates are supported on Linux and macOS" >&2
    exit 1
    ;;
esac
