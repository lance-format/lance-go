#!/usr/bin/env bash
# Hosted public-consumer acceptance for one published Unix target.
#
# The checkout supplies the source oracle and harness sources. All consumer
# modules, module caches, build caches, binaries, and SDK caches live in a new
# temporary directory and come from the public Go module proxy.
set -euo pipefail

ROOT_MODULE=${ROOT_MODULE:-github.com/lance-format/lance-go}
ROOT_REVISION=${ROOT_REVISION:-}
SOURCE_COMMIT=${SOURCE_COMMIT:-}
TARGET_GOOS=${TARGET_GOOS:?TARGET_GOOS is required}
TARGET_GOARCH=${TARGET_GOARCH:?TARGET_GOARCH is required}
EXPECTED_UNAME=${EXPECTED_UNAME:-}
LIBRARY_NAME=${LIBRARY_NAME:?LIBRARY_NAME is required}
REPO_DIR=${REPO_DIR:-${GITHUB_WORKSPACE:-$(pwd)}}
REPO_DIR=$(cd "$REPO_DIR" && pwd -P)
EVIDENCE_DIR=${EVIDENCE_DIR:-${RUNNER_TEMP:-/tmp}/public-consumer-evidence}
mkdir -p "$EVIDENCE_DIR"
EVIDENCE_DIR=$(cd "$EVIDENCE_DIR" && pwd -P)
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
VERIFY="$SCRIPT_DIR/verify.py"
NAMESPACE_SOURCE="$SCRIPT_DIR/namespace-consumer/main.go"
WORK=$(mktemp -d "${RUNNER_TEMP:-/tmp}/public-consumer.XXXXXX")
COMMANDS="$EVIDENCE_DIR/commands.log"
SUMMARY="$EVIDENCE_DIR/summary.txt"
CACHE_LIFECYCLE="$EVIDENCE_DIR/cache-lifecycle.txt"
MODULE_CACHE_TSV="$EVIDENCE_DIR/module-cache-inventory.tsv"
ARTIFACT_METADATA_TSV="$EVIDENCE_DIR/artifact-metadata.tsv"
REQUIRED_ARTIFACTS="$EVIDENCE_DIR/required-artifacts.txt"
PUBLIC_GO_ENV=(
  -u GOPRIVATE -u GONOPROXY -u GONOSUMDB -u GONOSUMCHECK -u GOINSECURE -u GOFLAGS
  GOENV=off GOWORK=off GOTOOLCHAIN=local CGO_ENABLED=0
  GOPROXY=https://proxy.golang.org GOSUMDB=sum.golang.org
)
: > "$COMMANDS"
: > "$SUMMARY"
: > "$CACHE_LIFECYCLE"
: > "$MODULE_CACHE_TSV"
: > "$ARTIFACT_METADATA_TSV"
printf 'label\tmodule\ttarget\tlibrary\tcompressed_size\tcompressed_sha256\tuncompressed_size\tuncompressed_sha256\n' >> "$ARTIFACT_METADATA_TSV"

finish() {
  local rc=$?
  set +e
  printf '%s\n' "$rc" > "$EVIDENCE_DIR/exit-code.txt"
  if [[ $rc -ne 0 ]]; then
    printf 'result=FAIL\nexit_code=%s\n' "$rc" >> "$SUMMARY"
  elif ! grep -q '^result=' "$SUMMARY" 2>/dev/null; then
    printf 'result=FAIL\nexit_code=%s\n' "$rc" >> "$SUMMARY"
  fi
  if [[ $rc -ne 0 ]]; then
    printf 'public-consumer acceptance FAILED (exit %s)\n' "$rc" | tee -a "$SUMMARY" >&2
    printf 'work directory kept for diagnosis: %s\n' "$WORK" >> "$SUMMARY"
  fi
}
trap finish EXIT

log() {
  printf '%s\n' "$*" | tee -a "$SUMMARY"
}

record() {
  printf '%s=%s\n' "$1" "$2" >> "$SUMMARY"
}

fail() {
  printf 'ERROR: %s\n' "$*" | tee -a "$SUMMARY" >&2
  exit 1
}

require_file() {
  [[ -f $1 ]] || fail "missing file: $1"
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    fail "need sha256sum or shasum"
  fi
}

assert_file_identity() {
  local path=$1 size=$2 hash=$3 label=$4 actual_size actual_hash
  require_file "$path"
  actual_size=$(wc -c < "$path" | tr -d ' ')
  actual_hash=$(sha256_file "$path")
  [[ $actual_size == "$size" ]] || fail "$label size $actual_size != $size"
  [[ $actual_hash == "$hash" ]] || fail "$label sha256 $actual_hash != $hash"
}

json_value() {
  python3 - "$1" "$2" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    data = json.load(handle)
value = data.get(sys.argv[2], "")
if isinstance(value, (dict, list)):
    print(json.dumps(value, sort_keys=True))
elif value is None:
    print("")
else:
    print(value)
PY
}

json_has_key() {
  python3 - "$1" "$2" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    data = json.load(handle)
print("yes" if sys.argv[2] in data else "no")
PY
}

log_cmd() {
  printf '+ cwd=%q ' "$PWD" >> "$COMMANDS"
  printf '%q ' "$@" >> "$COMMANDS"
  printf '\n' >> "$COMMANDS"
}

run_go() {
  local logfile=$1 modcache=$2 buildcache=$3 rc
  shift 3
  log_cmd env "${PUBLIC_GO_ENV[@]}" GOMODCACHE="$modcache" GOCACHE="$buildcache" go "$@"
  set +e
  env "${PUBLIC_GO_ENV[@]}" GOMODCACHE="$modcache" GOCACHE="$buildcache" go "$@" 2>&1 | tee -a "$logfile"
  rc=${PIPESTATUS[0]}
  set -e
  return "$rc"
}

run_go_capture() {
  local logfile=$1 outfile=$2 modcache=$3 buildcache=$4 rc
  shift 4
  log_cmd env "${PUBLIC_GO_ENV[@]}" GOMODCACHE="$modcache" GOCACHE="$buildcache" go "$@"
  set +e
  env "${PUBLIC_GO_ENV[@]}" GOMODCACHE="$modcache" GOCACHE="$buildcache" go "$@" > "$outfile" 2>> "$logfile"
  rc=$?
  set -e
  cat "$outfile" >> "$logfile"
  return "$rc"
}

run_runtime() {
  local logfile=$1 outfile=$2 rc
  shift 2
  log_cmd "$@"
  # Assertions read only this invocation's stdout, even when evidence is reused.
  set +e
  "$@" > "$outfile" 2>> "$logfile"
  rc=$?
  set -e
  cat "$outfile" | tee -a "$logfile"
  return "$rc"
}

