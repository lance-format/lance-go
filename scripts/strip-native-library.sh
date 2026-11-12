#!/usr/bin/env bash
# Copy an accepted liblance_c, strip the copy, and prove the source is
# unchanged. The source library remains owned by the install cache.
#
# Usage:
#   scripts/strip-native-library.sh --library PATH --header PATH \
#     --target GOOS/GOARCH --out DIR
#
# The target must be one of linux/amd64, linux/arm64, darwin/amd64, or
# darwin/arm64. Linux uses `strip --strip-unneeded`; macOS uses `strip -x`.
# The output directory must not already exist and must be outside the
# repository. Only the copy under <out>/library is modified.
#
# The helper records the source and stripped sizes and SHA-256s, the strip
# tool path, hash, version and argv, and the header hash. It fails if the
# source library's bytes or hash change. It does not rewrite dependencies,
# install names, or RPATH; callers revalidate the stripped copy separately.
set -euo pipefail
export LC_ALL=C

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

die() {
	printf 'strip-native-library: %s\n' "$*" >&2
	exit 1
}

usage() {
	cat <<'EOF'
Usage: scripts/strip-native-library.sh --library PATH --header PATH \
         --target GOOS/GOARCH --out DIR

Copies an accepted liblance_c into a new output directory, strips only that
copy with the target-appropriate tool, and verifies the source library is
unchanged. The output must be outside the repository and must not exist.

Required:
  --library PATH          accepted liblance_c.so or liblance_c.dylib
  --header PATH           matching include/lance/lance.h
  --target GOOS/GOARCH    linux or darwin on amd64 or arm64
  --out DIR               new output directory; its library/ child is created

Evidence is written to stdout. Dependencies, install names, and RPATH are not
modified.
EOF
}

sha256_file() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		die "need sha256sum or shasum"
	fi
}

absolute_file() {
	local path=$1 dir
	[[ -f $path && ! -L $path ]] || die "missing or not a regular file: $path"
	dir=$(cd "$(dirname "$path")" && pwd -P) || die "cannot resolve directory for: $path"
	printf '%s/%s' "$dir" "$(basename "$path")"
}

library=""
header=""
target=""
out_dir=""

while [[ $# -gt 0 ]]; do
	case "$1" in
	--library)
		[[ $# -ge 2 ]] || die "--library requires a path"
		library=$2
		shift 2
		;;
	--header)
		[[ $# -ge 2 ]] || die "--header requires a path"
		header=$2
		shift 2
		;;
	--target)
		[[ $# -ge 2 ]] || die "--target requires GOOS/GOARCH"
		target=$2
		shift 2
		;;
	--out)
		[[ $# -ge 2 ]] || die "--out requires a directory"
		out_dir=$2
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		printf 'strip-native-library: unknown argument: %s\n' "$1" >&2
		usage >&2
		exit 2
		;;
	esac
done

[[ -n $library && -n $header && -n $target && -n $out_dir ]] || {
	usage >&2
	exit 2
}
[[ $target == */* ]] || die "--target must be GOOS/GOARCH, got: $target"
goos=${target%%/*}
goarch=${target#*/}

case "$goos/$goarch" in
linux/amd64 | linux/arm64)
	expected_name=liblance_c.so
	strip_flags=(--strip-unneeded)
	;;
darwin/amd64 | darwin/arm64)
	expected_name=liblance_c.dylib
	strip_flags=(-x)
	;;
*)
	die "unsupported target: $target"
	;;
esac

library=$(absolute_file "$library")
header=$(absolute_file "$header")
[[ $(basename "$library") == "$expected_name" ]] ||
	die "library basename is $(basename "$library"), want $expected_name for $target"

source_bytes_before=$(wc -c < "$library" | tr -d ' ')
source_sha_before=$(sha256_file "$library")
header_sha=$(sha256_file "$header")

case "$out_dir" in
/*) ;;
*) out_dir="$PWD/$out_dir" ;;
esac
case "$out_dir" in
"$repo_root" | "$repo_root"/*)
	die "refusing to write inside the repository: $out_dir"
	;;
esac
[[ ! -e $out_dir && ! -L $out_dir ]] || die "output already exists: $out_dir; choose a new --out directory"
mkdir -p "$out_dir/library"
out_dir=$(cd "$out_dir" && pwd -P)
case "$out_dir" in
"$repo_root" | "$repo_root"/*)
	die "refusing to write inside the repository: $out_dir"
	;;
esac

out_library="$out_dir/library/$expected_name"
cp "$library" "$out_library"
cmp -s "$library" "$out_library" || die "copied library differs from the source before stripping"

strip_tool=$(command -v strip || true)
[[ -n $strip_tool ]] || die "required command not found: strip"
strip_tool_sha=$(sha256_file "$strip_tool")
strip_version=$("$strip_tool" --version 2>/dev/null | head -n 1 || true)
[[ -n $strip_version ]] || strip_version=unavailable

strip_argv=("$strip_tool" "${strip_flags[@]}" "$out_library")
strip_argv_string=""
for arg in "${strip_argv[@]}"; do
	printf -v quoted '%q' "$arg"
	strip_argv_string="${strip_argv_string}${quoted} "
done
strip_argv_string=${strip_argv_string% }

"${strip_argv[@]}"

[[ -f $out_library && ! -L $out_library ]] || die "stripped library is missing or not a regular file: $out_library"
stripped_bytes=$(wc -c < "$out_library" | tr -d ' ')
[[ $stripped_bytes -gt 0 ]] || die "stripped library is empty: $out_library"
stripped_sha=$(sha256_file "$out_library")

source_bytes_after=$(wc -c < "$library" | tr -d ' ')
source_sha_after=$(sha256_file "$library")
[[ $source_sha_after == "$source_sha_before" ]] ||
	die "source library sha256 changed from $source_sha_before to $source_sha_after"
[[ $source_bytes_after == "$source_bytes_before" ]] ||
	die "source library size changed from $source_bytes_before to $source_bytes_after"

printf 'packaging=stripped-production-candidate\n'
printf 'target=%s\n' "$target"
printf 'source_library=%s\n' "$library"
printf 'source_library_bytes=%s\n' "$source_bytes_before"
printf 'source_library_sha256=%s\n' "$source_sha_before"
printf 'source_library_sha256_after=%s\n' "$source_sha_after"
printf 'source_library_bytes_after=%s\n' "$source_bytes_after"
printf 'source_unchanged=verified\n'
printf 'header=%s\n' "$header"
printf 'header_sha256=%s\n' "$header_sha"
printf 'stripped_library=%s\n' "$out_library"
printf 'stripped_library_bytes=%s\n' "$stripped_bytes"
printf 'stripped_library_sha256=%s\n' "$stripped_sha"
printf 'stripped_size_delta_bytes=%s\n' "$((stripped_bytes - source_bytes_before))"
printf 'strip_tool=%s\n' "$strip_tool"
printf 'strip_tool_sha256=%s\n' "$strip_tool_sha"
printf 'strip_tool_version=%s\n' "$strip_version"
printf 'strip_argv=%s\n' "$strip_argv_string"
