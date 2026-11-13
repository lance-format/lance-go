#!/usr/bin/env bash

set -u
set -o pipefail

if [[ $# -ne 2 ]]; then
	printf 'usage: %s SOURCE_ROOT EVIDENCE_DIR\n' "$0" >&2
	exit 64
fi

SOURCE_ROOT=$1
EVIDENCE_DIR=$2

SOURCE_COMMIT=0e186d62d0fe3758e190502f9273df7dd7409c55
SOURCE_TREE=8816f47b6112dbf9812a978cbae2e0f56862ecb7
GO_BIN=/home/xuanwo/Code/tools/go/bin/go
EXPECTED_GO_VERSION='go version go1.23.12 linux/amd64'

ARTIFACT_ROOT=/home/xuanwo/Code/lance-c-v15-migration-evidence-20261113/candidate-382ed25
LIBRARY=$ARTIFACT_ROOT/lib/liblance_c.so
HEADER=$ARTIFACT_ROOT/include/lance/lance.h
CPP_HEADER=$ARTIFACT_ROOT/include/lance/lance.hpp
IDENTITY=$ARTIFACT_ROOT/IDENTITY.txt
CARGO_LOCK=/home/xuanwo/Code/lance-c-v15-migration/Cargo.lock

EXPECTED_LIBRARY_SHA=3fcf09f82ff12b003f7a9c5afb4121a24607594aabca9e9be03850dc5c5eab3c
EXPECTED_HEADER_SHA=37895ce8eb1a66b8ab95c68f207b3d914e811112a97dd437470fb0b159a9f502
EXPECTED_CPP_HEADER_SHA=6cd4a6c3ba4cf76a83fa0b9c378d15f783b4216e8c8e097708cb18c90b68fb39
EXPECTED_CARGO_LOCK_SHA=20824b7e1455b2d6373ca4fbbeed845dae06b9efb02a682c558323d00ca6d123

SELECTOR='^(TestFragmentMergeAndUpdatePublish|TestIndexSegmentProgressAndMetadata|TestNearestMultiVector|TestFTSPreparedQueriesPinSnapshot|TestMergeInsertModesAndStats|TestScannerScalarIndexSegment)$'

if [[ -e "$EVIDENCE_DIR" ]]; then
	printf 'evidence directory already exists: %s\n' "$EVIDENCE_DIR" >&2
	exit 65
fi
mkdir -p "$EVIDENCE_DIR/raw"

fail() {
	printf 'preflight failed: %s\n' "$*" >&2
	printf '%s\n' "$*" >"$EVIDENCE_DIR/preflight-error.txt"
	exit 2
}

sha256_of() {
	sha256sum -- "$1" | awk '{print $1}'
}

is_regular_file() {
	[[ -f "$1" && ! -L "$1" ]]
}

[[ -d "$SOURCE_ROOT/.git" ]] || fail "source root is not a Git checkout: $SOURCE_ROOT"
[[ -x "$GO_BIN" ]] || fail "Go tool is not executable: $GO_BIN"
for path in "$LIBRARY" "$HEADER" "$CPP_HEADER" "$IDENTITY" "$CARGO_LOCK"; do
	is_regular_file "$path" || fail "required supplied file is missing or not regular: $path"
done

source_head=$(git -C "$SOURCE_ROOT" rev-parse HEAD) || fail 'cannot read source HEAD'
source_tree=$(git -C "$SOURCE_ROOT" rev-parse 'HEAD^{tree}') || fail 'cannot read source tree'
[[ "$source_head" == "$SOURCE_COMMIT" ]] || fail "source HEAD is $source_head, want $SOURCE_COMMIT"
[[ "$source_tree" == "$SOURCE_TREE" ]] || fail "source tree is $source_tree, want $SOURCE_TREE"

git -C "$SOURCE_ROOT" status --porcelain=v1 --untracked-files=all >"$EVIDENCE_DIR/git-status-before.txt" ||
	fail 'cannot inspect source status before execution'
if [[ -s "$EVIDENCE_DIR/git-status-before.txt" ]]; then
	cp "$EVIDENCE_DIR/git-status-before.txt" "$EVIDENCE_DIR/preflight-git-status.txt"
	fail 'source worktree is not clean before execution'
fi

go_version=$(env CGO_ENABLED=0 GOTOOLCHAIN=local GOENV=off "$GO_BIN" version) ||
	fail 'cannot run go version'
[[ "$go_version" == "$EXPECTED_GO_VERSION" ]] ||
	fail "unexpected Go toolchain: $go_version"

go_os=$(env CGO_ENABLED=0 GOTOOLCHAIN=local GOENV=off "$GO_BIN" env GOOS) || fail 'cannot read GOOS'
go_arch=$(env CGO_ENABLED=0 GOTOOLCHAIN=local GOENV=off "$GO_BIN" env GOARCH) || fail 'cannot read GOARCH'
go_cgo=$(env CGO_ENABLED=0 GOTOOLCHAIN=local GOENV=off "$GO_BIN" env CGO_ENABLED) || fail 'cannot read CGO_ENABLED'
go_toolchain=$(env CGO_ENABLED=0 GOTOOLCHAIN=local GOENV=off "$GO_BIN" env GOTOOLCHAIN) || fail 'cannot read GOTOOLCHAIN'
go_work=$(cd "$SOURCE_ROOT" && env CGO_ENABLED=0 GOTOOLCHAIN=local GOENV=off "$GO_BIN" env GOWORK) ||
	fail 'cannot read GOWORK'
[[ "$go_os" == linux && "$go_arch" == amd64 && "$go_cgo" == 0 && "$go_toolchain" == local ]] ||
	fail "unexpected Go environment: GOOS=$go_os GOARCH=$go_arch CGO_ENABLED=$go_cgo GOTOOLCHAIN=$go_toolchain"
[[ -z "$go_work" || "$go_work" == off ]] || fail "unexpected Go workspace: $go_work"

library_sha_before=$(sha256_of "$LIBRARY")
header_sha=$(sha256_of "$HEADER")
cpp_header_sha=$(sha256_of "$CPP_HEADER")
cargo_lock_sha=$(sha256_of "$CARGO_LOCK")

[[ "$library_sha_before" == "$EXPECTED_LIBRARY_SHA" ]] ||
	fail "library sha256 is $library_sha_before, want $EXPECTED_LIBRARY_SHA"
[[ "$header_sha" == "$EXPECTED_HEADER_SHA" ]] ||
	fail "C header sha256 is $header_sha, want $EXPECTED_HEADER_SHA"
[[ "$cpp_header_sha" == "$EXPECTED_CPP_HEADER_SHA" ]] ||
	fail "C++ header sha256 is $cpp_header_sha, want $EXPECTED_CPP_HEADER_SHA"
[[ "$cargo_lock_sha" == "$EXPECTED_CARGO_LOCK_SHA" ]] ||
	fail "Cargo.lock sha256 is $cargo_lock_sha, want $EXPECTED_CARGO_LOCK_SHA"

grep -Fq "$SOURCE_COMMIT" "$IDENTITY" || fail 'IDENTITY.txt does not name the candidate source commit'
grep -Fq "$EXPECTED_LIBRARY_SHA" "$IDENTITY" || fail 'IDENTITY.txt does not record the expected library digest'
grep -Fq "$EXPECTED_CARGO_LOCK_SHA" "$IDENTITY" || fail 'IDENTITY.txt does not record the expected Cargo.lock digest'
grep -Fq "$EXPECTED_HEADER_SHA" "$IDENTITY" || fail 'IDENTITY.txt does not record the expected C header digest'
grep -Fq "$EXPECTED_CPP_HEADER_SHA" "$IDENTITY" || fail 'IDENTITY.txt does not record the expected C++ header digest'

cp -- "$IDENTITY" "$EVIDENCE_DIR/IDENTITY.txt"
sha256sum -- "$LIBRARY" "$HEADER" "$CPP_HEADER" "$CARGO_LOCK" >"$EVIDENCE_DIR/hashes-before.txt"
stat -c '%n|%s|%y' "$LIBRARY" "$HEADER" "$CPP_HEADER" "$CARGO_LOCK" "$IDENTITY" >"$EVIDENCE_DIR/artifact-stats-before.txt"

git -C "$SOURCE_ROOT" show -s --format='commit %H%ntree %T%nparent %P%nauthor %an <%ae> %ad%ncommitter %cn <%ce> %cd%nsubject %s' --date=iso-strict HEAD \
	>"$EVIDENCE_DIR/source-identity.txt"
printf '%s\n' \
	TestFragmentMergeAndUpdatePublish \
	TestIndexSegmentProgressAndMetadata \
	TestNearestMultiVector \
	TestFTSPreparedQueriesPinSnapshot \
	TestMergeInsertModesAndStats \
	TestScannerScalarIndexSegment >"$EVIDENCE_DIR/selectors.txt"

command_text="cd $(printf '%q' "$SOURCE_ROOT") && env CGO_ENABLED=0 GOTOOLCHAIN=local GOENV=off LANCE_LIB_PATH=$(printf '%q' "$LIBRARY") $(printf '%q' "$GO_BIN") test -json -count=1 -tags lance_external_lib -run $(printf '%q' "$SELECTOR") ."
printf '%s\n' "$command_text" >"$EVIDENCE_DIR/command.txt"

{
	printf 'timestamp_utc=%s\n' "$(date -u +%FT%TZ)"
	printf 'source_root=%s\n' "$SOURCE_ROOT"
	printf 'source_head=%s\n' "$source_head"
	printf 'source_tree=%s\n' "$source_tree"
	printf 'go_bin=%s\n' "$GO_BIN"
	printf 'go_version=%s\n' "$go_version"
	uname -srmo
	command -v git
	git --version
	getconf GNU_LIBC_VERSION 2>/dev/null || true
	(
		cd "$SOURCE_ROOT" || exit 70
		env CGO_ENABLED=0 GOTOOLCHAIN=local GOENV=off "$GO_BIN" version
		env CGO_ENABLED=0 GOTOOLCHAIN=local GOENV=off "$GO_BIN" env GOOS GOARCH GOVERSION CGO_ENABLED GOTOOLCHAIN GOENV GOWORK GOMOD GOPATH GOMODCACHE GOCACHE
	)
} >"$EVIDENCE_DIR/environment.txt" 2>&1

printf '%s\n' "$(date -u +%FT%TZ)" >"$EVIDENCE_DIR/started-at-utc.txt"
(
	cd "$SOURCE_ROOT" || exit 70
	env CGO_ENABLED=0 GOTOOLCHAIN=local GOENV=off LANCE_LIB_PATH="$LIBRARY" \
		"$GO_BIN" test -json -count=1 -tags lance_external_lib -run "$SELECTOR" .
) >"$EVIDENCE_DIR/raw/go-test.stdout.json" 2>"$EVIDENCE_DIR/raw/go-test.stderr"
test_status=$?
printf '%s\n' "$test_status" >"$EVIDENCE_DIR/raw/go-test.exit-code"
printf '%s\n' "$(date -u +%FT%TZ)" >"$EVIDENCE_DIR/finished-at-utc.txt"

postflight_status=0
library_sha_after=$(sha256_of "$LIBRARY")
printf '%s\n' "$library_sha_after" >"$EVIDENCE_DIR/library-sha256-after.txt"
sha256sum -- "$LIBRARY" "$HEADER" "$CPP_HEADER" "$CARGO_LOCK" >"$EVIDENCE_DIR/hashes-after.txt"
stat -c '%n|%s|%y' "$LIBRARY" "$HEADER" "$CPP_HEADER" "$CARGO_LOCK" "$IDENTITY" >"$EVIDENCE_DIR/artifact-stats-after.txt"
if [[ "$library_sha_after" != "$library_sha_before" ]]; then
	printf 'library sha256 changed: before=%s after=%s\n' "$library_sha_before" "$library_sha_after" \
		>"$EVIDENCE_DIR/postflight-error.txt"
	postflight_status=1
fi

git -C "$SOURCE_ROOT" status --porcelain=v1 --untracked-files=all >"$EVIDENCE_DIR/git-status-after.txt" ||
	postflight_status=1
if [[ -s "$EVIDENCE_DIR/git-status-after.txt" ]]; then
	printf 'source worktree changed during execution\n' >>"$EVIDENCE_DIR/postflight-error.txt"
	postflight_status=1
fi

if [[ "$test_status" -ne 0 ]]; then
	exit "$test_status"
fi
if [[ "$postflight_status" -ne 0 ]]; then
	exit 3
fi
exit 0
