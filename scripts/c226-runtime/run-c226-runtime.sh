#!/usr/bin/env bash

set -u
set -o pipefail

if [[ $# -ne 6 ]]; then
	printf 'usage: %s SOURCE_ROOT EVIDENCE_DIR NEW_LIB NEW_HEADER NEW_HPP LEGACY_LIB\n' "$0" >&2
	exit 64
fi

SOURCE_ROOT=$1
EVIDENCE_DIR=$2
NEW_LIB=$3
NEW_HEADER=$4
NEW_HPP=$5
LEGACY_LIB=$6

GO_BIN=/home/xuanwo/Code/tools/go/bin/go
GOFMT_BIN=/home/xuanwo/Code/tools/go/bin/gofmt
PARSER="$(cd "$(dirname "$0")" && pwd)/summarize_go_test_json.py"

SOURCE_COMMIT=85de278e6d342495f5fb0da5b35dcabc7532dfb0
SOURCE_TREE=47e2c4a9c0cb29f88b65a0f2737cadead7f5fe25
BASE_COMMIT=2aefe5085991374748ec5c688e617864f615ec65
BASE_TREE=823947864f523de758c11d229c6f058aaee1e8d4
REMOTE_BRANCH=refs/heads/xuanwo/open-base-store-params
EXPECTED_GO_VERSION='go version go1.23.12 linux/amd64'

NEW_LIB_SHA=9cd519b3c539042f95da444c87d5278e1af87c64aaefc229ab71cb371845dbb5
NEW_LIB_SIZE=2441371928
NEW_HEADER_SHA=9091736570e0f85897f4d86f76b3a7463720321880a3f85802e81f4ba53ab564
NEW_HPP_SHA=90f7d1599bb8f8ad7580d20c8a5c7d0430ed4ad37be44b2efe08aba4685dad51
LEGACY_LIB_SHA=e5571d6da16e81a0434f2509ade5262785b63df0b37ff7155537642841dd9378

CHANGED_FILES=(
	options.go
	dataset_base_store_params.go
	dataset_write_native.go
	dataset.go
	namespace.go
	internal/ffi/bindings.go
	internal/ffi/dataset_new.go
	internal/ffi/dataset_new_base_store_test.go
	dataset_open_base_store_internal_test.go
	dataset_open_base_store_test.go
)

FFI_SELECTOR='^(TestOpenDatasetWithBaseStoreParamsPreservesNilAndEmpty|TestOpenWithoutBaseStoreParamsDoesNotRequireOptionalSymbol|TestOpenDatasetWithBaseStoreParamsRequiresOptionalSymbol|TestOpenDatasetWithBaseStoreParamsMarshalsExactKeysAndScalars|TestOpenDatasetWithBaseStoreParamsForwardsSessionAndAccessor|TestOpenDatasetWithBaseStoreParamsCapturesNativeError|TestOpenDatasetWithBaseStoreParamsRejectsUnsupportedRoutes)$'
FFI_TESTS=(
	TestOpenDatasetWithBaseStoreParamsPreservesNilAndEmpty
	TestOpenWithoutBaseStoreParamsDoesNotRequireOptionalSymbol
	TestOpenDatasetWithBaseStoreParamsRequiresOptionalSymbol
	TestOpenDatasetWithBaseStoreParamsMarshalsExactKeysAndScalars
	TestOpenDatasetWithBaseStoreParamsForwardsSessionAndAccessor
	TestOpenDatasetWithBaseStoreParamsCapturesNativeError
	TestOpenDatasetWithBaseStoreParamsRejectsUnsupportedRoutes
)

OLD_SELECTOR='^(TestOpenWithBaseStoreParamsPreservesNilAndEmpty|TestOpenWithBaseStoreParamsValidatesAndOwnsAccessors|TestOpenWithoutBaseStoreParamsDoesNotRequireOptionalSymbol|TestOpenWithBaseStoreParamsRequiresOptionalSymbol|TestOpenWithBaseStoreParamsRejectsCommitLock|TestOpenFromNamespaceRejectsBaseStoreParams|TestNativeWriteOptionsBaseStoreParams|TestNativeWriteOptionsBasePlacement)$'
OLD_TESTS=(
	TestOpenWithBaseStoreParamsPreservesNilAndEmpty
	TestOpenWithBaseStoreParamsValidatesAndOwnsAccessors
	TestOpenWithoutBaseStoreParamsDoesNotRequireOptionalSymbol
	TestOpenWithBaseStoreParamsRequiresOptionalSymbol
	TestOpenWithBaseStoreParamsRejectsCommitLock
	TestOpenFromNamespaceRejectsBaseStoreParams
	TestNativeWriteOptionsBaseStoreParams
	TestNativeWriteOptionsBasePlacement
)

NEW_SELECTOR='^(TestOpenWithBaseStoreParamsPreservesNilAndEmpty|TestOpenWithBaseStoreParamsValidatesAndOwnsAccessors|TestOpenWithBaseStoreParamsBridgesStaticOptionsPresence|TestOpenWithoutBaseStoreParamsDoesNotRequireOptionalSymbol|TestOpenWithBaseStoreParamsRequiresOptionalSymbol|TestOpenWithBaseStoreParamsRejectsCommitLock|TestOpenFromNamespaceRejectsBaseStoreParams|TestOpenWithBaseStoreParamsReopenExactPaths)$'
NEW_TESTS=(
	TestOpenWithBaseStoreParamsPreservesNilAndEmpty
	TestOpenWithBaseStoreParamsValidatesAndOwnsAccessors
	TestOpenWithBaseStoreParamsBridgesStaticOptionsPresence
	TestOpenWithoutBaseStoreParamsDoesNotRequireOptionalSymbol
	TestOpenWithBaseStoreParamsRequiresOptionalSymbol
	TestOpenWithBaseStoreParamsRejectsCommitLock
	TestOpenFromNamespaceRejectsBaseStoreParams
	TestOpenWithBaseStoreParamsReopenExactPaths
)

fail() {
	printf 'preflight failed: %s\n' "$*" >&2
	printf '%s\n' "$*" >"$EVIDENCE_DIR/preflight-error.txt"
	printf '2\n' >"$EVIDENCE_DIR/runner-exit-code.txt"
	exit 2
}

sha256_of() {
	sha256sum -- "$1" | awk '{print $1}'
}

is_regular_file() {
	[[ -f "$1" && ! -L "$1" ]]
}

run_go() {
	local library=$1
	shift
	env \
		-u GOFLAGS \
		-u GO111MODULE \
		-u GOWORK \
		-u LANCE_C_HEADER \
		-u LD_LIBRARY_PATH \
		-u DYLD_LIBRARY_PATH \
		CGO_ENABLED=0 \
		GOENV=off \
		GOTOOLCHAIN=local \
		LANCE_LIB_PATH="$library" \
		GOMAXPROCS=2 \
		"$GO_BIN" "$@"
}

write_source_snapshot() {
	local phase=$1
	local identity="$EVIDENCE_DIR/source-identity-$phase.txt"
	local status="$EVIDENCE_DIR/git-status-$phase.txt"
	local manifest="$EVIDENCE_DIR/source-manifest-$phase.txt"
	local diff_status="$EVIDENCE_DIR/git-diff-exit-$phase.txt"
	local cached_status="$EVIDENCE_DIR/git-diff-cached-exit-$phase.txt"

	git -C "$SOURCE_ROOT" show -s \
		--format='commit %H%ntree %T%nparent %P%nauthor %an <%ae> %ad%ncommitter %cn <%ce> %cd%nsubject %s' \
		--date=iso-strict HEAD >"$identity" || return 1
	git -C "$SOURCE_ROOT" status --porcelain=v1 --untracked-files=all >"$status" || return 1
	git -C "$SOURCE_ROOT" diff --exit-code HEAD -- >"$EVIDENCE_DIR/git-diff-$phase.txt" 2>&1
	printf '%s\n' "$?" >"$diff_status" || return 1
	git -C "$SOURCE_ROOT" diff --cached --exit-code HEAD -- >"$EVIDENCE_DIR/git-diff-cached-$phase.txt" 2>&1
	printf '%s\n' "$?" >"$cached_status" || return 1
	{
		cat "$identity" || return 1
		printf '%s\n' '--- tree ---'
		git -C "$SOURCE_ROOT" ls-tree -r --full-tree HEAD || return 1
		printf '%s\n' '--- index ---'
		git -C "$SOURCE_ROOT" ls-files -s || return 1
		printf '%s\n' '--- status ---'
		cat "$status" || return 1
	} >"$manifest" || return 1
	sha256_of "$manifest" >"$EVIDENCE_DIR/source-sha256-$phase.txt"
}

write_command_line() {
	local output=$1
	local label=$2
	local library=$3
	local package=$4
	local selector=$5
	printf 'lane=%s\nsource_root=%s\nlibrary=%s\npackage=%s\nselector=%s\nargv=cd %q && env -u GOFLAGS -u GO111MODULE -u GOWORK -u LANCE_C_HEADER -u LD_LIBRARY_PATH -u DYLD_LIBRARY_PATH CGO_ENABLED=0 GOENV=off GOTOOLCHAIN=local LANCE_LIB_PATH=%q GOMAXPROCS=2 %q test -json -count=1 -p=1 -vet=off -mod=readonly -timeout=10m -tags=lance_external_lib -run %q %s\n' \
		"$label" "$SOURCE_ROOT" "$library" "$package" "$selector" \
		"$SOURCE_ROOT" "$library" "$GO_BIN" "$selector" "$package" >"$output"
}

run_lane() {
	local label=$1
	local library=$2
	local package=$3
	local selector=$4
	shift 4
	local expected=("$@")
	local lane_dir="$EVIDENCE_DIR/lanes/$label"
	local status=0
	local parse_status=0

	mkdir -p "$lane_dir" || return 70
	write_command_line "$lane_dir/command.txt" "$label" "$library" "$package" "$selector" ||
		return 70
	printf '%s\n' "${expected[@]}" >"$lane_dir/expected-tests.txt"
	printf '%s\n' "$selector" >"$lane_dir/selector.txt"
	printf '%s\n' "$(date -u +%FT%TZ)" >"$lane_dir/started-at-utc.txt"
	stat -c '%n|%d|%i|%s|%y|%z' -- "$library" >"$lane_dir/library-stat-before.txt" ||
		return 70
	sha256_of "$library" >"$lane_dir/library-sha256-before.txt" || return 70

	(
		cd "$SOURCE_ROOT" || exit 70
		run_go "$library" test \
			-json \
			-count=1 \
			-p=1 \
			-vet=off \
			-mod=readonly \
			-timeout=10m \
			-tags=lance_external_lib \
			-run "$selector" \
			"$package"
	) >"$lane_dir/go-test.json" 2>"$lane_dir/stderr.txt"
	status=$?
	printf '%s\n' "$status" >"$lane_dir/go-test-exit-code.txt"
	printf '%s\n' "$(date -u +%FT%TZ)" >"$lane_dir/finished-at-utc.txt"
	sha256_of "$library" >"$lane_dir/library-sha256-after.txt" || return 70
	stat -c '%n|%d|%i|%s|%y|%z' -- "$library" >"$lane_dir/library-stat-after.txt" ||
		return 70

	python3 "$PARSER" \
		--label "$label" \
		--json "$lane_dir/go-test.json" \
		--out "$lane_dir/summary.json" \
		--expected "${expected[@]}" \
		>"$lane_dir/summary.txt" 2>"$lane_dir/parser.stderr.txt"
	parse_status=$?
	printf '%s\n' "$parse_status" >"$lane_dir/parser-exit-code.txt"
	cat "$lane_dir/summary.txt"
	if [[ "$status" -ne 0 ]]; then
		printf '%s: go test exit=%s\n' "$label" "$status" >&2
	fi
	if [[ "$parse_status" -ne 0 ]]; then
		printf '%s: summary check failed; see %s\n' "$label" "$lane_dir/parser.stderr.txt" >&2
	fi
	if [[ "$status" -ne 0 || "$parse_status" -ne 0 ]]; then
		return 1
	fi
	return 0
}

for path in "$SOURCE_ROOT" "$EVIDENCE_DIR" "$NEW_LIB" "$NEW_HEADER" "$NEW_HPP" "$LEGACY_LIB"; do
	[[ "$path" == /* ]] || {
		printf 'path must be absolute: %s\n' "$path" >&2
		exit 64
	}
done
[[ "$EVIDENCE_DIR" != / ]] || {
	printf 'evidence directory must not be /\n' >&2
	exit 64
}
case "$EVIDENCE_DIR/" in
"$SOURCE_ROOT/"*)
	printf 'evidence directory must be outside the source checkout: %s\n' "$EVIDENCE_DIR" >&2
	exit 64
	;;
esac
[[ ! -e "$EVIDENCE_DIR" ]] || {
	printf 'evidence directory already exists: %s\n' "$EVIDENCE_DIR" >&2
	exit 65
}

umask 022
mkdir -p "$EVIDENCE_DIR/lanes" || exit 70

[[ -d "$SOURCE_ROOT" ]] || fail "source checkout is missing: $SOURCE_ROOT"
[[ -x "$GO_BIN" ]] || fail "Go tool is not executable: $GO_BIN"
[[ -x "$GOFMT_BIN" ]] || fail "gofmt tool is not executable: $GOFMT_BIN"
[[ -x "$PARSER" ]] || fail "JSON summarizer is not executable: $PARSER"
is_regular_file "$NEW_LIB" || fail "new library is missing, not regular, or a symlink: $NEW_LIB"
is_regular_file "$NEW_HEADER" || fail "new header is missing, not regular, or a symlink: $NEW_HEADER"
is_regular_file "$NEW_HPP" || fail "new hpp is missing, not regular, or a symlink: $NEW_HPP"
is_regular_file "$LEGACY_LIB" || fail "legacy library is missing, not regular, or a symlink: $LEGACY_LIB"

source_top=$(git -C "$SOURCE_ROOT" rev-parse --show-toplevel 2>/dev/null) ||
	fail "cannot read source checkout root: $SOURCE_ROOT"
[[ "$source_top" == "$SOURCE_ROOT" ]] ||
	fail "source checkout root is $source_top, want $SOURCE_ROOT"
source_head=$(git -C "$SOURCE_ROOT" rev-parse HEAD) || fail 'cannot read source HEAD'
source_tree=$(git -C "$SOURCE_ROOT" rev-parse 'HEAD^{tree}') || fail 'cannot read source tree'
base_tree=$(git -C "$SOURCE_ROOT" rev-parse "$BASE_COMMIT^{tree}" 2>/dev/null) ||
	fail "cannot read base tree: $BASE_COMMIT"
[[ "$source_head" == "$SOURCE_COMMIT" ]] ||
	fail "source HEAD is $source_head, want $SOURCE_COMMIT"
[[ "$source_tree" == "$SOURCE_TREE" ]] ||
	fail "source tree is $source_tree, want $SOURCE_TREE"
[[ "$base_tree" == "$BASE_TREE" ]] ||
	fail "base tree is $base_tree, want $BASE_TREE"
git -C "$SOURCE_ROOT" merge-base --is-ancestor "$BASE_COMMIT" "$SOURCE_COMMIT" ||
	fail "base commit $BASE_COMMIT is not an ancestor of $SOURCE_COMMIT"

git -C "$SOURCE_ROOT" remote get-url origin >"$EVIDENCE_DIR/source-origin-url.txt" 2>&1 ||
	fail 'cannot record source origin URL'
if ! git -C "$SOURCE_ROOT" ls-remote origin "$REMOTE_BRANCH" \
	>"$EVIDENCE_DIR/remote-branch.txt" 2>"$EVIDENCE_DIR/remote-branch.stderr.txt"; then
	fail "cannot read remote branch $REMOTE_BRANCH"
fi
remote_head=$(awk '{print $1}' "$EVIDENCE_DIR/remote-branch.txt")
[[ "$remote_head" == "$SOURCE_COMMIT" ]] ||
	fail "origin $REMOTE_BRANCH is $remote_head, want $SOURCE_COMMIT"
tracking_head=$(git -C "$SOURCE_ROOT" rev-parse "refs/remotes/origin/${REMOTE_BRANCH#refs/heads/}" 2>/dev/null) ||
	fail "missing remote-tracking ref for $REMOTE_BRANCH"
[[ "$tracking_head" == "$SOURCE_COMMIT" ]] ||
	fail "remote-tracking head is $tracking_head, want $SOURCE_COMMIT"
printf '%s\n' "$remote_head" >"$EVIDENCE_DIR/source-remote-head.txt"
printf '%s\n' "$tracking_head" >"$EVIDENCE_DIR/source-tracking-head.txt"
git -C "$SOURCE_ROOT" show -s \
	--format='%H %T %P %s' "$BASE_COMMIT" >"$EVIDENCE_DIR/base-identity.txt" ||
	fail 'cannot record base commit identity'
git -C "$SOURCE_ROOT" log --format='%H %T %s' \
	"$BASE_COMMIT..$SOURCE_COMMIT" >"$EVIDENCE_DIR/source-commits.txt" ||
	fail 'cannot record source commits'

printf '%s\n' "${CHANGED_FILES[@]}" | LC_ALL=C sort >"$EVIDENCE_DIR/changed-files-expected.txt"
git -C "$SOURCE_ROOT" diff --name-only "$BASE_COMMIT" "$SOURCE_COMMIT" |
	LC_ALL=C sort >"$EVIDENCE_DIR/changed-files-actual.txt" ||
	fail 'cannot record changed files'
cmp -s "$EVIDENCE_DIR/changed-files-expected.txt" "$EVIDENCE_DIR/changed-files-actual.txt" ||
	fail 'changed file set differs from the implementation report'
git -C "$SOURCE_ROOT" diff --name-status "$BASE_COMMIT" "$SOURCE_COMMIT" \
	>"$EVIDENCE_DIR/changed-files-status.txt" || fail 'cannot record changed file statuses'

write_source_snapshot before || fail 'cannot snapshot source before execution'
[[ -s "$EVIDENCE_DIR/git-status-before.txt" ]] &&
	fail 'source worktree is not clean before execution'
[[ "$(cat "$EVIDENCE_DIR/git-diff-exit-before.txt")" == 0 ]] ||
	fail 'source has unstaged changes before execution'
[[ "$(cat "$EVIDENCE_DIR/git-diff-cached-exit-before.txt")" == 0 ]] ||
	fail 'source has staged changes before execution'
source_sha_before=$(sha256_of "$EVIDENCE_DIR/source-manifest-before.txt") ||
	fail 'cannot hash source snapshot before execution'

new_lib_sha_before=$(sha256_of "$NEW_LIB") || fail 'cannot hash new library before execution'
new_header_sha_before=$(sha256_of "$NEW_HEADER") ||
	fail 'cannot hash new header before execution'
new_hpp_sha_before=$(sha256_of "$NEW_HPP") || fail 'cannot hash new hpp before execution'
legacy_lib_sha_before=$(sha256_of "$LEGACY_LIB") ||
	fail 'cannot hash legacy library before execution'
new_lib_size=$(stat -c '%s' -- "$NEW_LIB") || fail 'cannot stat new library size'
[[ "$new_lib_size" == "$NEW_LIB_SIZE" ]] ||
	fail "new library size is $new_lib_size, want $NEW_LIB_SIZE"
[[ "$new_lib_sha_before" == "$NEW_LIB_SHA" ]] ||
	fail "new library sha256 is $new_lib_sha_before, want $NEW_LIB_SHA"
[[ "$new_header_sha_before" == "$NEW_HEADER_SHA" ]] ||
	fail "new header sha256 is $new_header_sha_before, want $NEW_HEADER_SHA"
[[ "$new_hpp_sha_before" == "$NEW_HPP_SHA" ]] ||
	fail "new hpp sha256 is $new_hpp_sha_before, want $NEW_HPP_SHA"
[[ "$legacy_lib_sha_before" == "$LEGACY_LIB_SHA" ]] ||
	fail "legacy library sha256 is $legacy_lib_sha_before, want $LEGACY_LIB_SHA"

stat -c '%n|%d|%i|%s|%y|%z' -- \
	"$NEW_LIB" "$NEW_HEADER" "$NEW_HPP" "$LEGACY_LIB" \
	>"$EVIDENCE_DIR/artifact-stat-before.txt" || fail 'cannot stat artifacts before execution'
{
	printf 'new_library_path=%s\n' "$NEW_LIB"
	printf 'new_library_sha256=%s\n' "$new_lib_sha_before"
	printf 'new_library_size=%s\n' "$new_lib_size"
	printf 'new_header_path=%s\n' "$NEW_HEADER"
	printf 'new_header_sha256=%s\n' "$new_header_sha_before"
	printf 'new_hpp_path=%s\n' "$NEW_HPP"
	printf 'new_hpp_sha256=%s\n' "$new_hpp_sha_before"
	printf 'legacy_library_path=%s\n' "$LEGACY_LIB"
	printf 'legacy_library_sha256=%s\n' "$legacy_lib_sha_before"
} >"$EVIDENCE_DIR/artifact-identity-before.txt"

nm -D --defined-only -- "$NEW_LIB" |
	grep -F ' T lance_dataset_open_with_base_store_params' \
		>"$EVIDENCE_DIR/new-library-open-symbol.txt" ||
	fail 'new library does not export lance_dataset_open_with_base_store_params'
grep -F 'lance_dataset_open_with_base_store_params' "$NEW_HEADER" \
	>"$EVIDENCE_DIR/new-header-open-symbol.txt" ||
	fail 'new header does not declare lance_dataset_open_with_base_store_params'
nm -D --defined-only -- "$LEGACY_LIB" |
	grep -F 'lance_dataset_open_with_base_store_params' \
		>"$EVIDENCE_DIR/legacy-library-optional-symbol.txt" &&
	fail 'legacy library unexpectedly exports the optional open symbol'
printf 'absent\n' >"$EVIDENCE_DIR/legacy-library-optional-symbol-state.txt"
file -- "$NEW_LIB" "$LEGACY_LIB" >"$EVIDENCE_DIR/library-file.txt" ||
	fail 'cannot inspect library file types'

go_version=$(run_go "$NEW_LIB" version) || fail 'cannot run go version'
[[ "$go_version" == "$EXPECTED_GO_VERSION" ]] ||
	fail "unexpected Go toolchain: $go_version"
go_os=$(cd "$SOURCE_ROOT" && run_go "$NEW_LIB" env GOOS) || fail 'cannot read GOOS'
go_arch=$(cd "$SOURCE_ROOT" && run_go "$NEW_LIB" env GOARCH) || fail 'cannot read GOARCH'
go_cgo=$(cd "$SOURCE_ROOT" && run_go "$NEW_LIB" env CGO_ENABLED) ||
	fail 'cannot read CGO_ENABLED'
go_toolchain=$(cd "$SOURCE_ROOT" && run_go "$NEW_LIB" env GOTOOLCHAIN) ||
	fail 'cannot read GOTOOLCHAIN'
go_work=$(cd "$SOURCE_ROOT" && run_go "$NEW_LIB" env GOWORK) ||
	fail 'cannot read GOWORK'
go_mod=$(cd "$SOURCE_ROOT" && run_go "$NEW_LIB" env GOMOD) ||
	fail 'cannot read GOMOD'
[[ "$go_os" == linux && "$go_arch" == amd64 ]] ||
	fail "unexpected Go target: GOOS=$go_os GOARCH=$go_arch"
[[ "$go_cgo" == 0 && "$go_toolchain" == local ]] ||
	fail "unexpected Go environment: CGO_ENABLED=$go_cgo GOTOOLCHAIN=$go_toolchain"
[[ "$go_work" == off ]] || fail "unexpected Go workspace: $go_work"
[[ "$go_mod" == "$SOURCE_ROOT/go.mod" ]] ||
	fail "unexpected Go module root: $go_mod"

{
	printf 'source_root=%s\n' "$SOURCE_ROOT"
	printf 'source_head=%s\n' "$source_head"
	printf 'source_tree=%s\n' "$source_tree"
	printf 'base_head=%s\n' "$BASE_COMMIT"
	printf 'base_tree=%s\n' "$BASE_TREE"
	printf 'remote_branch=%s\n' "$REMOTE_BRANCH"
	printf 'remote_head=%s\n' "$remote_head"
	printf 'go_bin=%s\n' "$GO_BIN"
	printf 'go_version=%s\n' "$go_version"
	printf 'goos=%s\n' "$go_os"
	printf 'goarch=%s\n' "$go_arch"
	printf 'cgo_enabled=%s\n' "$go_cgo"
	printf 'gotoolchain=%s\n' "$go_toolchain"
	printf 'gowork=%s\n' "$go_work"
	printf 'gomod=%s\n' "$go_mod"
	printf 'configured_goenv=off\n'
	printf 'configured_gomaxprocs=2\n'
	printf 'configured_goflags=unset\n'
	printf 'configured_go111module=unset\n'
	printf 'configured_lance_c_header=unset\n'
	printf 'configured_ld_library_path=unset\n'
	printf 'configured_dyld_library_path=unset\n'
	printf 'kernel=%s\n' "$(uname -srm)"
	printf 'glibc=%s\n' "$(getconf GNU_LIBC_VERSION 2>/dev/null || printf unknown)"
	printf 'hostname=%s\n' "$(hostname)"
} >"$EVIDENCE_DIR/environment.txt"

printf '%s\n' "${FFI_TESTS[@]}" >"$EVIDENCE_DIR/lanes/ffi_synthetic-expected.txt"
printf '%s\n' "${OLD_TESTS[@]}" >"$EVIDENCE_DIR/lanes/public_old_library_compatible-expected.txt"
printf '%s\n' "${NEW_TESTS[@]}" >"$EVIDENCE_DIR/lanes/c226_new_runtime_zero_skip-expected.txt"

runner_sha_before=$(sha256_of "$0") || fail 'cannot hash runner before execution'
parser_sha_before=$(sha256_of "$PARSER") || fail 'cannot hash parser before execution'
{
	printf 'runner_sha256=%s\n' "$runner_sha_before"
	printf 'parser_sha256=%s\n' "$parser_sha_before"
	printf 'source_sha256=%s\n' "$source_sha_before"
	printf 'new_library_sha256=%s\n' "$new_lib_sha_before"
	printf 'new_header_sha256=%s\n' "$new_header_sha_before"
	printf 'new_hpp_sha256=%s\n' "$new_hpp_sha_before"
	printf 'legacy_library_sha256=%s\n' "$legacy_lib_sha_before"
} >"$EVIDENCE_DIR/hashes-before.txt"

printf '%s\n' "$(date -u +%FT%TZ)" >"$EVIDENCE_DIR/started-at-utc.txt"

gofmt_status=0
gofmt_check_status=0
(
	cd "$SOURCE_ROOT" || exit 70
	"$GOFMT_BIN" -d "${CHANGED_FILES[@]}"
) >"$EVIDENCE_DIR/gofmt.diff" 2>"$EVIDENCE_DIR/gofmt.stderr"
gofmt_status=$?
printf '%s\n' "$gofmt_status" >"$EVIDENCE_DIR/gofmt-exit-code.txt"
if [[ "$gofmt_status" -ne 0 || -s "$EVIDENCE_DIR/gofmt.diff" ]]; then
	gofmt_check_status=1
fi
printf '%s\n' "$gofmt_check_status" >"$EVIDENCE_DIR/gofmt-check-status.txt"
if [[ "$gofmt_check_status" -eq 0 ]]; then
	printf 'gofmt status=clean changed_files=%s\n' "${#CHANGED_FILES[@]}"
else
	printf 'gofmt status=diff-or-error exit=%s changed_files=%s\n' \
		"$gofmt_status" "${#CHANGED_FILES[@]}"
fi

ffi_status=0
old_status=0
new_status=0

run_lane \
	ffi_synthetic \
	"$NEW_LIB" \
	./internal/ffi \
	"$FFI_SELECTOR" \
	"${FFI_TESTS[@]}" || ffi_status=1

run_lane \
	public_old_library_compatible \
	"$LEGACY_LIB" \
	. \
	"$OLD_SELECTOR" \
	"${OLD_TESTS[@]}" || old_status=1

run_lane \
	c226_new_runtime_zero_skip \
	"$NEW_LIB" \
	. \
	"$NEW_SELECTOR" \
	"${NEW_TESTS[@]}" || new_status=1

postflight_status=0
postflight_error() {
	printf '%s\n' "$*" >>"$EVIDENCE_DIR/postflight-error.txt"
	postflight_status=1
}

write_source_snapshot after || postflight_error 'cannot snapshot source after execution'
source_sha_after=unavailable
if [[ -f "$EVIDENCE_DIR/source-manifest-after.txt" ]]; then
	source_sha_after=$(sha256_of "$EVIDENCE_DIR/source-manifest-after.txt") ||
		postflight_error 'cannot hash source snapshot after execution'
fi
[[ "$source_sha_after" == "$source_sha_before" ]] ||
	postflight_error "source snapshot changed: before=$source_sha_before after=$source_sha_after"
if [[ -s "$EVIDENCE_DIR/git-status-after.txt" ]]; then
	postflight_error 'source worktree is not clean after execution'
fi
[[ "$(cat "$EVIDENCE_DIR/git-diff-exit-after.txt")" == 0 ]] ||
	postflight_error 'source has unstaged changes after execution'
[[ "$(cat "$EVIDENCE_DIR/git-diff-cached-exit-after.txt")" == 0 ]] ||
	postflight_error 'source has staged changes after execution'

new_lib_sha_after=$(sha256_of "$NEW_LIB") ||
	postflight_error 'cannot hash new library after execution'
new_header_sha_after=$(sha256_of "$NEW_HEADER") ||
	postflight_error 'cannot hash new header after execution'
new_hpp_sha_after=$(sha256_of "$NEW_HPP") ||
	postflight_error 'cannot hash new hpp after execution'
legacy_lib_sha_after=$(sha256_of "$LEGACY_LIB") ||
	postflight_error 'cannot hash legacy library after execution'
[[ "$new_lib_sha_after" == "$new_lib_sha_before" ]] ||
	postflight_error "new library sha256 changed: before=$new_lib_sha_before after=$new_lib_sha_after"
[[ "$new_header_sha_after" == "$new_header_sha_before" ]] ||
	postflight_error "new header sha256 changed: before=$new_header_sha_before after=$new_header_sha_after"
[[ "$new_hpp_sha_after" == "$new_hpp_sha_before" ]] ||
	postflight_error "new hpp sha256 changed: before=$new_hpp_sha_before after=$new_hpp_sha_after"
[[ "$legacy_lib_sha_after" == "$legacy_lib_sha_before" ]] ||
	postflight_error "legacy library sha256 changed: before=$legacy_lib_sha_before after=$legacy_lib_sha_after"
stat -c '%n|%d|%i|%s|%y|%z' -- \
	"$NEW_LIB" "$NEW_HEADER" "$NEW_HPP" "$LEGACY_LIB" \
	>"$EVIDENCE_DIR/artifact-stat-after.txt" ||
	postflight_error 'cannot stat artifacts after execution'
cmp -s "$EVIDENCE_DIR/artifact-stat-before.txt" "$EVIDENCE_DIR/artifact-stat-after.txt" ||
	postflight_error 'artifact stat changed during execution'

runner_sha_after=unavailable
if runner_sha_after=$(sha256_of "$0"); then
	:
else
	postflight_error 'cannot hash runner after execution'
fi
parser_sha_after=unavailable
if parser_sha_after=$(sha256_of "$PARSER"); then
	:
else
	postflight_error 'cannot hash parser after execution'
fi
[[ "$runner_sha_after" == "$runner_sha_before" ]] ||
	postflight_error "runner changed: before=$runner_sha_before after=$runner_sha_after"
[[ "$parser_sha_after" == "$parser_sha_before" ]] ||
	postflight_error "parser changed: before=$parser_sha_before after=$parser_sha_after"

{
	printf 'new_library_sha256=%s\n' "$new_lib_sha_after"
	printf 'new_header_sha256=%s\n' "$new_header_sha_after"
	printf 'new_hpp_sha256=%s\n' "$new_hpp_sha_after"
	printf 'legacy_library_sha256=%s\n' "$legacy_lib_sha_after"
	printf 'source_sha256=%s\n' "$source_sha_after"
	printf 'runner_sha256=%s\n' "$runner_sha_after"
	printf 'parser_sha256=%s\n' "$parser_sha_after"
} >"$EVIDENCE_DIR/hashes-after.txt"

printf '%s\n' "$(date -u +%FT%TZ)" >"$EVIDENCE_DIR/finished-at-utc.txt"

overall_status=0
if [[ "$ffi_status" -ne 0 || "$old_status" -ne 0 || "$new_status" -ne 0 ]]; then
	overall_status=1
fi
if [[ "$gofmt_check_status" -ne 0 ]]; then
	overall_status=4
fi
if [[ "$postflight_status" -ne 0 ]]; then
	overall_status=3
fi
printf '%s\n' "$overall_status" >"$EVIDENCE_DIR/runner-exit-code.txt"
printf 'evidence=%s ffi_exit=%s old_exit=%s new_exit=%s gofmt_exit=%s postflight_exit=%s runner_exit=%s\n' \
	"$EVIDENCE_DIR" "$ffi_status" "$old_status" "$new_status" \
	"$gofmt_check_status" "$postflight_status" "$overall_status"
exit "$overall_status"
