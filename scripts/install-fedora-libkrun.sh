#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "Fedora package installation must run as root" >&2
  exit 1
fi

BASE=https://dl.fedoraproject.org/pub/fedora/linux/updates/44/Everything/x86_64/Packages/l
KRUN=libkrun-1.19.0-1.fc44.x86_64.rpm
KRUN_DEVEL=libkrun-devel-1.19.0-1.fc44.x86_64.rpm
KRUNFW=libkrunfw-5.5.0-1.fc44.x86_64.rpm
KRUN_SHA256=2433513a051847a0ce6d35b932804168a70d512f846416a5a31d14d58632e243
KRUN_DEVEL_SHA256=d1458f3fcd2075fd4107e4a94f65f59de31fae8b9b7e204c65801bac27a3ba33
KRUNFW_SHA256=d006902bd255d13c74854c38fe4e8786b831c4004e056b49c3e2e88fadffd35e

WORK=$(mktemp -d "${TMPDIR:-/tmp}/abox-fedora-libkrun.XXXXXX")
trap 'rm -rf "$WORK"' EXIT HUP INT TERM

for package in "$KRUN" "$KRUN_DEVEL" "$KRUNFW"; do
  curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 \
    --output "$WORK/$package" "$BASE/$package"
done
printf '%s  %s\n' \
  "$KRUN_SHA256" "$WORK/$KRUN" \
  "$KRUN_DEVEL_SHA256" "$WORK/$KRUN_DEVEL" \
  "$KRUNFW_SHA256" "$WORK/$KRUNFW" | sha256sum -c -
rpmkeys --checksig "$WORK/$KRUN" "$WORK/$KRUN_DEVEL" "$WORK/$KRUNFW"
dnf -y install "$WORK/$KRUNFW" "$WORK/$KRUN" "$WORK/$KRUN_DEVEL"

test "$(rpm -q libkrun)" = 'libkrun-1.19.0-1.fc44.x86_64'
test "$(rpm -q libkrun-devel)" = 'libkrun-devel-1.19.0-1.fc44.x86_64'
test "$(rpm -q libkrunfw)" = 'libkrunfw-5.5.0-1.fc44.x86_64'
