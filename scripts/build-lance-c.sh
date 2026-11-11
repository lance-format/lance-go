#!/usr/bin/env bash
# Build liblance_c from the exact lance-c commit pinned by
# internal/ffi/upstream.go, then install it into a local prefix.
#
# Requires: git, cmake >= 3.22, cargo (Rust >= 1.94.0), network access for
# Cargo crates and CMake's Corrosion dependency, and a SHA-256 tool.
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
upstream_go="$repo_root/internal/ffi/upstream.go"

usage() {
	cat <<'EOF'
Usage: scripts/build-lance-c.sh [--out DIR]

Builds lance-c from the commit pinned in internal/ffi/upstream.go and
installs headers and libraries under DIR (default: <repo>/dist/lance-c).

Environment:
  LANCE_C_WORK_DIR      checkout and build cache
                        (default: ${XDG_CACHE_HOME:-$HOME/.cache}/lance-c-build)
  LANCE_C_UPSTREAM_URL  git remote to clone (default: pinned GitHub repository)
  JOBS                  parallel build jobs (default: CPU count)

The upstream checkout and build directory are reused across runs. DIR must
not already exist; choose a new output prefix to preserve an earlier build.
EOF
}

die() {
	printf 'build-lance-c: %s\n' "$*" >&2
	exit 1
}

out_dir="$repo_root/dist/lance-c"
while [[ $# -gt 0 ]]; do
	case "$1" in
	--out)
		[[ $# -ge 2 ]] || {
			usage >&2
			exit 2
		}
		out_dir=$2
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		printf 'build-lance-c: unknown argument %q\n' "$1" >&2
		usage >&2
		exit 2
		;;
	esac
done

[[ ! -e "$out_dir" && ! -L "$out_dir" ]] || die "output already exists: $out_dir; choose a new --out directory"

for cmd in git cmake cargo; do
	command -v "$cmd" >/dev/null 2>&1 || die "required command not found: $cmd"
done

sha256_file() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		die "need sha256sum or shasum to verify the pinned header"
	fi
}

read_pin() {
	local name=$1 value count
	count=$(grep -c "^[[:space:]]*$name[[:space:]]*=" "$upstream_go" || true)
	[[ $count -eq 1 ]] || die "expected exactly one $name in $upstream_go, found $count"
	value=$(sed -n "s/^[[:space:]]*$name[[:space:]]*=[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p" "$upstream_go")
	[[ -n $value ]] || die "could not read $name from $upstream_go"
	printf '%s' "$value"
}

upstream_url=${LANCE_C_UPSTREAM_URL:-$(read_pin upstreamRepo)}
upstream_commit=$(read_pin upstreamCommit)
header_sha256=$(read_pin upstreamHeaderSHA256)
work_dir=${LANCE_C_WORK_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/lance-c-build}
jobs=${JOBS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)}

[[ $upstream_commit =~ ^[0-9a-f]{40}$ ]] || die "pinned upstreamCommit is not a full SHA-1: $upstream_commit"
[[ $header_sha256 =~ ^[0-9a-f]{64}$ ]] || die "pinned upstreamHeaderSHA256 is not a SHA-256: $header_sha256"

src_dir="$work_dir/src"
build_dir="$work_dir/build"

mkdir -p "$work_dir"
stage_dir=$(mktemp -d "$work_dir/stage.XXXXXX")
trap 'rm -rf -- "$stage_dir"' EXIT

if [[ ! -d "$src_dir/.git" ]]; then
	printf 'build-lance-c: cloning %s into %s\n' "$upstream_url" "$src_dir"
	git clone "$upstream_url" "$src_dir" || die "git clone failed"
fi

printf 'build-lance-c: fetching and checking out %s\n' "$upstream_commit"
git -C "$src_dir" fetch --tags --prune origin || die "git fetch failed"
git -C "$src_dir" checkout --detach "$upstream_commit" ||
	die "cannot check out the pinned commit; remove local changes in $src_dir or set LANCE_C_WORK_DIR to a fresh directory"

actual_commit=$(git -C "$src_dir" rev-parse HEAD)
[[ $actual_commit == "$upstream_commit" ]] || die "source checkout is at $actual_commit, want $upstream_commit"

if [[ -n $(git -C "$src_dir" status --porcelain) ]]; then
	git -C "$src_dir" status --porcelain >&2
	die "source checkout is dirty; clean it or set LANCE_C_WORK_DIR to a fresh directory"
fi

source_header="$src_dir/include/lance/lance.h"
[[ -f $source_header ]] || die "pinned commit does not contain include/lance/lance.h"
actual_header_sha=$(sha256_file "$source_header")
[[ $actual_header_sha == "$header_sha256" ]] ||
	die "header sha256 at $upstream_commit is $actual_header_sha, want $header_sha256; update the pin in $upstream_go"

printf 'build-lance-c: building with %s job(s); this compiles all of Lance and takes a while\n' "$jobs"
cmake -S "$src_dir" -B "$build_dir" -DCMAKE_BUILD_TYPE=Release
cmake --build "$build_dir" --parallel "$jobs"
cmake --install "$build_dir" --prefix "$stage_dir"

lib_path=$(find "$stage_dir" -type f \( -name liblance_c.so -o -name liblance_c.dylib \) -print -quit)
[[ -n $lib_path ]] || die "build succeeded but no liblance_c shared library was installed"
installed_header=$(find "$stage_dir" -type f -path '*/include/lance/lance.h' -print -quit)
[[ -n $installed_header ]] || die "build succeeded but include/lance/lance.h was not installed"

actual_header_sha=$(sha256_file "$installed_header")
[[ $actual_header_sha == "$header_sha256" ]] ||
	die "installed header sha256 is $actual_header_sha, want $header_sha256"

[[ ! -e "$out_dir" && ! -L "$out_dir" ]] || die "output appeared during build: $out_dir"
mkdir -p "$(dirname "$out_dir")"
mv "$stage_dir" "$out_dir"

lib_path=$(find "$out_dir" -type f \( -name liblance_c.so -o -name liblance_c.dylib \) -print -quit)
installed_header="$out_dir/include/lance/lance.h"

cat <<EOF
build-lance-c: installed lance-c $upstream_commit
  library: $lib_path
  header:  $installed_header
  sha256(liblance_c): $(sha256_file "$lib_path")

Next steps:
  export LANCE_LIB_PATH="$lib_path"
  go test ./...
  LANCE_C_HEADER="$installed_header" go run ./scripts/check-abi-symbols.go
EOF
