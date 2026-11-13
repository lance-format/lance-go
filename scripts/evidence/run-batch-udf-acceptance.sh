#!/usr/bin/env bash

set -u
set -o pipefail

if [[ $# -ne 5 ]]; then
	printf 'usage: %s LIBRARY EXPECTED_SHA256 EVIDENCE_DIR SOURCE_COMMIT SOURCE_TREE\n' "$0" >&2
	exit 64
fi

LIBRARY=$1
EXPECTED_LIBRARY_SHA=$2
EVIDENCE_DIR=$3

SOURCE_ROOT=/home/xuanwo/Code/lance-go-beta20-acceptance-20261113
GO_BIN=/home/xuanwo/Code/tools/go/bin/go

SOURCE_COMMIT=$4
SOURCE_TREE=$5
EXPECTED_GO_VERSION='go version go1.23.12 linux/amd64'

ROOT_PACKAGE=.
FFI_PACKAGE=./internal/ffi
ROOT_SELECTOR='^Test.*BatchUDF'
FFI_SELECTOR='^Test.*BatchUDF'
FRAGMENT_SELECTOR='^TestFragmentAddColumnsVariantsPublish$/^batch_udf$'

if [[ "$LIBRARY" != /* ]]; then
	printf 'library path must be absolute: %s\n' "$LIBRARY" >&2
	exit 64
fi
if [[ "$EVIDENCE_DIR" != /* || "$EVIDENCE_DIR" == / ]]; then
	printf 'evidence directory must be an absolute non-root path: %s\n' "$EVIDENCE_DIR" >&2
	exit 64
fi
if [[ ! "$EXPECTED_LIBRARY_SHA" =~ ^[0-9a-f]{64}$ ]]; then
	printf 'expected library sha256 must be 64 lowercase hex characters: %s\n' "$EXPECTED_LIBRARY_SHA" >&2
	exit 64
fi
if [[ ! "$SOURCE_COMMIT" =~ ^[0-9a-f]{40}$ || ! "$SOURCE_TREE" =~ ^[0-9a-f]{40}$ ]]; then
    printf 'source commit and tree must be full Git object IDs\n' >&2
    exit 64
fi
case "$EVIDENCE_DIR/" in
"$SOURCE_ROOT/"*)
	printf 'evidence directory must be outside the source checkout: %s\n' "$EVIDENCE_DIR" >&2
	exit 64
	;;
esac
if [[ -e "$EVIDENCE_DIR" ]]; then
	printf 'evidence directory already exists: %s\n' "$EVIDENCE_DIR" >&2
	exit 65
fi

umask 022
mkdir -p "$EVIDENCE_DIR/raw" || exit 70

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
	env \
		-u GOFLAGS \
		-u GO111MODULE \
		-u GOWORK \
		CGO_ENABLED=0 \
		GOENV=off \
		GOTOOLCHAIN=local \
		LANCE_LIB_PATH="$LIBRARY" \
		GOMAXPROCS=2 \
		"$GO_BIN" "$@"
}

write_source_snapshot() {
	local phase=$1
	local identity="$EVIDENCE_DIR/source-identity-$phase.txt"
	local status="$EVIDENCE_DIR/git-status-$phase.txt"
	local manifest="$EVIDENCE_DIR/source-manifest-$phase.txt"

	git -C "$SOURCE_ROOT" show -s \
		--format='commit %H%ntree %T%nparent %P%nauthor %an <%ae> %ad%ncommitter %cn <%ce> %cd%nsubject %s' \
		--date=iso-strict HEAD >"$identity" || return 1
	git -C "$SOURCE_ROOT" status --porcelain=v1 --untracked-files=all >"$status" || return 1
	{
		cat "$identity"
		printf '%s\n' '--- tree ---'
		git -C "$SOURCE_ROOT" ls-tree -r --full-tree HEAD || return 1
		printf '%s\n' '--- status ---'
		cat "$status"
	} >"$manifest" || return 1
	sha256_of "$manifest" >"$EVIDENCE_DIR/source-sha256-$phase.txt"
}

write_command_lines() {
	local output=$1
	{
		printf 'root_package=%s\n' "$ROOT_PACKAGE"
		printf 'ffi_package=%s\n' "$FFI_PACKAGE"
		printf 'root: cd %q && env -u GOFLAGS -u GO111MODULE -u GOWORK CGO_ENABLED=0 GOENV=off GOTOOLCHAIN=local LANCE_LIB_PATH=%q GOMAXPROCS=2 %q test -json -count=1 -p 1 -vet=off -tags=lance_external_lib -run %q %s\n' \
			"$SOURCE_ROOT" "$LIBRARY" "$GO_BIN" "$ROOT_SELECTOR" "$ROOT_PACKAGE"
		printf 'ffi: cd %q && env -u GOFLAGS -u GO111MODULE -u GOWORK CGO_ENABLED=0 GOENV=off GOTOOLCHAIN=local LANCE_LIB_PATH=%q GOMAXPROCS=2 %q test -json -count=1 -p 1 -vet=off -tags=lance_external_lib -run %q %s\n' \
			"$SOURCE_ROOT" "$LIBRARY" "$GO_BIN" "$FFI_SELECTOR" "$FFI_PACKAGE"
        printf 'fragment: cd %q && env -u GOFLAGS -u GO111MODULE -u GOWORK CGO_ENABLED=0 GOENV=off GOTOOLCHAIN=local LANCE_LIB_PATH=%q GOMAXPROCS=2 %q test -json -count=1 -p 1 -vet=off -tags=lance_external_lib -run %q %s\n' \
            "$SOURCE_ROOT" "$LIBRARY" "$GO_BIN" "$FRAGMENT_SELECTOR" "$ROOT_PACKAGE"
	} >"$output"
}

capture_environment() {
	local output=$1
	(
		cd "$SOURCE_ROOT" || exit 1
		printf 'source_root=%s\n' "$SOURCE_ROOT"
		printf 'library_path=%s\n' "$LIBRARY"
		printf 'expected_library_sha256=%s\n' "$EXPECTED_LIBRARY_SHA"
		printf 'configured_goenv=off\n'
		printf 'configured_cgo_enabled=0\n'
		printf 'configured_gotoolchain=local\n'
		printf 'configured_gomaxprocs=2\n'
		printf 'configured_goflags=unset\n'
		printf 'configured_go111module=unset\n'
		printf 'configured_gowork=unset\n'
		printf 'go_bin=%s\n' "$GO_BIN"
		printf 'go_version=%s\n' "$(run_go version)"
		printf 'uname_s=%s\n' "$(uname -s)"
		printf 'uname_r=%s\n' "$(uname -r)"
		printf 'uname_m=%s\n' "$(uname -m)"
		printf 'glibc=%s\n' "$(getconf GNU_LIBC_VERSION 2>/dev/null || printf unknown)"
		printf 'git_path=%s\n' "$(command -v git)"
		printf 'git_version=%s\n' "$(git --version)"
		printf 'goos=%s\n' "$(run_go env GOOS)"
		printf 'goarch=%s\n' "$(run_go env GOARCH)"
		printf 'goversion=%s\n' "$(run_go env GOVERSION)"
		printf 'cgo_enabled=%s\n' "$(run_go env CGO_ENABLED)"
		printf 'gotoolchain=%s\n' "$(run_go env GOTOOLCHAIN)"
		printf 'gowork=%s\n' "$(run_go env GOWORK)"
		printf 'gomod=%s\n' "$(run_go env GOMOD)"
		printf 'gocache=%s\n' "$(run_go env GOCACHE)"
		printf 'gomodcache=%s\n' "$(run_go env GOMODCACHE)"
	) >"$output" 2>&1
}

run_package() {
	local label=$1
	local package=$2
	local selector=$3
	local status

	printf '%s\n' "$(date -u +%FT%TZ)" >"$EVIDENCE_DIR/raw/$label.started-at-utc.txt"
	(
		cd "$SOURCE_ROOT" || exit 70
		run_go test \
			-json \
			-count=1 \
			-p 1 \
			-vet=off \
			-tags=lance_external_lib \
			-run "$selector" \
			"$package"
	) >"$EVIDENCE_DIR/raw/$label.stdout.json" 2>"$EVIDENCE_DIR/raw/$label.stderr.txt"
	status=$?
	printf '%s\n' "$status" >"$EVIDENCE_DIR/raw/$label.exit-code.txt"
	printf '%s\n' "$(date -u +%FT%TZ)" >"$EVIDENCE_DIR/raw/$label.finished-at-utc.txt"
	return "$status"
}

[[ -d "$SOURCE_ROOT" ]] || fail "source checkout is missing: $SOURCE_ROOT"
[[ -x "$GO_BIN" ]] || fail "Go tool is not executable: $GO_BIN"
is_regular_file "$LIBRARY" || fail "library is missing, not regular, or a symlink: $LIBRARY"

source_top=$(git -C "$SOURCE_ROOT" rev-parse --show-toplevel 2>/dev/null) ||
	fail "cannot read source checkout root: $SOURCE_ROOT"
[[ "$source_top" == "$SOURCE_ROOT" ]] ||
	fail "source checkout root is $source_top, want $SOURCE_ROOT"
source_head=$(git -C "$SOURCE_ROOT" rev-parse HEAD) || fail 'cannot read source HEAD'
source_tree=$(git -C "$SOURCE_ROOT" rev-parse 'HEAD^{tree}') || fail 'cannot read source tree'
[[ "$source_head" == "$SOURCE_COMMIT" ]] || fail "source HEAD is $source_head, want $SOURCE_COMMIT"
[[ "$source_tree" == "$SOURCE_TREE" ]] || fail "source tree is $source_tree, want $SOURCE_TREE"

write_source_snapshot before || fail 'cannot snapshot source before execution'
if [[ -s "$EVIDENCE_DIR/git-status-before.txt" ]]; then
	fail 'source worktree is not clean before execution'
fi
source_sha_before=$(sha256_of "$EVIDENCE_DIR/source-manifest-before.txt") ||
	fail 'cannot hash source snapshot before execution'

library_sha_before=$(sha256_of "$LIBRARY") || fail 'cannot hash library before execution'
printf '%s\n' "$EXPECTED_LIBRARY_SHA" >"$EVIDENCE_DIR/library-sha256-expected.txt"
printf '%s\n' "$library_sha_before" >"$EVIDENCE_DIR/library-sha256-before.txt"
printf 'path=%s\nexpected_sha256=%s\nactual_sha256=%s\n' \
	"$LIBRARY" "$EXPECTED_LIBRARY_SHA" "$library_sha_before" \
	>"$EVIDENCE_DIR/library-identity-before.txt"
stat -c '%n|%d|%i|%s|%y|%z' -- "$LIBRARY" >"$EVIDENCE_DIR/library-stat-before.txt" ||
	fail 'cannot stat library before execution'
[[ "$library_sha_before" == "$EXPECTED_LIBRARY_SHA" ]] ||
	fail "library sha256 is $library_sha_before, want $EXPECTED_LIBRARY_SHA"

go_version=$(run_go version) || fail 'cannot run go version'
[[ "$go_version" == "$EXPECTED_GO_VERSION" ]] ||
	fail "unexpected Go toolchain: $go_version"
go_os=$(cd "$SOURCE_ROOT" && run_go env GOOS) || fail 'cannot read GOOS'
go_arch=$(cd "$SOURCE_ROOT" && run_go env GOARCH) || fail 'cannot read GOARCH'
go_cgo=$(cd "$SOURCE_ROOT" && run_go env CGO_ENABLED) || fail 'cannot read CGO_ENABLED'
go_toolchain=$(cd "$SOURCE_ROOT" && run_go env GOTOOLCHAIN) || fail 'cannot read GOTOOLCHAIN'
go_work=$(cd "$SOURCE_ROOT" && run_go env GOWORK) || fail 'cannot read GOWORK'
go_mod=$(cd "$SOURCE_ROOT" && run_go env GOMOD) || fail 'cannot read GOMOD'
[[ "$go_os" == linux && "$go_arch" == amd64 ]] ||
	fail "unexpected Go target: GOOS=$go_os GOARCH=$go_arch"
[[ "$go_cgo" == 0 && "$go_toolchain" == local ]] ||
	fail "unexpected Go environment: CGO_ENABLED=$go_cgo GOTOOLCHAIN=$go_toolchain"
[[ -z "$go_work" || "$go_work" == off ]] || fail "unexpected Go workspace: $go_work"
[[ "$go_mod" == "$SOURCE_ROOT/go.mod" ]] || fail "unexpected Go module root: $go_mod"

write_command_lines "$EVIDENCE_DIR/command-lines-before.txt" ||
	fail 'cannot record command lines before execution'
command_sha_before=$(sha256_of "$EVIDENCE_DIR/command-lines-before.txt") ||
	fail 'cannot hash command lines before execution'
capture_environment "$EVIDENCE_DIR/environment-before.txt" ||
	fail 'cannot capture environment before execution'
environment_sha_before=$(sha256_of "$EVIDENCE_DIR/environment-before.txt") ||
	fail 'cannot hash environment before execution'
runner_sha_before=$(sha256_of "$0") || fail 'cannot hash runner before execution'

printf '%s\n' "$ROOT_SELECTOR" >"$EVIDENCE_DIR/selectors-root.txt"
printf '%s\n' "$FFI_SELECTOR" >"$EVIDENCE_DIR/selectors-ffi.txt"
printf '%s\n' "$FRAGMENT_SELECTOR" >"$EVIDENCE_DIR/selectors-fragment.txt"

{
	printf 'runner_sha256=%s\n' "$runner_sha_before"
	printf 'source_sha256=%s\n' "$source_sha_before"
	printf 'command_sha256=%s\n' "$command_sha_before"
	printf 'environment_sha256=%s\n' "$environment_sha_before"
	printf 'library_sha256=%s\n' "$library_sha_before"
} >"$EVIDENCE_DIR/hashes-before.txt"

printf '%s\n' "$(date -u +%FT%TZ)" >"$EVIDENCE_DIR/started-at-utc.txt"

root_status=0
run_package root "$ROOT_PACKAGE" "$ROOT_SELECTOR" || root_status=$?
ffi_status=0
run_package ffi "$FFI_PACKAGE" "$FFI_SELECTOR" || ffi_status=$?
fragment_status=0
run_package fragment "$ROOT_PACKAGE" "$FRAGMENT_SELECTOR" || fragment_status=$?

postflight_status=0
postflight_error() {
	printf '%s\n' "$*" >>"$EVIDENCE_DIR/postflight-error.txt"
	postflight_status=1
}

library_sha_after=missing
if is_regular_file "$LIBRARY"; then
	if library_sha_after=$(sha256_of "$LIBRARY"); then
		:
	else
		library_sha_after=unreadable
		postflight_error "cannot hash library after execution"
	fi
	stat -c '%n|%d|%i|%s|%y|%z' -- "$LIBRARY" >"$EVIDENCE_DIR/library-stat-after.txt" ||
		postflight_error "cannot stat library after execution"
else
	postflight_error "library is missing, not regular, or a symlink after execution: $LIBRARY"
	printf 'missing\n' >"$EVIDENCE_DIR/library-stat-after.txt"
fi
printf '%s\n' "$library_sha_after" >"$EVIDENCE_DIR/library-sha256-after.txt"
printf 'path=%s\nexpected_sha256=%s\nactual_sha256=%s\n' \
	"$LIBRARY" "$EXPECTED_LIBRARY_SHA" "$library_sha_after" \
	>"$EVIDENCE_DIR/library-identity-after.txt"
[[ "$library_sha_after" == "$library_sha_before" ]] ||
	postflight_error "library sha256 changed: before=$library_sha_before after=$library_sha_after"
if [[ -f "$EVIDENCE_DIR/library-stat-before.txt" && -f "$EVIDENCE_DIR/library-stat-after.txt" ]]; then
	cmp -s "$EVIDENCE_DIR/library-stat-before.txt" "$EVIDENCE_DIR/library-stat-after.txt" ||
		postflight_error "library stat changed during execution"
fi

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

write_command_lines "$EVIDENCE_DIR/command-lines-after.txt" ||
	postflight_error 'cannot record command lines after execution'
command_sha_after=unavailable
if [[ -f "$EVIDENCE_DIR/command-lines-after.txt" ]]; then
	command_sha_after=$(sha256_of "$EVIDENCE_DIR/command-lines-after.txt") ||
		postflight_error 'cannot hash command lines after execution'
fi
[[ "$command_sha_after" == "$command_sha_before" ]] ||
	postflight_error "command lines changed: before=$command_sha_before after=$command_sha_after"

capture_environment "$EVIDENCE_DIR/environment-after.txt" ||
	postflight_error 'cannot capture environment after execution'
environment_sha_after=unavailable
if [[ -f "$EVIDENCE_DIR/environment-after.txt" ]]; then
	environment_sha_after=$(sha256_of "$EVIDENCE_DIR/environment-after.txt") ||
		postflight_error 'cannot hash environment after execution'
fi
[[ "$environment_sha_after" == "$environment_sha_before" ]] ||
	postflight_error "environment changed: before=$environment_sha_before after=$environment_sha_after"

runner_sha_after=unavailable
if runner_sha_after=$(sha256_of "$0"); then
	:
else
	postflight_error 'cannot hash runner after execution'
fi
[[ "$runner_sha_after" == "$runner_sha_before" ]] ||
	postflight_error "runner changed: before=$runner_sha_before after=$runner_sha_after"

{
	printf 'runner_sha256=%s\n' "$runner_sha_after"
	printf 'source_sha256=%s\n' "$source_sha_after"
	printf 'command_sha256=%s\n' "$command_sha_after"
	printf 'environment_sha256=%s\n' "$environment_sha_after"
	printf 'library_sha256=%s\n' "$library_sha_after"
} >"$EVIDENCE_DIR/hashes-after.txt"

printf '%s\n' "$(date -u +%FT%TZ)" >"$EVIDENCE_DIR/finished-at-utc.txt"

overall_status=$root_status
if [[ "$overall_status" -eq 0 ]]; then
	overall_status=$ffi_status
fi
if [[ "$overall_status" -eq 0 ]]; then
	overall_status=$fragment_status
fi
if [[ "$overall_status" -eq 0 && "$postflight_status" -ne 0 ]]; then
	overall_status=3
fi
printf '%s\n' "$overall_status" >"$EVIDENCE_DIR/runner-exit-code.txt"
printf 'evidence=%s root_exit=%s ffi_exit=%s fragment_exit=%s postflight_exit=%s runner_exit=%s\n' \
	"$EVIDENCE_DIR" "$root_status" "$ffi_status" "$fragment_status" "$postflight_status" "$overall_status"
exit "$overall_status"
