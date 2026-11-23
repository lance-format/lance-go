#!/usr/bin/env bash
# Lightweight publisher fixture test for PR CI; no R2 credentials or payloads.
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
work=$(mktemp -d)
server_pid=""
cleanup() {
  [[ -n ${server_pid:-} ]] && kill "$server_pid" 2>/dev/null || true
  chmod -R u+w "$work" 2>/dev/null || true
  rm -rf "$work"
}
trap cleanup EXIT

origin="$work/origin"
log="$work/requests.log"
list_key="modules/go.lance.org/liblance/linux-amd64/@v/list"
mkdir -p "$origin/modules/go.lance.org/liblance/linux-amd64/@v" "$origin/liblance"
printf 'v0.0.1\n' > "$origin/$list_key"
printf '{"Version":"v0.0.1","Time":"2026-11-23T06:10:00Z"}\n' > "$origin/modules/go.lance.org/liblance/linux-amd64/@v/v0.0.1.info"
printf 'module go.lance.org/liblance/linux-amd64\n\ngo 1.26.0\n' > "$origin/modules/go.lance.org/liblance/linux-amd64/@v/v0.0.1.mod"
printf '<!doctype html><meta name="go-import" content="go.lance.org/liblance/linux-amd64 mod https://go.lance.org/modules">\n' > "$origin/liblance/linux-amd64"
python3 - "$origin" "$list_key" <<'PY'
import hashlib
import json
import pathlib
import sys

origin = pathlib.Path(sys.argv[1])
list_key = sys.argv[2]
objects = []
for key, kind, immutable, ctype, cache in [
    ("liblance/linux-amd64", "discovery", True, "text/html; charset=utf-8", "public, max-age=300"),
    (list_key, "list", False, "text/plain; charset=utf-8", "public, max-age=60"),
    ("modules/go.lance.org/liblance/linux-amd64/@v/v0.0.1.info", "v0.0.1.info", True, "application/json", "public, max-age=31536000, immutable"),
    ("modules/go.lance.org/liblance/linux-amd64/@v/v0.0.1.mod", "v0.0.1.mod", True, "text/plain; charset=utf-8", "public, max-age=31536000, immutable"),
]:
    data = (origin / key).read_bytes()
    objects.append({
        "key": key, "kind": kind, "module": "go.lance.org/liblance/linux-amd64",
        "version": "v0.0.1", "target": "linux-amd64",
        "sha256": hashlib.sha256(data).hexdigest(), "size": len(data),
        "content_type": ctype, "cache_control": cache, "immutable": immutable,
    })
manifest = {
    "version": "v0.0.1", "module_prefix": "go.lance.org/liblance",
    "base_url": "https://go.lance.org", "time": "2026-11-23T06:10:00Z",
    "source_commit": "fixture", "source_release": "fixture", "objects": objects,
}
(origin / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
PY

python3 "$repo_root/scripts/go-module-origin/mock-s3.py" --root "$work/store" --port 18791 --log "$log" > "$work/server.log" 2>&1 &
server_pid=$!
sleep 1
export AWS_NO_SIGN_REQUEST=1 AWS_DEFAULT_REGION=auto AWS_EC2_METADATA_DISABLED=true
publish=( "$repo_root/scripts/publish-go-module-origin.sh" --bucket test-bucket --endpoint http://127.0.0.1:18791 )

"${publish[@]}" --manifest "$origin/manifest.json" > "$work/first.log" 2>&1
grep -F 'publication verified' "$work/first.log" >/dev/null
first=$(grep -c '^PUT ' "$log")
[[ $first -eq 4 ]] || { echo "first PUT count $first, want 4" >&2; exit 1; }

"${publish[@]}" --manifest "$origin/manifest.json" > "$work/repeat.log" 2>&1
grep -F 'publication verified' "$work/repeat.log" >/dev/null
second=$(grep -c '^PUT ' "$log")
[[ $second -eq $first ]] || { echo "repeat added PUTs" >&2; exit 1; }

# Immutable conflict must abort and leave the conflicting bytes.
printf 'conflict\n' > "$work/store/modules/go.lance.org/liblance/linux-amd64/@v/v0.0.1.mod"
before=$(grep -c '^PUT ' "$log")
set +e
"${publish[@]}" --manifest "$origin/manifest.json" > "$work/conflict.log" 2>&1
rc=$?
set -e
[[ $rc -ne 0 ]] || { echo "publisher accepted conflict" >&2; exit 1; }
grep -F 'immutable object conflict' "$work/conflict.log" >/dev/null
[[ $(cat "$work/store/modules/go.lance.org/liblance/linux-amd64/@v/v0.0.1.mod") == conflict ]] || { echo "conflict overwritten" >&2; exit 1; }
after=$(grep -c '^PUT ' "$log")
[[ $after -eq $before ]] || { echo "conflict wrote objects" >&2; exit 1; }

# Restore the immutable file and update the list to v0.0.2; the older version
# must remain.
printf 'module go.lance.org/liblance/linux-amd64\n\ngo 1.26.0\n' > "$work/store/modules/go.lance.org/liblance/linux-amd64/@v/v0.0.1.mod"
printf 'v0.0.2\n' > "$origin/$list_key"
python3 - "$origin/manifest.json" <<'PY'
import hashlib
import json
import pathlib
import sys
path = pathlib.Path(sys.argv[1])
data = json.loads(path.read_text())
for obj in data["objects"]:
    if obj["key"].endswith("@v/list"):
        raw = (path.parent / obj["key"]).read_bytes()
        obj["sha256"] = hashlib.sha256(raw).hexdigest()
        obj["size"] = len(raw)
path.write_text(json.dumps(data, indent=2) + "\n")
PY
"${publish[@]}" --manifest "$origin/manifest.json" > "$work/list.log" 2>&1
grep -F 'publication verified' "$work/list.log" >/dev/null
mapfile -t versions < <(cat "$work/store/$list_key")
[[ ${versions[*]} == 'v0.0.1 v0.0.2' ]] || { printf 'list merge lost a version: %s\n' "${versions[*]}" >&2; exit 1; }
grep -F 'if-match="' "$log" >/dev/null || { echo "list did not send quoted If-Match" >&2; exit 1; }

echo "publisher fixture: PASS"
