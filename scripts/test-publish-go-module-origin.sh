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

# Derive the immutable object and list identities from the manifest under test.
read -r current_version test_version test_module test_target zip_key list_key < <(
python3 - "$origin/manifest.json" <<'PY'
import json
import re
import sys

manifest = json.load(open(sys.argv[1], encoding="utf-8"))
zips = [obj for obj in manifest["objects"] if obj["key"].endswith(".zip")]
zip_obj = next((obj for obj in zips if obj.get("target") == "linux-amd64"), zips[0])
version = zip_obj["version"]
match = re.match(r"^(.*?)([0-9]+)$", version)
test_version = match.group(1) + str(int(match.group(2)) + 1) if match else version + "-test"
print("\t".join([
    version,
    test_version,
    zip_obj["module"],
    zip_obj["target"],
    zip_obj["key"],
    "modules/" + zip_obj["module"] + "/@v/list",
]))
PY
)

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

list_prefix="$work/list-prefix"
mkdir -p "$list_prefix/$(dirname "$list_key")"
list_file="$list_prefix/$list_key"
write_list_manifest() {
  local version=$1 size hash
  printf '%s\n' "$version" > "$list_file"
  size=$(wc -c < "$list_file" | tr -d ' ')
  hash=$(sha256sum "$list_file" | awk '{print $1}')
  cat > "$list_prefix/manifest.json" <<EOF
{
  "version": "$version",
  "module_prefix": "go.lance.org/liblance",
  "base_url": "https://go.lance.org",
  "objects": [
    {
      "key": "$list_key",
      "kind": "list",
      "module": "$test_module",
      "version": "$version",
      "target": "$test_target",
      "sha256": "$hash",
      "size": $size,
      "content_type": "text/plain; charset=utf-8",
      "cache_control": "public, max-age=60",
      "immutable": false
    }
  ]
}
EOF
}

# Forward: a list containing the newer test version is merged with the store's
# current version.
write_list_manifest "$test_version"
"${publish[@]}" --manifest "$list_prefix/manifest.json" > "$work/list-forward.log" 2>&1
grep -F 'publication verified' "$work/list-forward.log" >/dev/null
grep -E "PUT .*@v/list 200 .*if-match=\"" "$log" >/dev/null || {
  echo "list update did not send a quoted If-Match ETag" >&2
  exit 1
}
mapfile -t versions < <(cat "$store/$list_key")
[[ ${versions[*]} == "$current_version $test_version" ]] || {
  printf 'forward list merge: %s\n' "${versions[*]}" >&2
  exit 1
}

# Reverse: a manifest that only mentions the current version must not remove
# the newer test version.
write_list_manifest "$current_version"
"${publish[@]}" --manifest "$list_prefix/manifest.json" > "$work/list-reverse.log" 2>&1
grep -F 'publication verified' "$work/list-reverse.log" >/dev/null
mapfile -t versions < <(cat "$store/$list_key")
[[ ${versions[*]} == "$current_version $test_version" ]] || {
  printf 'reverse list merge lost a version: %s\n' "${versions[*]}" >&2
  exit 1
}

# Replacement: a manifest that only mentions the newer test version must still
# leave the current version present.
write_list_manifest "$test_version"
"${publish[@]}" --manifest "$list_prefix/manifest.json" > "$work/list-replacement.log" 2>&1
grep -F 'publication verified' "$work/list-replacement.log" >/dev/null
mapfile -t versions < <(cat "$store/$list_key")
[[ ${versions[*]} == "$current_version $test_version" ]] || {
  printf 'replacement list merge lost a version: %s\n' "${versions[*]}" >&2
  exit 1
}

echo "publisher repeat/conflict/list-merge: PASS"
