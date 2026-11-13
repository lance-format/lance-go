#!/usr/bin/env bash

set -u
set -o pipefail

if [[ $# -ne 5 ]]; then
	printf 'usage: %s SOURCE_ROOT EVIDENCE_DIR FINALIZER LAUNCHER LAUNCH_LOG\n' "$0" >&2
	exit 64
fi

SOURCE_ROOT=$1
EVIDENCE_DIR=$2
FINALIZER=$3
LAUNCHER=$4
LAUNCH_LOG=$5

SOURCE_COMMIT=6c21e7520f40888a2f780f7b3ecf0ec18a7cd91c
SOURCE_TREE=260fa4a8c9c73a36da52feb0f769af1f31a1680e
BASE_COMMIT=c23fee617cab010c336343453e8911bc4a7f16b7
CANDIDATE_SOURCE=c8e07c0c7fb455747df2724e506d4bc684093be6
CORE_PIN=98f84091db531775ab03fa7edb3d34c86937fbf5
NATIVE_VERSION=v15.0.0-beta.19
GO_BIN=/home/xuanwo/Code/tools/go/bin/go
GOFMT_BIN=/home/xuanwo/Code/tools/go/bin/gofmt
EXPECTED_GO_VERSION='go version go1.23.12 linux/amd64'

ARTIFACT_ROOT=/home/xuanwo/Code/lance-c-v15-migration-evidence-20261113/candidate-c8e07c0
LIBRARY=$ARTIFACT_ROOT/lib/liblance_c.so
HEADER=$ARTIFACT_ROOT/include/lance/lance.h
CPP_HEADER=$ARTIFACT_ROOT/include/lance/lance.hpp
IDENTITY=$ARTIFACT_ROOT/IDENTITY.txt
NATIVE_SUMS=$ARTIFACT_ROOT/SHA256SUMS
CARGO_LOCK=/home/xuanwo/Code/lance-c-v15-migration/Cargo.lock

EXPECTED_LIBRARY_SHA=46d4bdac8497255d78e637dd8ef13b3c0e6986a3ca4aa532fff06511c00b3b13
EXPECTED_HEADER_SHA=37895ce8eb1a66b8ab95c68f207b3d914e811112a97dd437470fb0b159a9f502
EXPECTED_CPP_HEADER_SHA=6cd4a6c3ba4cf76a83fa0b9c378d15f783b4216e8c8e097708cb18c90b68fb39
EXPECTED_CARGO_LOCK_SHA=20824b7e1455b2d6373ca4fbbeed845dae06b9efb02a682c558323d00ca6d123
EXPECTED_ARCHIVE_SHA=a29dc366627b3af8383cb8c5c34732a72db0e8c57500ee7b2e816965284171a6

SELECTOR='^TestFTSPreparedIndexOnlyExcludesUnindexedFragment$'

if [[ -e "$EVIDENCE_DIR" ]]; then
	printf 'evidence directory already exists: %s\n' "$EVIDENCE_DIR" >&2
	exit 65
fi
mkdir -p "$EVIDENCE_DIR/raw" || exit 70

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
[[ -x "$GOFMT_BIN" ]] || fail "gofmt tool is not executable: $GOFMT_BIN"
[[ -f "$FINALIZER" ]] || fail "finalizer is missing: $FINALIZER"
[[ -f "$LAUNCHER" ]] || fail "launcher is missing: $LAUNCHER"
[[ -f "$LAUNCH_LOG" ]] || fail "launch log is missing: $LAUNCH_LOG"
for path in "$LIBRARY" "$HEADER" "$CPP_HEADER" "$IDENTITY" "$NATIVE_SUMS" "$CARGO_LOCK"; do
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

git -C "$SOURCE_ROOT" diff --name-status "$BASE_COMMIT..$SOURCE_COMMIT" >"$EVIDENCE_DIR/source-diff-name-status.txt" ||
	fail 'cannot inspect the accepted source diff'
expected_diff=$'M\tdataset_fts.go\nM\tdataset_fts_test.go'
actual_diff=$(cat "$EVIDENCE_DIR/source-diff-name-status.txt")
[[ "$actual_diff" == "$expected_diff" ]] ||
	fail 'source diff does not match the accepted two-file scope'
git -C "$SOURCE_ROOT" diff --stat "$BASE_COMMIT..$SOURCE_COMMIT" >"$EVIDENCE_DIR/source-diff-stat.txt"
git -C "$SOURCE_ROOT" diff --check "$BASE_COMMIT..$SOURCE_COMMIT" >"$EVIDENCE_DIR/source-diff-check.txt" 2>&1 ||
	fail 'source diff contains whitespace errors'
[[ ! -s "$EVIDENCE_DIR/source-diff-check.txt" ]] || fail 'git diff --check emitted diagnostics'

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
go_mod=$(cd "$SOURCE_ROOT" && env CGO_ENABLED=0 GOTOOLCHAIN=local GOENV=off "$GO_BIN" env GOMOD) ||
	fail 'cannot read GOMOD'
[[ "$go_os" == linux && "$go_arch" == amd64 && "$go_cgo" == 0 && "$go_toolchain" == local ]] ||
	fail "unexpected Go environment: GOOS=$go_os GOARCH=$go_arch CGO_ENABLED=$go_cgo GOTOOLCHAIN=$go_toolchain"
[[ -z "$go_work" || "$go_work" == off ]] || fail "unexpected Go workspace: $go_work"
[[ "$go_mod" == "$SOURCE_ROOT/go.mod" ]] || fail "unexpected Go module root: $go_mod"

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

grep -Fq "source_commit=$CANDIDATE_SOURCE" "$IDENTITY" || fail 'IDENTITY.txt does not name the candidate source commit'
grep -Fq "native_lance_rev=$CORE_PIN" "$IDENTITY" || fail 'IDENTITY.txt does not name the upstream native pin'
grep -Fq "native_lance_version=$NATIVE_VERSION" "$IDENTITY" || fail 'IDENTITY.txt does not name the official beta.19 version'
grep -Fq 'rustc=rustc 1.95.0 ' "$IDENTITY" || fail 'IDENTITY.txt does not record Rust 1.95.0'
grep -Fq 'cargo=cargo 1.95.0 ' "$IDENTITY" || fail 'IDENTITY.txt does not record Cargo 1.95.0'
grep -Fq 'profile=release' "$IDENTITY" || fail 'IDENTITY.txt does not record the release profile'
grep -Fq 'build=CARGO_BUILD_JOBS=8 cargo +1.95.0 build --release --locked --lib' "$IDENTITY" ||
	fail 'IDENTITY.txt does not record the locked release build'
grep -Fq "cargo_lock_sha256=$EXPECTED_CARGO_LOCK_SHA" "$IDENTITY" || fail 'IDENTITY.txt does not record the Cargo.lock digest'
grep -Fq "source_archive_sha256=$EXPECTED_ARCHIVE_SHA" "$IDENTITY" || fail 'IDENTITY.txt does not record the C source archive digest'
grep -Fq "$EXPECTED_LIBRARY_SHA" "$IDENTITY" || fail 'IDENTITY.txt does not record the expected library digest'
grep -Fq "$EXPECTED_HEADER_SHA" "$IDENTITY" || fail 'IDENTITY.txt does not record the expected C header digest'
grep -Fq "$EXPECTED_CPP_HEADER_SHA" "$IDENTITY" || fail 'IDENTITY.txt does not record the expected C++ header digest'

cp -- "$IDENTITY" "$EVIDENCE_DIR/IDENTITY.txt"
cp -- "$NATIVE_SUMS" "$EVIDENCE_DIR/native-SHA256SUMS.txt"
printf '%s\n' "$library_sha_before" >"$EVIDENCE_DIR/library-sha256-before.txt"
sha256sum -- "$LIBRARY" "$HEADER" "$CPP_HEADER" "$CARGO_LOCK" "$IDENTITY" "$NATIVE_SUMS" >"$EVIDENCE_DIR/hashes-before.txt"
stat -c '%n|%s|%y' "$LIBRARY" "$HEADER" "$CPP_HEADER" "$CARGO_LOCK" "$IDENTITY" "$NATIVE_SUMS" >"$EVIDENCE_DIR/artifact-stats-before.txt"

git -C "$SOURCE_ROOT" show -s --format='commit %H%ntree %T%nparent %P%nauthor %an <%ae> %ad%ncommitter %cn <%ce> %cd%nsubject %s' --date=iso-strict HEAD \
	>"$EVIDENCE_DIR/source-identity.txt"
printf '%s\n' TestFTSPreparedIndexOnlyExcludesUnindexedFragment >"$EVIDENCE_DIR/selectors.txt"

command_text="cd $(printf '%q' "$SOURCE_ROOT") && env CGO_ENABLED=0 GOTOOLCHAIN=local GOENV=off LANCE_LIB_PATH=$(printf '%q' "$LIBRARY") $(printf '%q' "$GO_BIN") test -json -count=1 -tags lance_external_lib -run $(printf '%q' "$SELECTOR") ."
printf '%s\n' "$command_text" >"$EVIDENCE_DIR/command.txt"

{
	printf 'timestamp_utc=%s\n' "$(date -u +%FT%TZ)"
	printf 'source_root=%s\n' "$SOURCE_ROOT"
	printf 'source_head=%s\n' "$source_head"
	printf 'source_tree=%s\n' "$source_tree"
	printf 'candidate_source=%s\n' "$CANDIDATE_SOURCE"
	printf 'core_pin=%s\n' "$CORE_PIN"
	printf 'native_version=%s\n' "$NATIVE_VERSION"
	printf 'go_bin=%s\n' "$GO_BIN"
	printf 'go_version=%s\n' "$go_version"
	printf 'uname_s=%s\n' "$(uname -s)"
	printf 'uname_r=%s\n' "$(uname -r)"
	printf 'uname_m=%s\n' "$(uname -m)"
	printf 'glibc=%s\n' "$(getconf GNU_LIBC_VERSION 2>/dev/null || printf unknown)"
	printf 'git_path=%s\n' "$(command -v git)"
	printf 'git_version=%s\n' "$(git --version)"
	printf 'python3_path=%s\n' "$(command -v python3)"
	printf 'python3_version=%s\n' "$(python3 --version 2>&1)"
	printf 'goos=%s\n' "$go_os"
	printf 'goarch=%s\n' "$go_arch"
	printf 'goversion=%s\n' "$(env CGO_ENABLED=0 GOTOOLCHAIN=local GOENV=off "$GO_BIN" env GOVERSION)"
	printf 'cgo_enabled=%s\n' "$go_cgo"
	printf 'gotoolchain=%s\n' "$go_toolchain"
	printf 'configured_goenv=off\n'
	printf 'gowork=%s\n' "$go_work"
	printf 'gomod=%s\n' "$go_mod"
	printf 'gopath=%s\n' "$(env CGO_ENABLED=0 GOTOOLCHAIN=local GOENV=off "$GO_BIN" env GOPATH)"
	printf 'gomodcache=%s\n' "$(env CGO_ENABLED=0 GOTOOLCHAIN=local GOENV=off "$GO_BIN" env GOMODCACHE)"
	printf 'gocache=%s\n' "$(env CGO_ENABLED=0 GOTOOLCHAIN=local GOENV=off "$GO_BIN" env GOCACHE)"
} >"$EVIDENCE_DIR/environment.txt" 2>&1

gofmt_status=0
(
	cd "$SOURCE_ROOT" || exit 70
	env CGO_ENABLED=0 GOTOOLCHAIN=local GOENV=off "$GOFMT_BIN" -d dataset_fts.go dataset_fts_test.go
) >"$EVIDENCE_DIR/raw/gofmt.stdout" 2>"$EVIDENCE_DIR/raw/gofmt.stderr"
gofmt_status=$?
printf '%s\n' "$gofmt_status" >"$EVIDENCE_DIR/raw/gofmt.exit-code"

cp -- "$LAUNCH_LOG" "$EVIDENCE_DIR/launch.txt"
sha256sum -- "$0" "$LAUNCHER" "$FINALIZER" >"$EVIDENCE_DIR/driver-and-launcher-sha256.txt"

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
sha256sum -- "$LIBRARY" "$HEADER" "$CPP_HEADER" "$CARGO_LOCK" "$IDENTITY" "$NATIVE_SUMS" >"$EVIDENCE_DIR/hashes-after.txt"
stat -c '%n|%s|%y' "$LIBRARY" "$HEADER" "$CPP_HEADER" "$CARGO_LOCK" "$IDENTITY" "$NATIVE_SUMS" >"$EVIDENCE_DIR/artifact-stats-after.txt"
if [[ "$library_sha_after" != "$library_sha_before" ]]; then
	printf 'library sha256 changed: before=%s after=%s\n' "$library_sha_before" "$library_sha_after" \
		>"$EVIDENCE_DIR/postflight-error.txt"
	postflight_status=1
fi
if ! cmp -s "$EVIDENCE_DIR/hashes-before.txt" "$EVIDENCE_DIR/hashes-after.txt"; then
	printf 'artifact digests changed during execution\n' >>"$EVIDENCE_DIR/postflight-error.txt"
	postflight_status=1
fi

git -C "$SOURCE_ROOT" status --porcelain=v1 --untracked-files=all >"$EVIDENCE_DIR/git-status-after.txt" ||
	postflight_status=1
if [[ -s "$EVIDENCE_DIR/git-status-after.txt" ]]; then
	printf 'source worktree changed during execution\n' >>"$EVIDENCE_DIR/postflight-error.txt"
	postflight_status=1
fi

python3 -B "$FINALIZER" "$EVIDENCE_DIR" "$test_status" "$gofmt_status" "$postflight_status" \
	>"$EVIDENCE_DIR/finalizer.stdout" 2>"$EVIDENCE_DIR/finalizer.stderr"
finalizer_status=$?
printf '%s\n' "$finalizer_status" >"$EVIDENCE_DIR/finalizer.exit-code"

if [[ ! -f "$EVIDENCE_DIR/RESULT.json" || ! -f "$EVIDENCE_DIR/RESULT.md" ]]; then
	printf 'finalizer did not produce both result files\n' >"$EVIDENCE_DIR/finalize-error.txt"
	exit 4
fi

(
	cd "$EVIDENCE_DIR" || exit 70
	find . -type f ! -name SHA256SUMS -printf '%P\n' |
		LC_ALL=C sort |
		xargs -r sha256sum >SHA256SUMS
	sha256sum --check SHA256SUMS >/dev/null
) || {
	printf 'checksum manifest generation or verification failed\n' >"$EVIDENCE_DIR/manifest-error.txt"
	exit 5
}

if [[ "$test_status" -ne 0 ]]; then
	exit "$test_status"
fi
if [[ "$postflight_status" -ne 0 ]]; then
	exit 3
fi
if [[ "$finalizer_status" -ne 0 ]]; then
	exit "$finalizer_status"
fi
exit 0
