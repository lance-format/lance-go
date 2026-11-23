#!/usr/bin/env bash
# Minimal Go module proxy protocol check for PR CI; no network or R2 access.
set -euo pipefail

work=$(mktemp -d)
trap 'chmod -R u+w "$work" 2>/dev/null || true; rm -rf "$work"' EXIT
proxy="$work/proxy"
module=go.lance.org/liblance/linux-amd64
version=v0.0.1
mkdir -p "$proxy/$module/@v"
printf 'module %s\n\ngo 1.26.0\n' "$module" > "$work/go.mod"
cp "$work/go.mod" "$proxy/$module/@v/$version.mod"
printf '{"Version":"%s","Time":"2026-11-23T06:10:00Z"}\n' "$version" > "$proxy/$module/@v/$version.info"
python3 - "$work/go.mod" "$proxy/$module/@v/$version.zip" "$module" "$version" <<'PY'
import sys
import zipfile

mod, out, module, version = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as archive:
    archive.write(mod, f"{module}@{version}/go.mod")
PY
printf '%s\n' "$version" > "$proxy/$module/@v/list"
GOPROXY="file://$proxy" GOSUMDB=off GOMODCACHE="$work/modcache" GOCACHE="$work/gocache" \
  GOFLAGS=-mod=mod go mod download -json "$module@$version" > "$work/download.json"
python3 - "$work/download.json" <<'PY'
import json
import sys
data = json.load(open(sys.argv[1], encoding="utf-8"))
assert not data.get("Error"), data
assert data.get("Sum", "").startswith("h1:"), data
assert data.get("GoModSum", "").startswith("h1:"), data
print("fixture protocol: PASS")
PY
