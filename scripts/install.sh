#!/usr/bin/env sh
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in arm64|aarch64) arch=arm64 ;; *) arch=amd64 ;; esac
binary="$root/bin/$os-$arch/noctis"
if [ ! -f "$binary" ]; then
  echo "noctis: no binary for $os-$arch at $binary" >&2
  exit 1
fi
[ -x "$binary" ] || chmod +x "$binary"
exec "$binary" install --source "$root" "$@"
