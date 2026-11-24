#!/usr/bin/env bash
# Verify a generated origin tree with the real Go protocol, a local HTTP
# discovery/proxy, the five recorded library hashes, and the SDK
# roundtrip.
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
origin=${ORIGIN_DIR:?ORIGIN_DIR must point at the generated origin tree}
manifest="$origin/manifest.json"
GO=${GO:-go}
python3 - "$manifest" <<'PY'
import json
import pathlib
import sys
manifest = json.loads(pathlib.Path(sys.argv[1]).read_text())
assert manifest["module_prefix"] == "go.lance.org/liblance", manifest["module_prefix"]
assert manifest["version"] == "v0.3.1-preview.1", manifest["version"]
objects = manifest["objects"]
assert len(objects) == 25, len(objects)
keys = {obj["key"] for obj in objects}
for target in ("linux-amd64", "linux-arm64", "darwin-arm64", "windows-amd64", "windows-arm64"):
    assert f"liblance/{target}" in keys
    module = f"go.lance.org/liblance/{target}"
    for suffix in ("list", "v0.3.1-preview.1.info", "v0.3.1-preview.1.mod", "v0.3.1-preview.1.zip"):
        assert f"modules/{module}/@v/{suffix}" in keys, (target, suffix)
print("manifest keys: ok")
PY

work=$(mktemp -d)
cleanup() {
  chmod -R u+w "$work" 2>/dev/null || true
  rm -rf "$work"
  [[ -n ${server_pid:-} ]] && kill "$server_pid" 2>/dev/null || true
}
trap cleanup EXIT

python3 - "$manifest" > "$work/modules.tsv" <<'PY'
import json
import sys
manifest = json.load(open(sys.argv[1], encoding="utf-8"))
mods = {obj["module"]: obj["gomod_h1"] for obj in manifest["objects"] if obj["key"].endswith(".mod")}
for obj in manifest["objects"]:
    if obj["key"].endswith(".zip"):
        print("\t".join([obj["module"], obj["version"], obj["zip_h1"], mods[obj["module"]], obj["key"]]))
PY

cache="$work/file-modcache"
while IFS=$'\t' read -r module version zip_h1 gomod_h1 key; do
  json="$work/$(basename "$key").json"
  GOPROXY="file://$origin/modules" GOSUMDB=off GOMODCACHE="$cache" GOCACHE="$work/gocache" \
    GOFLAGS=-mod=mod "$GO" mod download -json "$module@$version" > "$json"
  python3 - "$json" "$zip_h1" "$gomod_h1" "$module" <<'PY'
import json
import sys
data = json.load(open(sys.argv[1], encoding="utf-8"))
assert not data.get("Error"), data
assert data["Sum"] == sys.argv[2], (sys.argv[4], data["Sum"], sys.argv[2])
assert data["GoModSum"] == sys.argv[3], (sys.argv[4], data["GoModSum"], sys.argv[3])
print(sys.argv[4], "file-proxy h1: ok")
PY
done < "$work/modules.tsv"

# Verify the recorded library hashes from the ZIP payloads.
declare -A library_sha=(
  [linux-amd64]='b960378ec4392f094ce5eb5a0fc7e654d04abb7f9f2ce036c94cfa9da7c92f08'
  [linux-arm64]='9094bfc27dca63b3cbd457f78833ec5ae4016a253e28c3eb014994234ba5f7cc'
  [darwin-arm64]='580235babb62b5da85b17ea3cc51562ce68d96f180090ad2d8685ba46691ed06'
  [windows-amd64]='c902ff0495a79d78ee83563d612410dff45142e33633609d9c3ed21da60b1bc9'
  [windows-arm64]='d7fc1af022779a894960de843affdbab7b3104fba4a9e7e78ed53739f5ddd462'
)
for target in linux-amd64 linux-arm64 darwin-arm64 windows-amd64 windows-arm64; do
  zip="$origin/modules/go.lance.org/liblance/$target/@v/v0.3.1-preview.1.zip"
  payload=$(unzip -Z1 "$zip" | grep '\.zst$' | head -1)
  [[ -n $payload ]] || { echo "$target: no payload in zip" >&2; exit 1; }
  unzip -p "$zip" "$payload" > "$work/$target.zst"
  zstd -q -d -f "$work/$target.zst" -o "$work/$target.lib"
  actual=$(sha256sum "$work/$target.lib" | awk '{print $1}')
  [[ $actual == "${library_sha[$target]}" ]] || { echo "$target: library hash $actual, want ${library_sha[$target]}" >&2; exit 1; }
  echo "$target official library: ok"
done

# HTTP discovery and HTTP proxy protocol.
server_pid=""
python3 "$repo_root/scripts/go-module-origin/test-server.py" --root "$origin" --port 18888 > "$work/server.log" 2>&1 &
server_pid=$!
sleep 1
curl -fsS "http://127.0.0.1:18888/liblance/linux-amd64?go-get=1" > "$work/discovery.html"
grep -F 'content="go.lance.org/liblance/linux-amd64 mod https://go.lance.org/modules"' "$work/discovery.html" >/dev/null
curl -fsSI "http://127.0.0.1:18888/liblance/linux-amd64?go-get=1" | grep -qi '^Content-type: text/html'
curl -fsS "http://127.0.0.1:18888/modules/go.lance.org/liblance/linux-amd64/@v/list" | grep -Fx 'v0.3.1-preview.1' >/dev/null
http_cache="$work/http-modcache"
GOPROXY="http://127.0.0.1:18888/modules" GOSUMDB=off GOMODCACHE="$http_cache" GOCACHE="$work/http-gocache" \
  GOFLAGS=-mod=mod "$GO" mod download -json "go.lance.org/liblance/linux-amd64@v0.3.1-preview.1" > "$work/http.json"
python3 - "$work/http.json" <<'PY'
import json
import sys
data = json.load(open(sys.argv[1], encoding="utf-8"))
assert not data.get("Error"), data
assert data["Sum"] == "h1:ZkFLYB5H4yXJY1EEAuhFnd5q54FKKLAo0m+zPx9UyFE=", data["Sum"]
print("http proxy and discovery: ok")
PY
kill "$server_pid" 2>/dev/null || true
server_pid=""

# SDK roundtrip with the unchanged import path, no CGO, no LANCE_LIB_PATH.
runtime_cache="$work/runtime-cache"
(
  cd "$repo_root"
  GOPROXY="file://$origin/modules,https://proxy.golang.org" GOSUMDB=off \
    GOMODCACHE="$work/root-modcache" GOCACHE="$work/root-gocache" \
    GOFLAGS=-mod=readonly CGO_ENABLED=0 LANCE_CACHE_DIR="$runtime_cache" \
    "$GO" run ./examples/roundtrip
) > "$work/roundtrip.log" 2>&1
grep -F 'Created, reopened and scanned 3 rows: [10 20 30]' "$work/roundtrip.log" >/dev/null
grep -F 'created 3-row' "$work/roundtrip.log" >/dev/null 2>&1 || true
cache_entry=$(find "$runtime_cache" -type f -name 'liblance_c.so' -o -name 'liblance_c.dylib' -o -name 'lance_c.dll' | head -1)
[[ -n $cache_entry ]] || { echo "no runtime library cache entry" >&2; exit 1; }
echo "runtime cache: $cache_entry"
echo "sdk roundtrip: ok"
echo "verification: PASS"
