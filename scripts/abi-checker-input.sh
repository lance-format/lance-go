#!/usr/bin/env bash
# Print the header input that check-abi-symbols should use for one acquisition
# record. New records report abi_header: a directory for the split header set or
# a single file for a legacy archive. Historical records only have header, whose
# single-file value keeps the published legacy check.
set -euo pipefail

die() {
	printf 'abi-checker-input: %s\n' "$*" >&2
	exit 1
}

usage() {
	cat <<'EOF'
Usage: scripts/abi-checker-input.sh ACQUISITION_RECORD

Prints the file or directory to pass as -header (or LANCE_C_HEADER) to
internal/tools/check-abi-symbols. Repeated abi_header entries use the first one.
EOF
}

case "${1:-}" in
-h | --help)
	usage
	exit 0
	;;
esac
[[ $# -eq 1 ]] || {
	usage >&2
	exit 2
}
record=$1
[[ -f $record ]] || die "acquisition record not found: $record"

abi_header=$(sed -n 's/^abi_header=//p' "$record" | head -n 1)
if [[ -n $abi_header ]]; then
	[[ -d $abi_header || -f $abi_header ]] ||
		die "abi_header is neither a file nor a directory: $abi_header"
	printf '%s\n' "$abi_header"
	exit 0
fi

header=$(sed -n 's/^header=//p' "$record" | head -n 1)
[[ -n $header ]] || die "acquisition record has neither abi_header nor header: $record"
[[ -f $header ]] || die "legacy header is not a regular file: $header"
printf '%s\n' "$header"