run_python() {
  local logfile=$1 rc
  shift
  log_cmd python3 "$VERIFY" "$@"
  set +e
  python3 "$VERIFY" "$@" 2>&1 | tee -a "$logfile"
  rc=${PIPESTATUS[0]}
  set -e
  return "$rc"
}

go_env_value() {
  env "${PUBLIC_GO_ENV[@]}" go env "$1"
}

snapshot() {
  local label=$1 root="$MODCACHE/cache/download/$ROOT_MODULE"
  [[ -d $root ]] || return 0
  find "$root" -type f \( -name '*.zip' -o -name '*.mod' \) -print | LC_ALL=C sort |
    while IFS= read -r path; do
      local relative module_dir module_path size hash kind version
      relative=${path#"$root"/}
      case "$relative" in
        @v/*)
          module_path=$ROOT_MODULE
          ;;
        */@v/*)
          module_dir=${relative%%/@v/*}
          module_path="$ROOT_MODULE/$module_dir"
          ;;
        *)
          continue
          ;;
      esac
      version=${relative##*/}
      version=${version%.*}
      kind=${relative##*.}
      size=$(wc -c < "$path" | tr -d ' ')
      hash=$(sha256_file "$path")
      printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
        "$label" "$relative" "$module_path" "$kind" "$version" "$size" "$hash" "$path" \
        >> "$MODULE_CACHE_TSV"
    done
}

artifact_zips_for() {
  local prefix=$1
  awk -F'\t' -v p="$prefix" -v r="$ROOT_MODULE/artifacts/" \
    '$1 ~ ("^" p) && index($3, r) == 1 && $4 == "zip" { print $3 "@" $5 }' \
    "$MODULE_CACHE_TSV" | LC_ALL=C sort -u
}

artifact_mods_for() {
  local prefix=$1
  awk -F'\t' -v p="$prefix" -v r="$ROOT_MODULE/artifacts/" \
    '$1 ~ ("^" p) && index($3, r) == 1 && $4 == "mod" { print $3 "@" $5 }' \
    "$MODULE_CACHE_TSV" | LC_ALL=C sort -u
}

root_zips_for() {
  local prefix=$1
  awk -F'\t' -v p="$prefix" -v r="$ROOT_MODULE" \
    '$1 ~ ("^" p) && $3 == r && $4 == "zip" { print $3 "@" $5 }' \
    "$MODULE_CACHE_TSV" | LC_ALL=C sort -u
}

assert_root_zip() {
  local prefix=$1 expected="$ROOT_MODULE@$ROOT_VERSION" got
  got=$(root_zips_for "$prefix")
  [[ $got == "$expected" ]] || fail "expected root zip $expected; found ${got:-<none>}"
  record "${prefix}_root_zip" "$got"
}

assert_only_selected_artifact_zip() {
  local prefix=$1 expected="$SELECTED_ARTIFACT_PATH@$SELECTED_ARTIFACT_VERSION" got
  got=$(artifact_zips_for "$prefix")
  if [[ $got != "$expected" ]]; then
    printf 'artifact payload archives in stage %s:\n%s\n' "$prefix" "${got:-<none>}" | tee -a "$SUMMARY"
    fail "expected exactly one downloaded artifact payload zip: $expected"
  fi
  record "${prefix}_selected_artifact_zip" "$got"
  record "${prefix}_artifact_mod_files" "$(artifact_mods_for "$prefix" | tr '\n' ';')"
}

artifact_field() {
  local dir=$1 name=$2 kind=${3:-string}
  local file="$dir/artifact.go"
  require_file "$file"
  if [[ $kind == int ]]; then
    sed -n "s/^[[:space:]]*${name}[[:space:]]*int64[[:space:]]*=[[:space:]]*\([0-9][0-9]*\).*/\1/p" "$file" | head -n 1
  else
    sed -n "s/^[[:space:]]*${name}[[:space:]]*=[[:space:]]*\"\([^\"]*\)\".*/\1/p" "$file" | head -n 1
  fi
}

verify_artifact_module() {
  local dir=$1 label=$2 path=$3 version=$4 sum=$5
  META_TARGET=$(artifact_field "$dir" Target)
  META_LIBRARY=$(artifact_field "$dir" LibraryName)
  META_COMPRESSED_SIZE=$(artifact_field "$dir" CompressedSize int)
  META_COMPRESSED_SHA=$(artifact_field "$dir" CompressedSHA256)
  META_UNCOMPRESSED_SIZE=$(artifact_field "$dir" UncompressedSize int)
  META_UNCOMPRESSED_SHA=$(artifact_field "$dir" UncompressedSHA256)
  META_UPSTREAM_REPO=$(artifact_field "$dir" UpstreamRepo)
  META_UPSTREAM_COMMIT=$(artifact_field "$dir" UpstreamCommit)
  META_HEADER_SHA=$(artifact_field "$dir" HeaderSHA256)

  [[ $META_TARGET == "$TARGET_GOOS/$TARGET_GOARCH" ]] || fail "$label target is $META_TARGET"
  [[ $META_LIBRARY == "$LIBRARY_NAME" ]] || fail "$label library is $META_LIBRARY"
  [[ $META_COMPRESSED_SIZE =~ ^[0-9]+$ ]] || fail "$label compressed size missing"
  [[ $META_UNCOMPRESSED_SIZE =~ ^[0-9]+$ ]] || fail "$label uncompressed size missing"
  [[ ${#META_COMPRESSED_SHA} -eq 64 ]] || fail "$label compressed sha missing"
  [[ ${#META_UNCOMPRESSED_SHA} -eq 64 ]] || fail "$label uncompressed sha missing"
  [[ $META_UPSTREAM_REPO == "$PIN_REPO" ]] || fail "$label upstream repo is $META_UPSTREAM_REPO"
  [[ $META_UPSTREAM_COMMIT == "$PIN_COMMIT" ]] || fail "$label upstream commit differs from the source pin"
  [[ $META_HEADER_SHA == "$PIN_HEADER" ]] || fail "$label header differs from the source pin"
  [[ -n $sum ]] || fail "$label module sum is empty"

  local gomod="$dir/go.mod" module_path gomod_go
  require_file "$gomod"
  module_path=$(awk '$1 == "module" { print $2; exit }' "$gomod")
  [[ $module_path == "$path" ]] || fail "$label module path is $module_path, want $path"
  gomod_go=$(awk '$1 == "go" { print $2; exit }' "$gomod")
  [[ $gomod_go == 1.23.* ]] || fail "$label go.mod go version is $gomod_go"
  if grep -Eq '^[[:space:]]*(replace|exclude)[[:space:]]' "$gomod"; then
    fail "$label go.mod contains a replace or exclude directive"
  fi

  local payloads="" payload payload_count=0
  for payload in "$dir"/*.zst; do
    if [[ -f $payload ]]; then
      payloads=$payload
      payload_count=$((payload_count + 1))
    fi
  done
  [[ $payload_count -eq 1 ]] || fail "$label payload count is not one"
  META_PAYLOAD=$payloads
  [[ $(basename "$META_PAYLOAD") == "$LIBRARY_NAME.zst" ]] || fail "$label payload name is $(basename "$META_PAYLOAD")"
  assert_file_identity "$META_PAYLOAD" "$META_COMPRESSED_SIZE" "$META_COMPRESSED_SHA" "$label payload"

  local tracked="$REPO_DIR/artifacts/${path##*/}" tracked_file
  for tracked_file in artifact.go go.mod LICENSE "$LIBRARY_NAME.zst"; do
    require_file "$tracked/$tracked_file"
    cmp "$tracked/$tracked_file" "$dir/$tracked_file" ||
      fail "$label published $tracked_file differs from tracked artifacts/${path##*/}/$tracked_file"
  done
  record "${label}_tracked_artifacts_match" yes

  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$label" "$path" "$META_TARGET" "$META_LIBRARY" "$META_COMPRESSED_SIZE" \
    "$META_COMPRESSED_SHA" "$META_UNCOMPRESSED_SIZE" "$META_UNCOMPRESSED_SHA" \
    >> "$ARTIFACT_METADATA_TSV"
  record "${label}_module" "$path@$version"
  record "${label}_sum" "$sum"
  record "${label}_compressed_size" "$META_COMPRESSED_SIZE"
  record "${label}_compressed_sha256" "$META_COMPRESSED_SHA"
  record "${label}_uncompressed_size" "$META_UNCOMPRESSED_SIZE"
  record "${label}_uncompressed_sha256" "$META_UNCOMPRESSED_SHA"
}

download_artifact() {
  local mode=$1 path=$2 version=$3 consumer=$4
  local out="$EVIDENCE_DIR/$mode-artifact-download.json" log="$EVIDENCE_DIR/$mode.log" err
  if ! ( cd "$consumer" && run_go_capture "$log" "$out" "$MODCACHE" "$GOCACHE" \
      mod download -json "$path@$version" ); then
    fail "$mode: artifact module download failed"
  fi
  err=$(json_value "$out" Error)
  [[ -z $err ]] || fail "$mode: artifact module download error: $err"
  ARTIFACT_DIR=$(json_value "$out" Dir)
  ARTIFACT_ZIP=$(json_value "$out" Zip)
  ARTIFACT_SUM=$(json_value "$out" Sum)
  ARTIFACT_GOMODSUM=$(json_value "$out" GoModSum)
  ARTIFACT_VERSION=$(json_value "$out" Version)
  [[ $ARTIFACT_VERSION == "$version" ]] || fail "$mode: artifact version is $ARTIFACT_VERSION"
  [[ -d $ARTIFACT_DIR ]] || fail "$mode: artifact module directory is missing"
  require_file "$ARTIFACT_ZIP"
  record "${mode}_artifact_version" "$ARTIFACT_VERSION"
  record "${mode}_artifact_gomodsum" "$ARTIFACT_GOMODSUM"
  record "${mode}_artifact_zip" "$ARTIFACT_ZIP"
}

verify_payload() {
  local mode=$1 path=$2 version=$3 zip=$4 dir=$5
  local log="$EVIDENCE_DIR/$mode.log"
  if ! run_python "$log" payload \
      --zip "$zip" \
      --module "$path" \
      --version "$version" \
      --name "$LIBRARY_NAME.zst" \
      --artifact-file "$dir/$LIBRARY_NAME.zst" \
      --expect-size "$META_COMPRESSED_SIZE" \
      --expect-sha256 "$META_COMPRESSED_SHA" \
      --out "$EVIDENCE_DIR/$mode-payload.json"; then
    fail "$mode: published payload does not match artifact metadata"
  fi
}

verify_binary() {
  local mode=$1 binary=$2 payload=$3 expect_artifact=$4 expect_payload=$5 expect_tag=$6 expect_sum=$7
  local log="$EVIDENCE_DIR/$mode.log" meta="$EVIDENCE_DIR/$mode-go-version-m.txt"
  if ! run_go_capture "$log" "$meta" "$MODCACHE" "$GOCACHE" version -m "$binary"; then
    fail "$mode: go version -m failed"
  fi
  if ! run_python "$log" binary \
      --binary "$binary" \
      --metadata "$meta" \
      --payload "$payload" \
      --root-module "$ROOT_MODULE" \
      --expect-root-version "$ROOT_VERSION" \
      --expect-root-sum "$ROOT_SUM" \
      --expect-artifact "$expect_artifact" \
      --expect-sum "$expect_sum" \
      --expect-payload "$expect_payload" \
      --expect-tag "$expect_tag" \
      --goos "$TARGET_GOOS" \
      --goarch "$TARGET_GOARCH" \
      --out "$EVIDENCE_DIR/$mode-binary.json"; then
    fail "$mode: binary metadata check failed"
  fi
}

capture_deps() {
  local mode=$1 pkg=$2 tag=$3
  local log="$EVIDENCE_DIR/$mode.log" deps="$EVIDENCE_DIR/$mode-deps.txt"
  if [[ -n $tag ]]; then
    if ! ( cd "$CONSUMER_DIR" && run_go_capture "$log" "$deps" "$MODCACHE" "$GOCACHE" list -deps -tags "$tag" "$pkg" ); then
      fail "$mode: go list -deps failed"
    fi
  else
    if ! ( cd "$CONSUMER_DIR" && run_go_capture "$log" "$deps" "$MODCACHE" "$GOCACHE" list -deps "$pkg" ); then
      fail "$mode: go list -deps failed"
    fi
  fi
}

artifact_deps_from_meta() {
  awk -F'\t' -v r="$ROOT_MODULE/artifacts/" '
    $2 == "dep" && index($3, r) == 1 { print $3 "@" $4 }
  ' "$1" | LC_ALL=C sort -u
}

assert_no_artifact_in_meta() {
  local meta=$1 label=$2 got
  got=$(artifact_deps_from_meta "$meta")
  [[ -z $got ]] || fail "$label binary must not list an artifact dependency; found $got"
  record "${label}_binary_artifact" "<none>"
}

count_artifact_packages() {
  grep -c "^$ROOT_MODULE/artifacts/" "$1" 2>/dev/null || true
}

assert_selected_artifact_package() {
  local deps=$1 label=$2 count
  count=$(count_artifact_packages "$deps")
  [[ $count -eq 1 ]] || fail "$label dependency graph has $count artifact packages, want one"
  grep -Fxq "$SELECTED_ARTIFACT_PATH" "$deps" || fail "$label dependency graph lacks $SELECTED_ARTIFACT_PATH"
  record "${label}_dependency_artifact" "$SELECTED_ARTIFACT_PATH"
}

assert_no_artifact_package() {
  local deps=$1 label=$2
  if grep -q "^$ROOT_MODULE/artifacts/" "$deps"; then
    grep "^$ROOT_MODULE/artifacts/" "$deps" | tee -a "$SUMMARY" >&2
    fail "$label dependency graph contains an artifact package"
  fi
  record "${label}_dependency_artifact" "<none>"
}

assert_root_package() {
  local deps=$1 label=$2
  grep -Fxq "$ROOT_MODULE" "$deps" || fail "$label dependency graph lacks root package"
  record "${label}_root_package" "present"
}

assert_no_root_package() {
  local deps=$1 label=$2
  if grep -Fxq "$ROOT_MODULE" "$deps"; then
    fail "$label dependency graph imports the root package"
  fi
  record "${label}_root_package" "<absent>"
}

check_cache() {
  local mode=$1 expect=$2
  local args=(cache --root "$SDK_CACHE_ROOT" --expect "$expect" --out "$EVIDENCE_DIR/$mode-cache.json")
  if [[ $expect == present ]]; then
    args+=(--sha "$META_UNCOMPRESSED_SHA" --library "$LIBRARY_NAME" --size "$META_UNCOMPRESSED_SIZE")
  fi
  if ! run_python "$EVIDENCE_DIR/$mode.log" "${args[@]}"; then
    fail "$mode: SDK cache check failed"
  fi
}

assert_cache_absent() {
  local stage=$1
  if [[ -e $SDK_CACHE || -L $SDK_CACHE ]]; then
    find "$SDK_CACHE" -print >> "$CACHE_LIFECYCLE" 2>/dev/null || true
    fail "SDK cache unexpectedly present before $stage: $SDK_CACHE"
  fi
  printf '%s: absent (%s)\n' "$stage" "$SDK_CACHE" >> "$CACHE_LIFECYCLE"
}

move_cache_aside() {
  local stage=$1
  local dest="$WORK/state/$stage-sdk-cache"
  [[ -d $SDK_CACHE ]] || fail "expected an SDK cache after $stage"
  mkdir -p "$WORK/state"
  mv "$SDK_CACHE" "$dest" || fail "could not move the SDK cache produced by $stage"
  printf '%s: moved produced cache to %s\n' "$stage" "$dest" >> "$CACHE_LIFECYCLE"
  [[ ! -e $SDK_CACHE && ! -L $SDK_CACHE ]] || fail "SDK cache still exists after moving $stage"
}

prepare_mode() {
  local mode=$1
  MODE_DIR="$WORK/$mode"
  CONSUMER_DIR="$MODE_DIR/consumer"
  MODCACHE="$MODE_DIR/modcache"
  GOCACHE="$MODE_DIR/buildcache"
  mkdir -p "$CONSUMER_DIR" "$MODCACHE" "$GOCACHE"
  [[ -z $(ls -A "$MODCACHE") ]] || fail "$mode: module cache is not fresh"
  [[ -z $(ls -A "$GOCACHE") ]] || fail "$mode: build cache is not fresh"
  if ! ( cd "$CONSUMER_DIR" && run_go "$EVIDENCE_DIR/$mode.log" "$MODCACHE" "$GOCACHE" \
      mod init "example.com/public-consumer-$mode" ); then
    fail "$mode: go mod init failed"
  fi
  [[ -f $CONSUMER_DIR/go.mod ]] || fail "$mode: go.mod is missing"
  record "${mode}_workdir" "$MODE_DIR"
}

go_get_root() {
  local mode=$1 consumer=$2
  if ! ( cd "$consumer" && run_go "$EVIDENCE_DIR/$mode.log" "$MODCACHE" "$GOCACHE" \
      get "$ROOT_MODULE@$ROOT_VERSION" ); then
    fail "$mode: go get $ROOT_MODULE@$ROOT_VERSION failed"
  fi
}

assert_consumer_module() {
  local consumer=$1 label=$2 gomod="$1/go.mod"
  require_file "$gomod"
  if grep -Eq '^[[:space:]]*(replace|exclude)[[:space:]]' "$gomod"; then
    fail "$label go.mod contains replace or exclude"
  fi
  if [[ -n $(find "$WORK" -path '*/modcache' -prune -o -name go.work -print) ]]; then
    fail "$label workspace contains a go.work file"
  fi
}

parse_source_pin() {
  local pin="$REPO_DIR/internal/ffi/upstream.go"
  require_file "$pin"
  PIN_REPO=$(sed -n 's/^[[:space:]]*upstreamRepo[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' "$pin" | head -n 1)
  PIN_COMMIT=$(sed -n 's/^[[:space:]]*upstreamCommit[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' "$pin" | head -n 1)
  PIN_HEADER=$(sed -n 's/^[[:space:]]*upstreamHeaderSHA256[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' "$pin" | head -n 1)
  [[ -n $PIN_REPO ]] || fail "upstream repo pin is missing"
  [[ $PIN_COMMIT =~ ^[0-9a-f]{40}$ ]] || fail "upstream commit pin is not 40 lowercase hex characters"
  [[ $PIN_HEADER =~ ^[0-9a-f]{64}$ ]] || fail "upstream header pin is not 64 lowercase hex characters"
  record source_pin_repo "$PIN_REPO"
  record source_pin_commit "$PIN_COMMIT"
  record source_pin_header_sha256 "$PIN_HEADER"
}

probe_cache_root() {
  local dir="$WORK/cache-probe" out="$EVIDENCE_DIR/cache-root.txt" log="$EVIDENCE_DIR/cache-probe.log" rc
  mkdir -p "$dir" "$dir/modcache" "$dir/buildcache" "$WORK/runtime"
  cat > "$dir/go.mod" <<'EOF'
module public-consumer-cache-probe

go 1.23.0
EOF
  cat > "$dir/main.go" <<'EOF'
package main

import (
	"fmt"
	"os"
)

func main() {
	root, err := os.UserCacheDir()
	if err != nil {
		panic(err)
	}
	fmt.Println(root)
}
EOF
  set +e
  ( cd "$dir" && run_runtime "$log" "$out" env "${PUBLIC_GO_ENV[@]}" XDG_CACHE_HOME="$WORK/runtime" \
      GOMODCACHE="$dir/modcache" GOCACHE="$dir/buildcache" go run . )
  rc=$?
  set -e
  [[ $rc -eq 0 ]] || fail "os.UserCacheDir probe failed"
  SDK_CACHE_ROOT=$(head -n 1 "$out")
  [[ -n $SDK_CACHE_ROOT && $SDK_CACHE_ROOT == /* ]] || fail "os.UserCacheDir probe returned '$SDK_CACHE_ROOT'"
  SDK_CACHE="$SDK_CACHE_ROOT/lance-go"
  case "$TARGET_GOOS" in
    linux) [[ $SDK_CACHE_ROOT == "$WORK/runtime" ]] || fail "Linux ignored XDG_CACHE_HOME: $SDK_CACHE_ROOT" ;;
    darwin) [[ $SDK_CACHE_ROOT == "$HOME/Library/Caches" ]] || fail "macOS cache root is $SDK_CACHE_ROOT" ;;
    *) fail "unsupported target OS $TARGET_GOOS" ;;
  esac
  record cache_probe_command "go run os.UserCacheDir probe"
  record cache_probe_stdout "$SDK_CACHE_ROOT"
  record sdk_cache "$SDK_CACHE"
  record xdg_cache_home "$WORK/runtime"
  record home "$HOME"
}

validate_revision() {
  [[ -n $ROOT_REVISION ]] || fail "ROOT_REVISION is required"
  case "$ROOT_REVISION" in
    ""|-*|*[!A-Za-z0-9._/+~-]*) fail "ROOT_REVISION contains unsupported characters" ;;
  esac
  if [[ $ROOT_REVISION =~ ^[0-9a-f]{40}$ ]]; then
    REVISION_KIND=sha
    [[ $ROOT_REVISION == "$SOURCE_COMMIT" ]] || fail "ROOT_REVISION does not match the checked-out source commit"
  else
    REVISION_KIND=tag
    local tag_commit
    tag_commit=$(git -C "$REPO_DIR" rev-parse "refs/tags/$ROOT_REVISION^{commit}" 2>/dev/null) ||
      fail "ROOT_REVISION must be a full 40-hex SHA or a checked-out tag"
    [[ $tag_commit == "$SOURCE_COMMIT" ]] || fail "ROOT_REVISION tag does not resolve to the checked-out commit"
  fi
  record revision_kind "$REVISION_KIND"
}

assert_version_identity() {
  local suffix prefix tag_commit
  VERSION_COMMIT_PREFIX=""
  if [[ $REVISION_KIND == tag ]]; then
    [[ $ROOT_VERSION == "$ROOT_REVISION" ]] ||
      fail "proxy returned $ROOT_VERSION for requested tag $ROOT_REVISION"
    VERSION_KIND=tag
  else
    case "$ROOT_VERSION" in
      v[0-9]*.[0-9]*.[0-9]*-[0-9]*-*) suffix=${ROOT_VERSION##*-} ;;
      *) suffix="" ;;
    esac
    if [[ $suffix =~ ^[0-9a-f]{12}$ ]]; then
      prefix=${SOURCE_COMMIT:0:12}
      [[ $suffix == "$prefix" ]] || fail "proxy version $ROOT_VERSION does not end in commit prefix $prefix"
      VERSION_KIND=pseudo
      VERSION_COMMIT_PREFIX=$suffix
    else
      VERSION_KIND=tag
    fi
  fi
  record version_kind "$VERSION_KIND"
  if [[ $VERSION_KIND == pseudo ]]; then
    record version_source_commit "$SOURCE_COMMIT"
    record version_identity "pseudo-version prefix matched checked-out source"
    return
  fi
  if [[ $REVISION_KIND == tag ]]; then
    tag_commit=$(git -C "$REPO_DIR" rev-parse "refs/tags/$ROOT_REVISION^{commit}")
  elif git -C "$REPO_DIR" show-ref --verify --quiet "refs/tags/$ROOT_VERSION"; then
    tag_commit=$(git -C "$REPO_DIR" rev-parse "refs/tags/$ROOT_VERSION^{commit}")
  else
    tag_commit=$(git -C "$REPO_DIR" ls-remote origin \
      "refs/tags/$ROOT_VERSION" "refs/tags/$ROOT_VERSION^{}" | awk '{print $1}' | tail -n 1)
    [[ -n $tag_commit ]] || fail "proxy version $ROOT_VERSION is not a tag at origin"
  fi
  [[ $tag_commit == "$SOURCE_COMMIT" ]] || fail "proxy version $ROOT_VERSION resolves to $tag_commit, not source $SOURCE_COMMIT"
  VERSION_COMMIT_PREFIX=$tag_commit
  record version_source_commit "$tag_commit"
}

resolve_root() {
  local out="$EVIDENCE_DIR/root-download.json" log="$EVIDENCE_DIR/default.log" err
  if ! ( cd "$DEFAULT_CONSUMER_DIR" && run_go_capture "$log" "$out" "$DEFAULT_MODCACHE" "$DEFAULT_BUILDCACHE" \
      mod download -json "$ROOT_MODULE@$ROOT_REVISION" ); then
    fail "root module resolution failed for $ROOT_MODULE@$ROOT_REVISION"
  fi
  err=$(json_value "$out" Error)
  [[ -z $err ]] || fail "root module resolution error: $err"
  ROOT_VERSION=$(json_value "$out" Version)
  ROOT_SUM=$(json_value "$out" Sum)
  ROOT_GOMODSUM=$(json_value "$out" GoModSum)
  ROOT_ZIP=$(json_value "$out" Zip)
  ROOT_DIR=$(json_value "$out" Dir)
  ROOT_ORIGIN=$(json_has_key "$out" Origin)
  [[ -n $ROOT_VERSION ]] || fail "root module version is empty"
  [[ -n $ROOT_SUM ]] || fail "root module sum is empty"
  [[ -n $ROOT_GOMODSUM ]] || fail "root module go.mod sum is empty"
  [[ -d $ROOT_DIR ]] || fail "root module directory is missing"
  require_file "$ROOT_ZIP"
  record root_version "$ROOT_VERSION"
  record root_sum "$ROOT_SUM"
  record root_gomodsum "$ROOT_GOMODSUM"
  record root_zip "$ROOT_ZIP"
  record root_zip_size "$(wc -c < "$ROOT_ZIP" | tr -d ' ')"
  record root_zip_sha256 "$(sha256_file "$ROOT_ZIP")"
  record root_dir "$ROOT_DIR"
  record root_origin_present "$ROOT_ORIGIN"
  record root_resolved_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)"

  assert_version_identity
  [[ -n $VERSION_COMMIT_PREFIX ]] || fail "could not identify the proxy-resolved source commit"
  record go_resolved_commit_prefix "$VERSION_COMMIT_PREFIX"
  record source_commit_prefix "${SOURCE_COMMIT:0:12}"
  [[ $VERSION_COMMIT_PREFIX == "$SOURCE_COMMIT" || $VERSION_COMMIT_PREFIX == "${SOURCE_COMMIT:0:12}" ]] ||
    fail "proxy source prefix $VERSION_COMMIT_PREFIX does not match source $SOURCE_COMMIT"

  if ! run_python "$log" archive \
      --repo "$REPO_DIR" \
      --zip "$ROOT_ZIP" \
      --module "$ROOT_MODULE" \
      --version "$ROOT_VERSION" \
      --source-commit "$SOURCE_COMMIT" \
      --nested-module artifacts/linux-amd64 \
      --nested-module artifacts/linux-arm64 \
      --nested-module artifacts/darwin-amd64 \
      --nested-module artifacts/darwin-arm64 \
      --out "$EVIDENCE_DIR/root-archive.json"; then
    fail "root archive does not match the checked-out source tree"
  fi
  require_file "$ROOT_DIR/go.mod"
  require_file "$ROOT_DIR/go.sum"
  cmp "$REPO_DIR/go.mod" "$ROOT_DIR/go.mod" || fail "downloaded root go.mod differs from checkout"
  cmp "$REPO_DIR/go.sum" "$ROOT_DIR/go.sum" || fail "downloaded root go.sum differs from checkout"
  record root_go_mod_matches_checkout yes
  record root_go_sum_matches_checkout yes

  ROOT_GOMOD="$ROOT_DIR/go.mod"
  require_file "$ROOT_GOMOD"
  awk '
    /^require[[:space:]]*\(/ { block = 1; next }
    block && /^\)/ { block = 0; next }
    block && NF >= 2 { print $1 " " $2 }
    !block && /^require[[:space:]]+/ { print $2 " " $3 }
  ' "$ROOT_GOMOD" | grep "^$ROOT_MODULE/artifacts/" | LC_ALL=C sort -u > "$REQUIRED_ARTIFACTS" || true
  [[ -s $REQUIRED_ARTIFACTS ]] || fail "downloaded root module has no artifact requirements"
  record required_artifact_modules "$(tr '\n' ';' < "$REQUIRED_ARTIFACTS")"
}

verify_root_go_mod() {
  local gomod_go
  gomod_go=$(awk '$1 == "go" { print $2; exit }' "$ROOT_GOMOD")
  [[ $gomod_go == 1.23.* ]] || fail "root go.mod go version is $gomod_go"
  if grep -Eq '^[[:space:]]*(replace|exclude)[[:space:]]' "$ROOT_GOMOD"; then
    fail "root go.mod contains a replace or exclude directive"
  fi
  record root_go_version "$gomod_go"
}

assert_selected_required() {
  grep -Fxq "$SELECTED_ARTIFACT_PATH $SELECTED_ARTIFACT_VERSION" "$REQUIRED_ARTIFACTS" ||
    fail "selected artifact $SELECTED_ARTIFACT_PATH@$SELECTED_ARTIFACT_VERSION is not required by the root module"
  record selected_artifact_required yes
}

assert_go_environment() {
  local goversion host_goos host_goarch
  goversion=$(go_env_value GOVERSION)
  host_goos=$(go_env_value GOOS)
  host_goarch=$(go_env_value GOARCH)
  [[ $goversion == go1.23.* ]] || fail "toolchain is $goversion, want go1.23.x"
  [[ $host_goos == "$TARGET_GOOS" ]] || fail "Go host GOOS is $host_goos"
  [[ $host_goarch == "$TARGET_GOARCH" ]] || fail "Go host GOARCH is $host_goarch"
  [[ $(go_env_value CGO_ENABLED) == 0 ]] || fail "CGO_ENABLED is not 0"
  [[ $(go_env_value GOTOOLCHAIN) == local ]] || fail "GOTOOLCHAIN is not local"
  [[ $(go_env_value GOWORK) == off ]] || fail "GOWORK is not off"
  [[ $(go_env_value GOPROXY) == https://proxy.golang.org ]] || fail "GOPROXY is not the public proxy"
  [[ $(go_env_value GOSUMDB) == sum.golang.org ]] || fail "GOSUMDB is not sum.golang.org"
  record go_toolchain "$goversion"
  record go_host "$host_goos/$host_goarch"
  record go_target "$TARGET_GOOS/$TARGET_GOARCH"
  record go_proxy "$(go_env_value GOPROXY)"
  record go_sumdb "$(go_env_value GOSUMDB)"
  record cgo_enabled "$(go_env_value CGO_ENABLED)"
  record gotoolchain "$(go_env_value GOTOOLCHAIN)"
  record gowork "$(go_env_value GOWORK)"
}

# --- setup and source identity ------------------------------------------------

log "public-consumer acceptance for $TARGET_GOOS/$TARGET_GOARCH"
record root_module "$ROOT_MODULE"
record root_revision "$ROOT_REVISION"
record target "$TARGET_GOOS/$TARGET_GOARCH"
record library "$LIBRARY_NAME"
record runner_os "$(uname -s)"
record runner_arch "$(uname -m)"
record runner_kernel "$(uname -srm)"
if [[ -r /etc/os-release ]]; then
  record os_release "$(tr '\n' ';' </etc/os-release)"
fi
if command -v sw_vers >/dev/null 2>&1; then
  record macos_product_version "$(sw_vers -productVersion)"
  record macos_build_version "$(sw_vers -buildVersion)"
fi
if command -v getconf >/dev/null 2>&1; then
  record glibc_version "$(getconf GNU_LIBC_VERSION 2>/dev/null || true)"
fi
record go_version_command "$(go version)"
record github_workflow "${GITHUB_WORKFLOW:-local}"
record github_job "${GITHUB_JOB:-local}"

source_head=$(git -C "$REPO_DIR" rev-parse HEAD)
if [[ -z ${SOURCE_COMMIT:-} ]]; then
  SOURCE_COMMIT=$source_head
fi
record source_commit "$SOURCE_COMMIT"
[[ $SOURCE_COMMIT == "$source_head" ]] || fail "SOURCE_COMMIT $SOURCE_COMMIT != checked-out HEAD $source_head"
case "$TARGET_GOOS" in
  linux) expected_kernel=Linux ;;
  darwin) expected_kernel=Darwin ;;
  *) fail "unsupported target OS $TARGET_GOOS" ;;
esac
[[ $(uname -s) == "$expected_kernel" ]] || fail "uname -s is $(uname -s), want $expected_kernel"
if [[ -n $EXPECTED_UNAME && $(uname -m) != "$EXPECTED_UNAME" ]]; then
  fail "uname -m is $(uname -m), want $EXPECTED_UNAME"
fi

assert_go_environment
validate_revision
parse_source_pin
probe_cache_root
assert_cache_absent initial
record cache_lifecycle "$CACHE_LIFECYCLE"

# --- default consumer ----------------------------------------------------------

prepare_mode default
DEFAULT_CONSUMER_DIR=$CONSUMER_DIR
DEFAULT_MODCACHE=$MODCACHE
DEFAULT_BUILDCACHE=$GOCACHE
DEFAULT_BIN="$MODE_DIR/examples-roundtrip"

resolve_root
snapshot default/root-resolve
verify_root_go_mod

if ! go_get_root default "$DEFAULT_CONSUMER_DIR"; then
  fail "default: go get failed"
fi
assert_consumer_module "$DEFAULT_CONSUMER_DIR" default
snapshot default/go-get

if ! ( cd "$DEFAULT_CONSUMER_DIR" && run_go "$EVIDENCE_DIR/default.log" "$DEFAULT_MODCACHE" "$DEFAULT_BUILDCACHE" \
    build -o "$DEFAULT_BIN" "$ROOT_MODULE/examples/roundtrip" ); then
  fail "default: build failed"
fi
snapshot default/build

default_meta="$EVIDENCE_DIR/default-go-version-m.txt"
if ! run_go_capture "$EVIDENCE_DIR/default.log" "$default_meta" "$DEFAULT_MODCACHE" "$DEFAULT_BUILDCACHE" \
    version -m "$DEFAULT_BIN"; then
  fail "default: go version -m failed"
fi
default_selected=$(artifact_deps_from_meta "$default_meta")
default_count=$(printf '%s\n' "$default_selected" | sed '/^$/d' | wc -l | tr -d ' ')
[[ $default_count -eq 1 ]] || fail "default: expected one artifact dependency, found ${default_selected:-<none>}"
SELECTED_ARTIFACT_PATH=${default_selected%@*}
SELECTED_ARTIFACT_VERSION=${default_selected##*@}
record selected_artifact_module "$SELECTED_ARTIFACT_PATH"
record selected_artifact_version "$SELECTED_ARTIFACT_VERSION"
record selected_artifact_identity "$SELECTED_ARTIFACT_PATH@$SELECTED_ARTIFACT_VERSION"

download_artifact default "$SELECTED_ARTIFACT_PATH" "$SELECTED_ARTIFACT_VERSION" "$DEFAULT_CONSUMER_DIR"
verify_artifact_module "$ARTIFACT_DIR" default "$SELECTED_ARTIFACT_PATH" "$SELECTED_ARTIFACT_VERSION" "$ARTIFACT_SUM"
DEFAULT_ARTIFACT_SUM=$ARTIFACT_SUM
DEFAULT_ARTIFACT_PAYLOAD=$META_PAYLOAD
DEFAULT_META_COMPRESSED_SIZE=$META_COMPRESSED_SIZE
DEFAULT_META_COMPRESSED_SHA=$META_COMPRESSED_SHA
DEFAULT_META_UNCOMPRESSED_SIZE=$META_UNCOMPRESSED_SIZE
DEFAULT_META_UNCOMPRESSED_SHA=$META_UNCOMPRESSED_SHA
verify_payload default "$SELECTED_ARTIFACT_PATH" "$SELECTED_ARTIFACT_VERSION" "$ARTIFACT_ZIP" "$ARTIFACT_DIR"
snapshot default/artifact

capture_deps default "$ROOT_MODULE/examples/roundtrip" ""
snapshot default/deps
assert_root_zip default/
assert_only_selected_artifact_zip default/
assert_selected_artifact_package "$EVIDENCE_DIR/default-deps.txt" default
assert_root_package "$EVIDENCE_DIR/default-deps.txt" default
assert_selected_required
verify_binary default "$DEFAULT_BIN" "$DEFAULT_ARTIFACT_PAYLOAD" \
  "$SELECTED_ARTIFACT_PATH@$SELECTED_ARTIFACT_VERSION" present "" "$DEFAULT_ARTIFACT_SUM"

check_cache default absent
if ! run_runtime "$EVIDENCE_DIR/default.log" "$EVIDENCE_DIR/default-runtime.stdout" env -u LANCE_LIB_PATH XDG_CACHE_HOME="$WORK/runtime" "$DEFAULT_BIN"; then
  fail "default: runtime failed"
fi
grep -Fxq 'Created, reopened and scanned 3 rows: [10 20 30]' "$EVIDENCE_DIR/default-runtime.stdout" ||
  fail "default: runtime output did not show [10 20 30]"
record default_runtime 'Created, reopened and scanned 3 rows: [10 20 30]'
check_cache default present
DEFAULT_LIB="$SDK_CACHE/$META_UNCOMPRESSED_SHA/$LIBRARY_NAME"
require_file "$DEFAULT_LIB"
mkdir -p "$WORK/supplied"
SUPPLIED_LIB="$WORK/supplied/$LIBRARY_NAME"
cp "$DEFAULT_LIB" "$SUPPLIED_LIB"
chmod 0500 "$SUPPLIED_LIB"
assert_file_identity "$SUPPLIED_LIB" "$META_UNCOMPRESSED_SIZE" "$META_UNCOMPRESSED_SHA" "external supplied library"
record external_supplied_library "$SUPPLIED_LIB"
record external_supplied_size "$META_UNCOMPRESSED_SIZE"
record external_supplied_sha256 "$META_UNCOMPRESSED_SHA"
move_cache_aside default
record external_supplied_origin "verified default payload cache $DEFAULT_LIB (moved to $WORK/state/default-sdk-cache)"

# --- namespace-only consumer ---------------------------------------------------

prepare_mode namespace
cp "$NAMESPACE_SOURCE" "$CONSUMER_DIR/main.go"
go_get_root namespace "$CONSUMER_DIR"
assert_consumer_module "$CONSUMER_DIR" namespace
snapshot namespace/go-get
NS_BIN="$MODE_DIR/namespace-consumer"
if ! ( cd "$CONSUMER_DIR" && run_go "$EVIDENCE_DIR/namespace.log" "$MODCACHE" "$GOCACHE" \
    build -o "$NS_BIN" . ); then
  fail "namespace: build failed"
fi
snapshot namespace/build
namespace_meta="$EVIDENCE_DIR/namespace-go-version-m.txt"
if ! run_go_capture "$EVIDENCE_DIR/namespace.log" "$namespace_meta" "$MODCACHE" "$GOCACHE" \
    version -m "$NS_BIN"; then
  fail "namespace: go version -m failed"
fi
namespace_selected=$(artifact_deps_from_meta "$namespace_meta")
namespace_count=$(printf '%s\n' "$namespace_selected" | sed '/^$/d' | wc -l | tr -d ' ')
[[ $namespace_count -eq 1 ]] || fail "namespace: expected one artifact dependency, found ${namespace_selected:-<none>}"
[[ $namespace_selected == "$SELECTED_ARTIFACT_PATH@$SELECTED_ARTIFACT_VERSION" ]] ||
  fail "namespace selected $namespace_selected, default selected $SELECTED_ARTIFACT_PATH@$SELECTED_ARTIFACT_VERSION"

download_artifact namespace "$SELECTED_ARTIFACT_PATH" "$SELECTED_ARTIFACT_VERSION" "$CONSUMER_DIR"
verify_artifact_module "$ARTIFACT_DIR" namespace "$SELECTED_ARTIFACT_PATH" "$SELECTED_ARTIFACT_VERSION" "$ARTIFACT_SUM"
[[ $META_COMPRESSED_SIZE == "$DEFAULT_META_COMPRESSED_SIZE" ]] || fail "namespace payload size differs from default"
[[ $META_COMPRESSED_SHA == "$DEFAULT_META_COMPRESSED_SHA" ]] || fail "namespace payload hash differs from default"
[[ $META_UNCOMPRESSED_SIZE == "$DEFAULT_META_UNCOMPRESSED_SIZE" ]] || fail "namespace library size differs from default"
[[ $META_UNCOMPRESSED_SHA == "$DEFAULT_META_UNCOMPRESSED_SHA" ]] || fail "namespace library hash differs from default"
verify_payload namespace "$SELECTED_ARTIFACT_PATH" "$SELECTED_ARTIFACT_VERSION" "$ARTIFACT_ZIP" "$ARTIFACT_DIR"
snapshot namespace/artifact

capture_deps namespace . ""
snapshot namespace/deps
assert_root_zip namespace/
assert_only_selected_artifact_zip namespace/
assert_selected_artifact_package "$EVIDENCE_DIR/namespace-deps.txt" namespace
assert_no_root_package "$EVIDENCE_DIR/namespace-deps.txt" namespace
grep -Fxq "$ROOT_MODULE/namespace" "$EVIDENCE_DIR/namespace-deps.txt" ||
  fail "namespace: dependency graph lacks $ROOT_MODULE/namespace"
record namespace_package present
verify_binary namespace "$NS_BIN" "$DEFAULT_ARTIFACT_PAYLOAD" \
  "$SELECTED_ARTIFACT_PATH@$SELECTED_ARTIFACT_VERSION" present "" "$ARTIFACT_SUM"

check_cache namespace absent
NS_ROOT="$MODE_DIR/namespace-root"
mkdir -p "$NS_ROOT"
record namespace_root "$NS_ROOT"
if ! run_runtime "$EVIDENCE_DIR/namespace.log" "$EVIDENCE_DIR/namespace-runtime.stdout" env -u LANCE_LIB_PATH XDG_CACHE_HOME="$WORK/runtime" \
    "$NS_BIN" -root "$NS_ROOT"; then
  fail "namespace: runtime failed"
fi
grep -Eq '^namespace id=".+" namespaces=0$' "$EVIDENCE_DIR/namespace-runtime.stdout" ||
  fail "namespace: runtime did not report an id and zero namespaces"
record namespace_runtime 'namespace id=<nonempty> namespaces=0'
check_cache namespace present
move_cache_aside namespace

# --- external consumer ---------------------------------------------------------

prepare_mode external
go_get_root external "$CONSUMER_DIR"
assert_consumer_module "$CONSUMER_DIR" external
snapshot external/go-get
EXT_BIN="$MODE_DIR/examples-roundtrip-external"
if ! ( cd "$CONSUMER_DIR" && run_go "$EVIDENCE_DIR/external.log" "$MODCACHE" "$GOCACHE" \
    build -tags lance_external_lib -o "$EXT_BIN" "$ROOT_MODULE/examples/roundtrip" ); then
  fail "external: build failed"
fi
snapshot external/build
external_meta="$EVIDENCE_DIR/external-go-version-m.txt"
if ! run_go_capture "$EVIDENCE_DIR/external.log" "$external_meta" "$MODCACHE" "$GOCACHE" \
    version -m "$EXT_BIN"; then
  fail "external: go version -m failed"
fi
assert_no_artifact_in_meta "$external_meta" external
verify_binary external "$EXT_BIN" "$DEFAULT_ARTIFACT_PAYLOAD" none absent lance_external_lib ""
capture_deps external "$ROOT_MODULE/examples/roundtrip" lance_external_lib
snapshot external/deps
assert_root_zip external/
assert_no_artifact_package "$EVIDENCE_DIR/external-deps.txt" external
assert_root_package "$EVIDENCE_DIR/external-deps.txt" external
record external_artifact_payload_zips "$(artifact_zips_for external/ | tr '\n' ';')"
record external_artifact_mod_files "$(artifact_mods_for external/ | tr '\n' ';')"
check_cache external absent
if ! run_runtime "$EVIDENCE_DIR/external.log" "$EVIDENCE_DIR/external-runtime.stdout" env LANCE_LIB_PATH="$SUPPLIED_LIB" XDG_CACHE_HOME="$WORK/runtime" "$EXT_BIN"; then
  fail "external: runtime failed"
fi
grep -Fxq 'Created, reopened and scanned 3 rows: [10 20 30]' "$EVIDENCE_DIR/external-runtime.stdout" ||
  fail "external: runtime output did not show [10 20 30]"
record external_runtime 'Created, reopened and scanned 3 rows: [10 20 30]'
check_cache external absent
snapshot external/run

rm -rf "$WORK"
record result PASS
record completed_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
log "public-consumer acceptance PASS: target=$TARGET_GOOS/$TARGET_GOARCH root=$ROOT_MODULE@$ROOT_VERSION artifact=$SELECTED_ARTIFACT_PATH@$SELECTED_ARTIFACT_VERSION"
