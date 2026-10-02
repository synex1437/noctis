#!/usr/bin/env sh
set -eu
fail() {
  printf 'noctis: %s\n' "$1" >&2
  exit 1
}
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
system=$(uname -s)
case "$system" in
  Linux)
    machine=$(uname -m)
    case "$machine" in
      x86_64|amd64) platform=linux-amd64 ;;
      arm64|aarch64) platform=linux-arm64 ;;
      *) fail "the $machine CPU is not supported (binaries are built for x86_64 and arm64)" ;;
    esac
    ;;
  # One universal binary holds the x86_64 and arm64 builds, and macOS runs the one for this CPU.
  Darwin) platform=darwin ;;
  MINGW*|MSYS*|CYGWIN*|*_NT*)
    hint='.\scripts\install.ps1'
    for arg in "$@"; do
      case "$arg" in --uninstall) hint='.\scripts\install.ps1 -Uninstall' ;; esac
    done
    fail "install.sh is for macOS and Linux; on Windows, run $hint in PowerShell instead" ;;
  *) fail "$system is not supported (binaries are built for macOS, Linux and Windows)" ;;
esac
binary="$root/bin/$platform/noctis"
[ -f "$binary" ] || fail "no binary for $platform at $binary"
[ -x "$binary" ] || chmod +x "$binary"
exec "$binary" install --source "$root" "$@"
