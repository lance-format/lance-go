#!/usr/bin/env bash
# Verify the version-pinned README quickstart and its read-only deployment path.
#
# This runs on linux/amd64. It resolves the published Go modules from the public
# proxy, never builds Rust or lance-c, and never uses a preinstalled native
# library or an existing dataset.
set -euo pipefail

VERSION=${LANCE_GO_QUICKSTART_VERSION:-v0.1.0}
ARTIFACT_MODULE=github.com/lance-format/lance-go/artifacts/linux-amd64
ARTIFACT_VERSION=${LANCE_GO_QUICKSTART_ARTIFACT_VERSION:-v0.1.0}
COMPANION_BYTES=87415140
LIBRARY_BYTES=342712936
LIBRARY_SHA256=4617c425e7e7660ef6d0d77f2e14dba48359400d78261b0072cd46e48e87e2ac
EXPECTED_OUTPUT='Created, reopened and scanned 3 rows: [10 20 30]'

if [[ "$(go env GOOS)/$(go env GOARCH)" != linux/amd64 ]]; then
  echo "verify-quickstart: this check requires linux/amd64" >&2
  exit 1
fi
for tool in go zstd sha256sum; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "verify-quickstart: $tool is required" >&2
    exit 1
  fi
done

work=$(mktemp -d "${TMPDIR:-/tmp}/lance-go-quickstart.XXXXXX")
cleanup() {
  chmod -R u+rwX "$work" 2>/dev/null || true
  rm -rf "$work"
}
trap cleanup EXIT

run_dir="$work/run"
modcache="$work/modcache"
buildcache="$work/buildcache"
runtime_cache="$work/runtime-cache"
deploy_dir="$work/deploy"
readonly_root="$work/readonly"
mkdir -p "$run_dir" "$modcache" "$buildcache" "$runtime_cache" "$deploy_dir"

common_env=(
  -u LANCE_LIB_PATH -u GOPRIVATE -u GONOPROXY -u GONOSUMDB -u GONOSUMCHECK
  -u GOINSECURE -u GOFLAGS
  GOENV=off GOWORK=off GOTOOLCHAIN=local CGO_ENABLED=0 GOTELEMETRY=off
  GOPROXY=https://proxy.golang.org GOSUMDB=sum.golang.org
  GOMODCACHE="$modcache" GOCACHE="$buildcache"
)

run_quickstart() {
  local stdout=$1 stderr=$2
  shift 2
  (
    cd "$run_dir"
    env "${common_env[@]}" "$@" \
      go run "github.com/lance-format/lance-go/examples/roundtrip@$VERSION"
  ) >"$stdout" 2>"$stderr"
}

assert_quickstart_output() {
  local stdout=$1
  printf '%s\n' "$EXPECTED_OUTPUT" >"$work/expected.out"
  diff -u "$work/expected.out" "$stdout"
}

assert_library() {
  local path=$1 label=$2 size hash
  size=$(wc -c <"$path" | tr -d ' ')
  hash=$(sha256sum "$path" | awk '{print $1}')
  [[ "$size" == "$LIBRARY_BYTES" ]] || {
    echo "verify-quickstart: $label size $size != $LIBRARY_BYTES" >&2
    exit 1
  }
  [[ "$hash" == "$LIBRARY_SHA256" ]] || {
    echo "verify-quickstart: $label sha256 $hash != $LIBRARY_SHA256" >&2
    exit 1
  }
}

# The quickstart must work from an empty directory with no inherited library.
if ! run_quickstart "$work/quickstart.out" "$work/quickstart.err" \
  XDG_CACHE_HOME="$runtime_cache"; then
  cat "$work/quickstart.err" >&2
  exit 1
fi
assert_quickstart_output "$work/quickstart.out"

mapfile -t companion_archives < <(
  find "$modcache/cache/download/github.com/lance-format/lance-go/artifacts" \
    -type f -name '*.zip' -print | LC_ALL=C sort
)
[[ ${#companion_archives[@]} -eq 1 ]] || {
  printf 'verify-quickstart: expected one companion archive, got %s\n' \
    "${#companion_archives[@]}" >&2
  printf '%s\n' "${companion_archives[@]}" >&2
  exit 1
}
[[ ${companion_archives[0]} == */linux-amd64/@v/* ]] || {
  echo "verify-quickstart: selected archive is not linux/amd64: ${companion_archives[0]}" >&2
  exit 1
}
companion_size=$(wc -c <"${companion_archives[0]}" | tr -d ' ')
[[ "$companion_size" == "$COMPANION_BYTES" ]] || {
  echo "verify-quickstart: companion size $companion_size != $COMPANION_BYTES" >&2
  exit 1
}

mapfile -t cached_libraries < <(
  find "$runtime_cache/lance-go" -type f -name liblance_c.so -print
)
[[ ${#cached_libraries[@]} -eq 1 ]] || {
  echo "verify-quickstart: expected one unpacked library, got ${#cached_libraries[@]}" >&2
  exit 1
}
assert_library "${cached_libraries[0]}" "unpacked library"

# Prepare the exact companion library and run with a cache root that cannot be
# written. LANCE_LIB_PATH must bypass extraction and leave the cache absent.
env "${common_env[@]}" go mod download "$ARTIFACT_MODULE@$ARTIFACT_VERSION"
payload="$modcache/$ARTIFACT_MODULE@$ARTIFACT_VERSION/liblance_c.so.zst"
[[ -f "$payload" ]] || {
  echo "verify-quickstart: missing companion payload $payload" >&2
  exit 1
}
zstd -q -d "$payload" -o "$deploy_dir/liblance_c.so"
chmod 0555 "$deploy_dir/liblance_c.so"
assert_library "$deploy_dir/liblance_c.so" "deployed library"

mkdir "$readonly_root"
chmod 0555 "$readonly_root"
if ! run_quickstart "$work/readonly.out" "$work/readonly.err" \
  XDG_CACHE_HOME="$readonly_root/cache" \
  LANCE_LIB_PATH="$deploy_dir/liblance_c.so"; then
  cat "$work/readonly.err" >&2
  exit 1
fi
assert_quickstart_output "$work/readonly.out"
[[ ! -e "$readonly_root/cache" ]] || {
  echo "verify-quickstart: read-only run wrote $readonly_root/cache" >&2
  exit 1
}

printf 'quickstart_version=%s\n' "$VERSION"
printf 'quickstart_output=%s\n' "$EXPECTED_OUTPUT"
printf 'companion_archives=1\n'
printf 'companion_bytes=%s\n' "$companion_size"
printf 'unpacked_library_bytes=%s\n' "$LIBRARY_BYTES"
printf 'unpacked_library_sha256=%s\n' "$LIBRARY_SHA256"
printf 'readonly_library_sha256=%s\n' "$LIBRARY_SHA256"
printf 'readonly_cache_absent=true\n'
printf 'verify_quickstart=ok\n'
