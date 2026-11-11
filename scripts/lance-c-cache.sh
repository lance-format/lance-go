#!/usr/bin/env bash
# Save and validate the reusable subset of an installed lance-c build.
#
# The install cache contains the shared library, the installed public
# headers, a manifest with the native pin and file hashes, and the original
# build provenance. Rust target trees and static archives are deliberately
# excluded. CI restores the entry only for an exact key and accepts a hit
# only after the manifest, the current pin, and every saved hash match.
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
upstream_go="$repo_root/internal/ffi/upstream.go"
build_script="$repo_root/scripts/build-lance-c.sh"
schema=lance-c-install-cache-v1

usage() {
	cat <<'EOF'
Usage:
  scripts/lance-c-cache.sh write --install DIR --cache DIR \
    --provenance FILE --cache-key KEY
  scripts/lance-c-cache.sh verify --cache DIR --cache-key KEY

write copies the installed liblance_c shared library and the installed lance
headers into a new cache directory. verify checks an existing cache directory
against the pin in internal/ffi/upstream.go. Both commands print the validated
library, header, and native commit.

Cache layout:
  lib/<library-name>
  include/lance/ (the installed .h and .hpp files)
  manifest.txt
  build-provenance.txt
EOF
}

die() {
	printf 'lance-c-cache: %s\n' "$*" >&2
	exit 1
}

# Keep this parser in sync with scripts/build-lance-c.sh and
# scripts/sync-lance-c.sh; all three read the pin from the same file.
read_pin() {
	local name=$1 value count
	count=$(grep -c "^[[:space:]]*${name}[[:space:]]*=" "$upstream_go" || true)
	[[ $count -eq 1 ]] || die "expected exactly one $name in $upstream_go, found $count"
	value=$(sed -n "s/^[[:space:]]*${name}[[:space:]]*=[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p" "$upstream_go")
	[[ -n $value ]] || die "could not read $name from $upstream_go"
	printf '%s' "$value"
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

sha256_stdin() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 | awk '{print $1}'
	else
		die "need sha256sum or shasum"
	fi
}

read_provenance_commit() {
	local file=$1 value count
	count=$(grep -c '^source_commit: ' "$file" || true)
	[[ $count -eq 1 ]] || die "build provenance must contain exactly one source_commit, found $count"
	value=$(sed -n 's/^source_commit: //p' "$file")
	[[ -n $value ]] || die "build provenance has an empty source_commit"
	printf '%s' "$value"
}

headers_sha256() {
	local dir=$1 file hash
	[[ -d $dir && ! -L $dir ]] || die "headers directory missing or not a directory: $dir"
	[[ -z $(find "$dir" -type l -print -quit) ]] || die "headers directory contains a symlink: $dir"
	(
		cd "$dir"
		find . -type f \( -name '*.h' -o -name '*.hpp' \) -print | LC_ALL=C sort |
			while IFS= read -r file; do
				hash=$(sha256_file "$file") || die "could not hash header: $file"
				printf '%s  %s\n' "$hash" "${file#./}"
			done
	) | sha256_stdin
}

command=${1:-}
[[ -n $command ]] || {
	usage >&2
	exit 2
}
shift
case "$command" in
write | verify) ;;
-h | --help)
	usage
	exit 0
	;;
*)
	printf 'lance-c-cache: unknown command %q\n' "$command" >&2
	usage >&2
	exit 2
	;;
esac

