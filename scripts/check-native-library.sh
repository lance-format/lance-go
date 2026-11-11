#!/usr/bin/env bash
# Statically validate an installed liblance_c against the repository pin and
# the host target. The library is only inspected; it is never loaded.
#
# Usage:
#   scripts/check-native-library.sh --library PATH --header PATH \
#     --target GOOS/GOARCH
#
# The target must be one of linux/amd64, linux/arm64, darwin/amd64, or
# darwin/arm64. The checks are:
#   - the header SHA-256 matches internal/ffi/upstream.go
#   - the library basename, object format, and architecture match the target
#   - direct dependencies resolve, with no build-prefix or Homebrew paths
#   - the libc/macOS deployment floor is not newer than the runner
#   - every symbol bound by internal/ffi/bindings.go is exported
#
# Export and dependency counts are reported as observed. This script does not
# claim that the library exports only lance symbols.
set -euo pipefail
export LC_ALL=C

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
upstream_go="$repo_root/internal/ffi/upstream.go"
bindings_go="$repo_root/internal/ffi/bindings.go"

die() {
	printf 'check-native-library: %s\n' "$*" >&2
	exit 1
}

usage() {
	cat <<'EOF'
Usage: scripts/check-native-library.sh --library PATH --header PATH --target GOOS/GOARCH

Statically checks one installed lance-c shared library against the pin in
internal/ffi/upstream.go and the host platform. The library is never loaded.
EOF
}

# Keep this parser in sync with scripts/build-lance-c.sh, scripts/lance-c-cache.sh,
# and scripts/sync-lance-c.sh; all read the pin from the same file.
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

# Print 1 when $1 is greater than $2, -1 when smaller, and 0 when equal.
vercmp() {
	awk -v a="$1" -v b="$2" 'BEGIN {
		n = split(a, av, ".")
		m = split(b, bv, ".")
		limit = n > m ? n : m
		for (i = 1; i <= limit; i++) {
			ai = (i <= n) ? av[i] + 0 : 0
			bi = (i <= m) ? bv[i] + 0 : 0
			if (ai > bi) { print 1; exit }
			if (ai < bi) { print -1; exit }
		}
		print 0
	}'
}

count_names() {
	sed '/^$/d' | wc -l | tr -d ' '
}

library=""
header=""
target=""

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
	-h | --help)
		usage
		exit 0
		;;
	*)
		printf 'check-native-library: unknown argument: %s\n' "$1" >&2
		usage >&2
		exit 2
		;;
	esac
done

[[ -n $library && -n $header && -n $target ]] || {
	usage >&2
	exit 2
}
[[ $target == */* ]] || die "--target must be GOOS/GOARCH, got: $target"
goos=${target%%/*}
goarch=${target#*/}
case "$goos/$goarch" in
linux/amd64 | linux/arm64 | darwin/amd64 | darwin/arm64) ;;
*) die "unsupported target: $target" ;;
esac

[[ -f $library && ! -L $library ]] || die "library missing or not a regular file: $library"
[[ -f $header && ! -L $header ]] || die "header missing or not a regular file: $header"

header_sha=$(sha256_file "$header")
expected_header_sha=$(read_pin upstreamHeaderSHA256)
[[ $header_sha == "$expected_header_sha" ]] ||
	die "header sha256 is $header_sha, want pinned $expected_header_sha"

case "$goos" in
linux) expected_name=liblance_c.so ;;
darwin) expected_name=liblance_c.dylib ;;
esac
[[ $(basename "$library") == "$expected_name" ]] ||
	die "library basename is $(basename "$library"), want $expected_name for $target"

library_bytes=$(wc -c < "$library" | tr -d ' ')
library_sha=$(sha256_file "$library")

printf 'target=%s\n' "$target"
printf 'library=%s\n' "$library"
printf 'library_bytes=%s\n' "$library_bytes"
printf 'library_sha256=%s\n' "$library_sha"
printf 'header=%s\n' "$header"
printf 'header_sha256=%s\n' "$header_sha"
printf 'upstream_commit=%s\n' "$(read_pin upstreamCommit)"
printf 'upstream_header_sha256=%s\n' "$expected_header_sha"

bound_symbols=$(sed -n '/^func (b \*bindings) symbols() \[\]symbol {/,/^}$/p' "$bindings_go" |
	sed -n 's/.*{"\(lance_[A-Za-z0-9_]*\)".*/\1/p' |
	LC_ALL=C sort -u)
bound_count=$(printf '%s\n' "$bound_symbols" | count_names)
[[ $bound_count -ge 500 ]] ||
	die "parsed only $bound_count bound symbols from $bindings_go; the bindings literal changed"
printf 'bound_symbols=%s\n' "$bound_count"

