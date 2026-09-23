#!/usr/bin/env sh
set -eu
fail() {
  printf 'noctis: %s\n' "$1" >&2
  exit 1
}
root=$(cd "$(dirname "$0")/.." && pwd)
system=$(uname -s)
case "$system" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  MINGW*|MSYS*|CYGWIN*|*_NT*)
    hint='.\scripts\install.ps1'
    for arg in "$@"; do
      case "$arg" in --uninstall) hint='.\scripts\install.ps1 -Uninstall' ;; esac
    done
    fail "install.sh is for macOS and Linux; on Windows, run $hint in PowerShell instead" ;;
  *) fail "$system is not supported (binaries are built for macOS, Linux and Windows)" ;;
esac
machine=$(uname -m)
case "$machine" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail "the $machine CPU is not supported (binaries are built for x86_64 and arm64)" ;;
esac
binary="$root/bin/$os-$arch/noctis"
[ -f "$binary" ] || fail "no binary for $os-$arch at $binary"
[ -x "$binary" ] || chmod +x "$binary"
exec "$binary" install --source "$root" "$@"
