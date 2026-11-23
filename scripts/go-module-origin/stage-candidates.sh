#!/usr/bin/env bash
# Normalize the exact hosted candidate-artifact extraction trees into the
# <target>/module layout consumed by the generator.
set -euo pipefail

downloads=${1:?downloads directory is required}
candidates=${2:?candidates directory is required}

rm -rf "$candidates"
mkdir -p "$candidates"

copy_module() {
  local target=$1 source=$2
  [[ -d $source ]] || { echo "missing candidate module: $source" >&2; exit 1; }
  mkdir -p "$candidates/$target"
  cp -a "$source" "$candidates/$target/module"
  [[ -f $candidates/$target/module/artifact.go ]] || { echo "missing artifact.go: $target" >&2; exit 1; }
  [[ -f $candidates/$target/module/go.mod ]] || { echo "missing go.mod: $target" >&2; exit 1; }
  [[ $(find "$candidates/$target/module" -maxdepth 1 -name '*.zst' | wc -l) -eq 1 ]] || {
    echo "expected one payload in $target" >&2
    exit 1
  }
}

copy_module linux-amd64 "$downloads/unix-linux-amd64/module"
copy_module linux-arm64 "$downloads/unix-linux-arm64/module"
copy_module darwin-arm64 "$downloads/unix-darwin-arm64/module"
copy_module windows-amd64 "$downloads/windows-amd64/windows-amd64/module"
copy_module windows-arm64 "$downloads/windows-arm64/windows-arm64/module"

echo "candidate modules normalized: $candidates"