missing_bound_symbols() {
	local actual=$1 missing preview
	missing=$(comm -23 <(printf '%s\n' "$bound_symbols") <(printf '%s\n' "$actual"))
	if [[ -n $missing ]]; then
		preview=$(printf '%s\n' "$missing" | head -n 10 | paste -sd, -)
		die "library is missing bound symbols ($preview)"
	fi
	printf 'missing_bound_symbols=none\n'
}

if [[ $goos == linux ]]; then
	command -v readelf >/dev/null 2>&1 || die "readelf is required for linux targets"
	command -v ldd >/dev/null 2>&1 || die "ldd is required for linux targets"

	elf_header=$(readelf -h "$library")
	elf_class=$(printf '%s\n' "$elf_header" | sed -n 's/^  Class:[[:space:]]*//p')
	elf_type=$(printf '%s\n' "$elf_header" | sed -n 's/^  Type:[[:space:]]*//p')
	elf_machine=$(printf '%s\n' "$elf_header" | sed -n 's/^  Machine:[[:space:]]*//p')
	[[ $elf_class == ELF64 ]] || die "library class is $elf_class, want ELF64"
	[[ $elf_type == *DYN* ]] || die "library type is $elf_type, want a shared object (DYN)"
	case "$goarch" in
	amd64) [[ $elf_machine == *X86-64* ]] || die "library machine is $elf_machine, want X86-64" ;;
	arm64) [[ $elf_machine == *AArch64* ]] || die "library machine is $elf_machine, want AArch64" ;;
	esac
	printf 'format=ELF64\narch=%s\n' "$goarch"

	dynamic=$(readelf -d "$library")
	if printf '%s\n' "$dynamic" | grep -Eq '\((RPATH|RUNPATH)\)'; then
		printf '%s\n' "$dynamic" | grep -E '\((RPATH|RUNPATH)\)' >&2
		die "library contains an RPATH or RUNPATH; it would not be relocatable"
	fi
	soname=$(printf '%s\n' "$dynamic" | sed -n 's/.*(SONAME).*\[\(.*\)\]/\1/p')
	needed=$(printf '%s\n' "$dynamic" | sed -n 's/.*(NEEDED).*\[\(.*\)\]/\1/p' | LC_ALL=C sort -u)
	[[ -n $needed ]] || die "library has no NEEDED entries"
	while IFS= read -r dependency; do
		[[ -n $dependency ]] || continue
		case "$dependency" in
		/*) die "library has an absolute NEEDED entry: $dependency" ;;
		esac
	done <<<"$needed"
	printf 'soname=%s\n' "$soname"
	printf 'dependencies=%s\n' "$(printf '%s\n' "$needed" | paste -sd, -)"

	ldd_output=$(ldd "$library" 2>&1) || die "ldd failed: $ldd_output"
	if printf '%s\n' "$ldd_output" | grep -Fq 'not found'; then
		printf '%s\n' "$ldd_output" >&2
		die "library has unresolved dependencies"
	fi
	printf 'resolved_dependencies=%s\n' "$(printf '%s\n' "$ldd_output" | sed '/^$/d' | count_names)"

	glibc_floor=0
	for version in $(readelf --version-info "$library" |
		sed -n 's/.*Name: GLIBC_\([0-9][0-9.]*\).*/\1/p' | LC_ALL=C sort -u); do
		if [[ $(vercmp "$version" "$glibc_floor") == 1 ]]; then
			glibc_floor=$version
		fi
	done
	glibc_runtime=$(ldd --version 2>/dev/null | head -n 1 | sed -n 's/.* \([0-9][0-9.]*\)$/\1/p' || true)
	if [[ -z $glibc_runtime ]]; then
		glibc_runtime=$(getconf GNU_LIBC_VERSION 2>/dev/null | awk '{print $2}' || true)
	fi
	[[ -n $glibc_runtime ]] || die "could not determine the runner glibc version"
	[[ $(vercmp "$glibc_floor" "$glibc_runtime") != 1 ]] ||
		die "library requires glibc $glibc_floor but the runner provides $glibc_runtime"
	printf 'glibc_floor=%s\n' "$glibc_floor"
	printf 'glibc_runtime=%s\n' "$glibc_runtime"
	cxx_versions=$(readelf --version-info "$library" |
		sed -n 's/.*Name: \(GLIBCXX_[0-9][0-9.]*\|CXXABI_[0-9][0-9.]*\).*/\1/p' |
		LC_ALL=C sort -u | paste -sd, - || true)
	printf 'cxx_symbol_versions=%s\n' "${cxx_versions:-none}"

	dynsym=$(readelf --dyn-syms --wide "$library")
	defined=$(printf '%s\n' "$dynsym" |
		awk '$1 ~ /^[0-9]+:$/ && $7 != "UND" && $8 != "" {print $8}' |
		sed 's/@.*//' | LC_ALL=C sort -u)
	[[ -n $defined ]] || die "library has no defined dynamic symbols"
	lance_exports=$(printf '%s\n' "$defined" | grep -E '^lance_' || true)
	defined_count=$(printf '%s\n' "$defined" | count_names)
	lance_count=$(printf '%s\n' "$lance_exports" | count_names)
	[[ $lance_count -gt 0 ]] || die "library exports no lance_ symbols"
	printf 'exported_defined_symbols=%s\n' "$defined_count"
	printf 'exported_lance_symbols=%s\n' "$lance_count"
	printf 'export_check=verified\n'
	missing_bound_symbols "$lance_exports"
