#!/usr/bin/env bash
# Exercise the publisher against a local S3 mock: repeat, conflict, no-overwrite,
# and @v/list merge under normal/reversed publication order.
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
origin=${ORIGIN_DIR:?ORIGIN_DIR must point at a generated origin tree}
work=$(mktemp -d)
server_pid=""
cleanup() {
  [[ -n ${server_pid:-} ]] && kill "$server_pid" 2>/dev/null || true
  chmod -R u+w "$work" 2>/dev/null || true
  rm -rf "$work"
}
trap cleanup EXIT

store="$work/store"
log="$work/requests.log"
python3 "$repo_root/scripts/go-module-origin/mock-s3.py" \
  --root "$store" --port 18790 --log "$log" > "$work/server.log" 2>&1 &
server_pid=$!
sleep 1

export AWS_NO_SIGN_REQUEST=1
export AWS_DEFAULT_REGION=auto
export AWS_EC2_METADATA_DISABLED=true
publish=( "$repo_root/scripts/publish-go-module-origin.sh" --bucket test-bucket --endpoint http://127.0.0.1:18790 )

"${publish[@]}" --manifest "$origin/manifest.json" > "$work/first.log" 2>&1
grep -F 'publication verified' "$work/first.log" >/dev/null
first_puts=$(grep -c '^PUT ' "$log")
[[ $first_puts -eq 25 ]] || { echo "first publish PUT count $first_puts, want 25" >&2; exit 1; }

"${publish[@]}" --manifest "$origin/manifest.json" > "$work/repeat.log" 2>&1
grep -F 'publication verified' "$work/repeat.log" >/dev/null
second_puts=$(grep -c '^PUT ' "$log")
[[ $second_puts -eq $first_puts ]] || { echo "repeat publish added PUTs: $second_puts != $first_puts" >&2; exit 1; }

# Conflict: mutate an immutable object in the store. The publisher must abort
# and must not overwrite it.
zip_key="modules/go.lance.org/liblance/linux-amd64/@v/v0.2.0-preview.1.zip"
zip_path="$store/$zip_key"
printf 'conflict bytes\n' > "$zip_path"
before_put=$(grep -c '^PUT ' "$log")
set +e
"${publish[@]}" --manifest "$origin/manifest.json" > "$work/conflict.log" 2>&1
conflict_rc=$?
set -e
[[ $conflict_rc -ne 0 ]] || { echo "publisher accepted an immutable conflict" >&2; exit 1; }
grep -F 'immutable object conflict' "$work/conflict.log" >/dev/null
[[ $(cat "$zip_path") == 'conflict bytes' ]] || { echo "publisher overwrote conflicting bytes" >&2; exit 1; }
after_put=$(grep -c '^PUT ' "$log")
[[ $after_put -eq $before_put ]] || { echo "conflict publish wrote objects" >&2; exit 1; }

# Restore the immutable object for the list-only merge test.
cp "$origin/$zip_key" "$zip_path"

list_key="modules/go.lance.org/liblance/linux-amd64/@v/list"
list_prefix="$work/list-prefix"
mkdir -p "$list_prefix/modules/go.lance.org/liblance/linux-amd64/@v"
printf 'v0.2.0-preview.2\n' > "$list_prefix/$list_key"
list_size=$(wc -c < "$list_prefix/$list_key" | tr -d ' ')
list_hash=$(sha256sum "$list_prefix/$list_key" | awk '{print $1}')
cat > "$list_prefix/manifest.json" <<EOF
{
  "version": "v0.2.0-preview.2",
  "module_prefix": "go.lance.org/liblance",
  "base_url": "https://go.lance.org",
  "objects": [
    {
      "key": "$list_key",
      "kind": "list",
      "module": "go.lance.org/liblance/linux-amd64",
      "version": "v0.2.0-preview.2",
      "target": "linux-amd64",
      "sha256": "$list_hash",
      "size": $list_size,
      "content_type": "text/plain; charset=utf-8",
      "cache_control": "public, max-age=60",
      "immutable": false
    }
  ]
}
EOF
"${publish[@]}" --manifest "$list_prefix/manifest.json" > "$work/list-forward.log" 2>&1
grep -F 'publication verified' "$work/list-forward.log" >/dev/null
grep -E "PUT .*@v/list 200 .*if-match=\"" "$log" >/dev/null || {
  echo "list update did not send a quoted If-Match ETag" >&2
  exit 1
}
mapfile -t versions < <(cat "$store/$list_key")
[[ ${versions[*]} == 'v0.2.0-preview.1 v0.2.0-preview.2' ]] || {
  printf 'forward list merge: %s\n' "${versions[*]}" >&2
  exit 1
}

# Reversed order: a later manifest that only mentions the older version must
# not remove the newer one.
printf 'v0.2.0-preview.1\n' > "$list_prefix/$list_key"
list_size=$(wc -c < "$list_prefix/$list_key" | tr -d ' ')
list_hash=$(sha256sum "$list_prefix/$list_key" | awk '{print $1}')
python3 - "$list_prefix/manifest.json" "$list_hash" "$list_size" <<'PY'
import json
import pathlib
import sys
path = pathlib.Path(sys.argv[1])
data = json.loads(path.read_text())
data["version"] = "v0.2.0-preview.1"
data["objects"][0]["version"] = "v0.2.0-preview.1"
data["objects"][0]["sha256"] = sys.argv[2]
data["objects"][0]["size"] = int(sys.argv[3])
path.write_text(json.dumps(data, indent=2) + "\n")
PY
"${publish[@]}" --manifest "$list_prefix/manifest.json" > "$work/list-reverse.log" 2>&1
grep -F 'publication verified' "$work/list-reverse.log" >/dev/null
mapfile -t versions < <(cat "$store/$list_key")
[[ ${versions[*]} == 'v0.2.0-preview.1 v0.2.0-preview.2' ]] || {
  printf 'reverse list merge lost a version: %s\n' "${versions[*]}" >&2
  exit 1
}

# Replacement update: a later manifest that mentions only the newer version
# must still leave both versions present.
printf 'v0.2.0-preview.2\n' > "$list_prefix/$list_key"
list_size=$(wc -c < "$list_prefix/$list_key" | tr -d ' ')
list_hash=$(sha256sum "$list_prefix/$list_key" | awk '{print $1}')
python3 - "$list_prefix/manifest.json" "$list_hash" "$list_size" <<'PY'
import json
import pathlib
import sys
path = pathlib.Path(sys.argv[1])
data = json.loads(path.read_text())
data["version"] = "v0.2.0-preview.2"
data["objects"][0]["version"] = "v0.2.0-preview.2"
data["objects"][0]["sha256"] = sys.argv[2]
data["objects"][0]["size"] = int(sys.argv[3])
path.write_text(json.dumps(data, indent=2) + "\n")
PY
"${publish[@]}" --manifest "$list_prefix/manifest.json" > "$work/list-replacement.log" 2>&1
grep -F 'publication verified' "$work/list-replacement.log" >/dev/null
mapfile -t versions < <(cat "$store/$list_key")
[[ ${versions[*]} == 'v0.2.0-preview.1 v0.2.0-preview.2' ]] || {
  printf 'replacement list merge lost a version: %s\n' "${versions[*]}" >&2
  exit 1
}

echo "publisher repeat/conflict/list-merge: PASS"
