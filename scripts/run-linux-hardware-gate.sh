#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
MANIFEST="$ROOT/.github/acceptance/linux-kvm-v1.json"

if [ "$#" -ne 7 ]; then
  echo "usage: $0 <arch|fedora> <archive> <sha256> <commit> <expected-runner> <harness> <harness-sha256>" >&2
  exit 2
fi
DISTRO=$1
ARCHIVE=$2
CANDIDATE_SHA256=$3
COMMIT=$4
EXPECTED_RUNNER=$5
HARNESS=$6
HARNESS_SHA256=$7

case "$ARCHIVE" in
  */abox_v[0-9]*.[0-9]*.[0-9]*_linux_amd64.tar.gz) ;;
  *) echo "invalid candidate archive name: $ARCHIVE" >&2; exit 1 ;;
esac
case "$CANDIDATE_SHA256" in
  *[!0-9a-f]*|'') echo "candidate SHA-256 must be lowercase hexadecimal" >&2; exit 1 ;;
esac
[ "${#CANDIDATE_SHA256}" -eq 64 ] || { echo "candidate SHA-256 must have 64 digits" >&2; exit 1; }
case "$COMMIT" in
  *[!0-9a-f]*|'') echo "commit must be lowercase hexadecimal" >&2; exit 1 ;;
esac
[ "${#COMMIT}" -eq 40 ] || { echo "commit must have 40 digits" >&2; exit 1; }

if [ -z "$EXPECTED_RUNNER" ] || [ "${RUNNER_NAME:-}" != "$EXPECTED_RUNNER" ]; then
  echo "protected runner identity mismatch: got ${RUNNER_NAME:-unset}, expected ${EXPECTED_RUNNER:-unset}" >&2
  exit 1
fi
if [ ! -c /dev/kvm ] || [ ! -r /dev/kvm ] || [ ! -w /dev/kvm ]; then
  echo "protected hardware gate requires readable/writable /dev/kvm" >&2
  exit 1
fi
if grep -Eqi '(microsoft|wsl)' /proc/sys/kernel/osrelease; then
  echo "WSL is not an accepted KVM hardware runner" >&2
  exit 1
fi
if command -v systemd-detect-virt >/dev/null 2>&1; then
  if systemd-detect-virt --container >/dev/null 2>&1; then
    echo "containerized VMM execution is not accepted as hardware evidence" >&2
    exit 1
  fi
  VIRT=$(systemd-detect-virt 2>/dev/null || true)
  if [ -n "$VIRT" ] && [ "$VIRT" != none ]; then
    echo "hardware gate requires a named physical host; detected virtualization: $VIRT" >&2
    exit 1
  fi
fi

. /etc/os-release
case "$DISTRO" in
  arch)
    [ "${ID:-}" = arch ] || { echo "Arch gate ran on ${ID:-unknown}" >&2; exit 1; }
    [ "$(pacman -Q libkrun)" = "libkrun 1.19.4-1" ] || { pacman -Q libkrun >&2; exit 1; }
    [ "$(pacman -Q libkrunfw)" = "libkrunfw 5.5.0-1" ] || { pacman -Q libkrunfw >&2; exit 1; }
    LIBKRUN_VERSION=1.19.4
    LIBKRUN_PACKAGE=$(pacman -Q libkrun)
    LIBKRUNFW_PACKAGE=$(pacman -Q libkrunfw)
    ;;
  fedora)
    [ "${ID:-}" = fedora ] && [ "${VERSION_ID:-}" = 44 ] || {
      echo "Fedora 44 gate ran on ${PRETTY_NAME:-unknown}" >&2
      exit 1
    }
    [ "$(rpm -q libkrun)" = "libkrun-1.19.0-1.fc44.x86_64" ] || { rpm -q libkrun >&2; exit 1; }
    [ "$(rpm -q libkrun-devel)" = "libkrun-devel-1.19.0-1.fc44.x86_64" ] || { rpm -q libkrun-devel >&2; exit 1; }
    [ "$(rpm -q libkrunfw)" = "libkrunfw-5.5.0-1.fc44.x86_64" ] || { rpm -q libkrunfw >&2; exit 1; }
    if command -v getenforce >/dev/null 2>&1; then
      [ "$(getenforce)" = Enforcing ] || { echo "Fedora gate requires SELinux enforcing" >&2; exit 1; }
    else
      echo "Fedora gate cannot verify SELinux enforcing mode" >&2
      exit 1
    fi
    LIBKRUN_VERSION=1.19.0
    LIBKRUN_PACKAGE=$(rpm -q libkrun)
    LIBKRUNFW_PACKAGE=$(rpm -q libkrunfw)
    ;;
  *) echo "unknown hardware-gate distro: $DISTRO" >&2; exit 2 ;;