else
	command -v otool >/dev/null 2>&1 || die "otool is required for darwin targets"
	command -v lipo >/dev/null 2>&1 || die "lipo is required for darwin targets"

	file_output=$(file -b "$library")
	[[ $file_output == *Mach-O*64-bit*shared\ library* ]] ||
		die "library is not a 64-bit Mach-O shared library: $file_output"
	case "$goarch" in
	amd64) [[ $file_output == *x86_64* ]] || die "library is not x86_64: $file_output" ;;
	arm64) [[ $file_output == *arm64* ]] || die "library is not arm64: $file_output" ;;
	esac
	lipo_output=$(lipo -info "$library")
	[[ $lipo_output == Non-fat* ]] || die "library is not a thin Mach-O: $lipo_output"
	otool_header=$(otool -hv "$library")
	printf '%s\n' "$otool_header" | grep -Fq 'DYLIB' ||
		die "library is not a Mach-O dylib: $(printf '%s\n' "$otool_header" | tail -n 1)"
	printf 'format=Mach-O64\narch=%s\n' "$goarch"

	install_name=$(otool -D "$library" | sed -n '2p')
	[[ -n $install_name ]] || die "library has no LC_ID_DYLIB install name"
	dependencies=$(otool -L "$library" | tail -n +2 | awk '{print $1}')
	[[ -n $dependencies ]] || die "library has no LC_LOAD_DYLIB entries"
	while IFS= read -r dependency; do
		[[ -n $dependency ]] || continue
		[[ $dependency == "$install_name" ]] && continue
		case "$dependency" in
		@rpath/* | @loader_path/* | @executable_path/*)
			die "library has a non-system load path: $dependency"
			;;
		/opt/homebrew/* | /usr/local/* | /Users/* | /private/tmp/* | /tmp/*)
			die "library depends on a build or package-manager path: $dependency"
			;;
		esac
	done <<<"$dependencies"
	printf 'install_name=%s\n' "$install_name"
	printf 'dependencies=%s\n' "$(printf '%s\n' "$dependencies" | paste -sd, -)"

	macos_minos=$(otool -l "$library" | awk '$1 == "minos" {print $2; exit}')
	macos_sdk=$(otool -l "$library" | awk '$1 == "sdk" {print $2; exit}')
	if [[ -z $macos_minos ]]; then
		macos_minos=$(otool -l "$library" |
			awk '/LC_VERSION_MIN_MACOSX/ {found=1} found && $1 == "version" {print $2; exit}')
	fi
	[[ -n $macos_minos ]] || die "library has no macOS deployment target"
	macos_runtime=$(sw_vers -productVersion)
	[[ $(vercmp "$macos_minos" "$macos_runtime") != 1 ]] ||
		die "library requires macOS $macos_minos but the runner provides $macos_runtime"
	printf 'macos_minos=%s\n' "$macos_minos"
	printf 'macos_sdk=%s\n' "${macos_sdk:-unknown}"
	printf 'macos_runtime=%s\n' "$macos_runtime"

	command -v nm >/dev/null 2>&1 || die "nm is required for darwin export verification"
	nm_output=$(nm -gU -j "$library") || die "nm could not inspect the Mach-O exports"
	[[ -n $nm_output ]] || die "library has no defined external symbols"
	# Mach-O's external C symbol names carry one ABI underscore prefix.
	defined=$(printf '%s\n' "$nm_output" | sed 's/^_//' | LC_ALL=C sort -u)
	lance_exports=$(printf '%s\n' "$defined" | grep -E '^lance_' || true)
	lance_count=$(printf '%s\n' "$lance_exports" | count_names)
	[[ $lance_count -gt 0 ]] || die "library symbol table has no lance_ exports"
	printf 'exported_defined_symbols=%s\n' "$(printf '%s\n' "$defined" | count_names)"
	printf 'exported_lance_symbols=%s\n' "$lance_count"
	missing_bound_symbols "$lance_exports"
	printf 'export_check=verified\n'
fi

printf 'check_native_library=ok\n'
