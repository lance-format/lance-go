#!/usr/bin/env bash
# Exercise the exact hosted-artifact path normalization and identity checker.
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

downloads="$work/downloads"
candidates="$work/candidates"
mkdir -p "$downloads"/{unix-linux-amd64,unix-linux-arm64,unix-darwin-arm64,windows-amd64/windows-amd64,windows-arm64/windows-arm64}/module
for target in linux-amd64 linux-arm64 darwin-arm64 windows-amd64 windows-arm64; do
  case "$target" in
  linux-amd64) dir="$downloads/unix-linux-amd64/module" ;;
  linux-arm64) dir="$downloads/unix-linux-arm64/module" ;;
  darwin-arm64) dir="$downloads/unix-darwin-arm64/module" ;;
  windows-amd64) dir="$downloads/windows-amd64/windows-amd64/module" ;;
  windows-arm64) dir="$downloads/windows-arm64/windows-arm64/module" ;;
  esac
  printf 'package artifact\n' > "$dir/artifact.go"
  printf 'module fixture/%s\n\ngo 1.23.0\n' "$target" > "$dir/go.mod"
  printf 'payload-%s' "$target" > "$dir/payload.zst"
done
printf 'candidate\n' > "$downloads/windows-amd64/windows-amd64/candidate-manifest.txt"
printf 'candidate\n' > "$downloads/windows-arm64/windows-arm64/candidate-manifest.txt"

"$repo_root/scripts/go-module-origin/stage-candidates.sh" "$downloads" "$candidates"
python3 - "$candidates" "$work/identity.json" <<'PY'
import hashlib
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
targets = {}
for target in ("linux-amd64", "linux-arm64", "darwin-arm64", "windows-amd64", "windows-arm64"):
    module = root / target / "module"
    payload = next(module.glob("*.zst"))
    targets[target] = {
        "candidate_artifact_go_sha256": hashlib.sha256((module / "artifact.go").read_bytes()).hexdigest(),
        "candidate_go_mod_sha256": hashlib.sha256((module / "go.mod").read_bytes()).hexdigest(),
        "candidate_payload_sha256": hashlib.sha256(payload.read_bytes()).hexdigest(),
    }
pathlib.Path(sys.argv[2]).write_text(json.dumps({"targets": targets}, indent=2) + "\n")
PY
"$repo_root/scripts/go-module-origin/verify-candidate-identity.py" "$work/identity.json" "$candidates"

# A wrong extraction layout must fail rather than silently reading the target
# directory instead of its module directory.
mv "$downloads/windows-arm64/windows-arm64/module" "$downloads/windows-arm64/windows-arm64/module-bad"
if "$repo_root/scripts/go-module-origin/stage-candidates.sh" "$downloads" "$work/bad" >/dev/null 2>&1; then
  echo "stage-candidates accepted a wrong extraction path" >&2
  exit 1
fi
echo "stage fixture: PASS"
