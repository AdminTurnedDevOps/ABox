#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "Arch package installation must run as root" >&2
  exit 1
fi

BASE=https://archive.archlinux.org/packages/l
KRUN=libkrun-1.19.4-1-x86_64.pkg.tar.zst
KRUNFW=libkrunfw-5.5.0-1-x86_64.pkg.tar.zst
KRUN_SHA256=cdda6e0006f69d9d45fa3d54b83e360c6779c67d3fa04cfd97eab31689504732
KRUN_SIG_SHA256=18cd1fd25f1472b5fab5e39ab88c9297a787e906aec77cea16c561d010c05cba
KRUNFW_SHA256=6c6414e4f8c5fc2f74ed4f330b3e2c0872e07a87d2c35b389ad6105ba7c16338
KRUNFW_SIG_SHA256=14c9e94acd5450b7fe6c3a5b6d4e72009dcf352628586905edd9607ca6dd01e2

WORK=$(mktemp -d "${TMPDIR:-/tmp}/abox-arch-libkrun.XXXXXX")
trap 'rm -rf "$WORK"' EXIT HUP INT TERM

download() {
  curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 \
    --output "$WORK/$2" "$1/$2"
}

download "$BASE/libkrun" "$KRUN"
download "$BASE/libkrun" "$KRUN.sig"
download "$BASE/libkrunfw" "$KRUNFW"
download "$BASE/libkrunfw" "$KRUNFW.sig"

printf '%s  %s\n' "$KRUN_SHA256" "$WORK/$KRUN" \
  "$KRUN_SIG_SHA256" "$WORK/$KRUN.sig" \
  "$KRUNFW_SHA256" "$WORK/$KRUNFW" \
  "$KRUNFW_SIG_SHA256" "$WORK/$KRUNFW.sig" | sha256sum -c -
pacman-key --verify "$WORK/$KRUN.sig" "$WORK/$KRUN"
pacman-key --verify "$WORK/$KRUNFW.sig" "$WORK/$KRUNFW"
pacman -U --needed --noconfirm "$WORK/$KRUNFW" "$WORK/$KRUN"

test "$(pacman -Q libkrun)" = 'libkrun 1.19.4-1'
test "$(pacman -Q libkrunfw)" = 'libkrunfw 5.5.0-1'