esac

if [ ! -x "$HARNESS" ]; then
  echo "protected runner harness is missing or not executable: $HARNESS" >&2
  exit 1
fi
OWNER_MODE=$(stat -c '%u %a' "$HARNESS")
set -- $OWNER_MODE
if [ "$1" -ne 0 ] || [ $((0$2 & 0022)) -ne 0 ]; then
  echo "hardware harness must be root-owned and not group/other writable: $OWNER_MODE" >&2
  exit 1
fi
ACTUAL_HARNESS_SHA256=$(sha256sum "$HARNESS")
ACTUAL_HARNESS_SHA256=${ACTUAL_HARNESS_SHA256%% *}
if [ -z "$HARNESS_SHA256" ] || [ "$ACTUAL_HARNESS_SHA256" != "$HARNESS_SHA256" ]; then
  echo "protected hardware harness digest mismatch" >&2
  exit 1
fi

sh "$ROOT/scripts/verify-linux-release.sh" "$ARCHIVE" "$CANDIDATE_SHA256" "$LIBKRUN_VERSION"

EVIDENCE=${ABOX_EVIDENCE_DIR:?ABOX_EVIDENCE_DIR must name an empty evidence directory}
mkdir -p "$EVIDENCE"
[ -z "$(ls -A "$EVIDENCE")" ] || { echo "evidence directory is not empty" >&2; exit 1; }
REPORT="$EVIDENCE/report.json"

"$HARNESS" \
  --acceptance-manifest "$MANIFEST" \
  --candidate "$ARCHIVE" \
  --candidate-sha256 "$CANDIDATE_SHA256" \
  --commit "$COMMIT" \
  --distro "$DISTRO" \
  --runner "$RUNNER_NAME" \
  --output "$REPORT"

MANIFEST_SHA256=$(python3 "$ROOT/scripts/validate-hardware-report.py" \
  "$MANIFEST" "$REPORT" "$DISTRO" "$COMMIT" "$CANDIDATE_SHA256" "$RUNNER_NAME")

{
  echo "runner=$RUNNER_NAME"
  echo "distro=$PRETTY_NAME"
  echo "kernel=$(uname -srvo)"
  echo "architecture=$(uname -m)"
  if command -v lscpu >/dev/null 2>&1; then
    lscpu | grep -E '^(Model name|Vendor ID|Virtualization):' || true
  fi
  echo "commit=$COMMIT"
  echo "candidate_sha256=$CANDIDATE_SHA256"
  echo "acceptance_manifest_sha256=$MANIFEST_SHA256"
  echo "harness_sha256=$ACTUAL_HARNESS_SHA256"
  echo "libkrun_version=$LIBKRUN_VERSION"
  echo "libkrun_package=$LIBKRUN_PACKAGE"
  echo "libkrunfw_package=$LIBKRUNFW_PACKAGE"
  echo "libkrun_soname=libkrun.so.1"
  echo "firmware_soname=libkrunfw.so.5"
  if command -v getenforce >/dev/null 2>&1; then echo "selinux=$(getenforce)"; fi
  if command -v lsmod >/dev/null 2>&1; then lsmod | grep '^kvm' || true; fi
} > "$EVIDENCE/host.txt"

echo "$MANIFEST_SHA256"