install=""
cache=""
provenance=""
cache_key=""
while [[ $# -gt 0 ]]; do
	case "$1" in
	--install)
		[[ $# -ge 2 ]] || {
			usage >&2
			exit 2
		}
		install=$2
		shift 2
		;;
	--cache)
		[[ $# -ge 2 ]] || {
			usage >&2
			exit 2
		}
		cache=$2
		shift 2
		;;
	--provenance)
		[[ $# -ge 2 ]] || {
			usage >&2
			exit 2
		}
		provenance=$2
		shift 2
		;;
	--cache-key)
		[[ $# -ge 2 ]] || {
			usage >&2
			exit 2
		}
		cache_key=$2
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		printf 'lance-c-cache: unknown argument %q\n' "$1" >&2
		usage >&2
		exit 2
		;;
	esac
done

[[ -n $cache ]] || die "--cache is required"
[[ -n $cache_key ]] || die "--cache-key is required"
[[ $cache_key != *$'\n'* ]] || die "--cache-key must fit on one line"
case "$cache" in
/*) ;;
*) die "--cache must be an absolute path" ;;
esac

if [[ $command == write ]]; then
	[[ -n $install ]] || die "--install is required for write"
	[[ -n $provenance ]] || die "--provenance is required for write"
	case "$install" in
	/*) ;;
	*) die "--install must be an absolute path" ;;
	esac
	case "$provenance" in
	/*) ;;
	*) die "--provenance must be an absolute path" ;;
	esac
	[[ ! -e $cache && ! -L $cache ]] || die "cache directory already exists: $cache"
else
	[[ -z $install ]] || die "--install is only valid for write"
	[[ -z $provenance ]] || die "--provenance is only valid for write"
	[[ -d $cache && ! -L $cache ]] || die "cache directory missing: $cache"
fi

manifest_value() {
	local key=$1 count value
	count=$(grep -c "^$key=" "$cache/manifest.txt" || true)
	[[ $count -eq 1 ]] || die "manifest must contain exactly one $key, found $count"
	value=$(sed -n "s/^$key=//p" "$cache/manifest.txt")
	printf '%s' "$value"
}

verify_entry() {
	local manifest="$cache/manifest.txt"
	local schema_value manifest_key manifest_repository pin_commit pin_header library_name
	local manifest_source_commit manifest_library_sha manifest_header_sha manifest_headers_sha
	local manifest_upstream_sha manifest_build_script_sha
	local native_commit native_header provenance_commit library header library_sha header_sha
	local headers_dir headers_sha

	[[ -f $manifest && ! -L $manifest ]] || die "cache manifest missing or not a regular file: $manifest"
	[[ -f $cache/build-provenance.txt && ! -L $cache/build-provenance.txt ]] ||
		die "cached build provenance missing or not a regular file"
	[[ -s $cache/build-provenance.txt ]] || die "cached build provenance is empty"
	grep -q '^repository_commit: ' "$cache/build-provenance.txt" ||
		die "cached build provenance has no repository_commit"
	grep -q '^workflow_run: ' "$cache/build-provenance.txt" ||
		die "cached build provenance has no workflow_run"

	schema_value=$(manifest_value schema)
	manifest_key=$(manifest_value cache_key)
	manifest_repository=$(manifest_value native_repository)
	pin_commit=$(manifest_value native_commit)
	pin_header=$(manifest_value native_header_sha256)
	manifest_source_commit=$(manifest_value source_commit)
	library_name=$(manifest_value library_name)
	manifest_library_sha=$(manifest_value library_sha256)
	manifest_header_sha=$(manifest_value header_sha256)
	manifest_headers_sha=$(manifest_value headers_sha256)
	manifest_upstream_sha=$(manifest_value upstream_sha256)
	manifest_build_script_sha=$(manifest_value build_script_sha256)

	[[ $schema_value == "$schema" ]] || die "unsupported cache schema: $schema_value"
	[[ $manifest_key == "$cache_key" ]] || die "cache entry belongs to a different key"
	[[ $manifest_repository == "$(read_pin upstreamRepo)" ]] ||
		die "cached native repository does not match the pin"

	native_commit=$(read_pin upstreamCommit)
	native_header=$(read_pin upstreamHeaderSHA256)
	[[ $pin_commit == "$native_commit" ]] ||
		die "cached native commit does not match the pin in $upstream_go"
	[[ $manifest_source_commit == "$native_commit" ]] ||
		die "cached source_commit does not match the native pin"
	[[ $pin_header == "$native_header" ]] ||
		die "cached native header pin does not match $upstream_go"
	provenance_commit=$(read_provenance_commit "$cache/build-provenance.txt")
	[[ $provenance_commit == "$native_commit" ]] ||
		die "cached build provenance source_commit does not match the native pin"
	[[ $(sha256_file "$upstream_go") == "$manifest_upstream_sha" ]] ||
		die "cached pin file hash does not match $upstream_go"
	[[ $(sha256_file "$build_script") == "$manifest_build_script_sha" ]] ||
		die "cached build script hash does not match $build_script"

	case "$library_name" in
	liblance_c.so | liblance_c.dylib) ;;
	*) die "unexpected cached library name: $library_name" ;;
	esac
	library="$cache/lib/$library_name"
	header="$cache/include/lance/lance.h"
	headers_dir="$cache/include/lance"
	[[ -f $library && ! -L $library ]] || die "cached library missing or not a regular file: $library"
	[[ -f $header && ! -L $header ]] || die "cached header missing or not a regular file: $header"

	library_sha=$(sha256_file "$library")
	header_sha=$(sha256_file "$header")
	headers_sha=$(headers_sha256 "$headers_dir")
	[[ $library_sha == "$manifest_library_sha" ]] ||
		die "cached library hash does not match its manifest"
	[[ $header_sha == "$manifest_header_sha" ]] ||
		die "cached header hash does not match its manifest"
	[[ $header_sha == "$native_header" ]] || die "cached header does not match the native pin"
	[[ $headers_sha == "$manifest_headers_sha" ]] ||
		die "cached headers hash does not match its manifest"

	printf 'library=%s\n' "$library"
	printf 'header=%s\n' "$header"
	printf 'commit=%s\n' "$native_commit"
}

write_entry() {
	local library header library_sha header_sha headers_sha native_commit source_commit

	[[ -d $install ]] || die "installed prefix missing: $install"
	[[ -d $install/lib ]] || die "installed library directory missing: $install/lib"
	[[ -f $provenance && ! -L $provenance ]] || die "build provenance missing or not a regular file: $provenance"
	[[ -s $provenance ]] || die "build provenance is empty: $provenance"
	grep -q '^repository_commit: ' "$provenance" || die "build provenance has no repository_commit"
	grep -q '^workflow_run: ' "$provenance" || die "build provenance has no workflow_run"
	native_commit=$(read_pin upstreamCommit)
	source_commit=$(read_provenance_commit "$provenance")
	[[ $source_commit == "$native_commit" ]] ||
		die "build provenance source_commit does not match the native pin"

	library=$(find "$install/lib" -maxdepth 1 -type f \( -name liblance_c.so -o -name liblance_c.dylib \) -print -quit)
	[[ -n $library ]] || die "no installed liblance_c under $install/lib"
	header="$install/include/lance/lance.h"
	[[ -f $header ]] || die "installed header missing: $header"
	[[ $(sha256_file "$header") == "$(read_pin upstreamHeaderSHA256)" ]] ||
		die "installed header does not match the native pin"
	[[ -d $install/include/lance ]] || die "installed headers directory missing: $install/include/lance"

	library_sha=$(sha256_file "$library")
	header_sha=$(sha256_file "$header")
	headers_sha=$(headers_sha256 "$install/include/lance")

	mkdir -p "$cache/lib" "$cache/include/lance"
	cp -- "$library" "$cache/lib/$(basename "$library")"
	cp -a "$install/include/lance/." "$cache/include/lance/"
	cp -- "$provenance" "$cache/build-provenance.txt"

	{
		printf 'schema=%s\n' "$schema"
		printf 'native_repository=%s\n' "$(read_pin upstreamRepo)"
		printf 'native_commit=%s\n' "$native_commit"
		printf 'source_commit=%s\n' "$source_commit"
		printf 'native_header_sha256=%s\n' "$(read_pin upstreamHeaderSHA256)"
		printf 'library_name=%s\n' "$(basename "$library")"
		printf 'library_sha256=%s\n' "$library_sha"
		printf 'header_sha256=%s\n' "$header_sha"
		printf 'headers_sha256=%s\n' "$headers_sha"
		printf 'upstream_sha256=%s\n' "$(sha256_file "$upstream_go")"
		printf 'build_script_sha256=%s\n' "$(sha256_file "$build_script")"
		printf 'cache_key=%s\n' "$cache_key"
	} > "$cache/manifest.txt"
}

if [[ $command == write ]]; then
	write_entry
fi
verify_entry
