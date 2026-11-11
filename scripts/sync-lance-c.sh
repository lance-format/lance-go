#!/usr/bin/env bash
# Package an already-built liblance_c into a data-only Go artifact module and
# a platform selector. This command does not build or download lance-c.
#
# Requires: go 1.23+, the dependencies added to the root go.mod, and a
# library/header built from the exact commit pinned in internal/ffi/upstream.go.
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
upstream_go="$repo_root/internal/ffi/upstream.go"

usage() {
	cat <<'EOF'
Usage: scripts/sync-lance-c.sh --library PATH --header PATH \
         --target GOOS/GOARCH --out DIR [--module-path PATH] \
         [--selector-package NAME]

Packages an already-built lance-c shared library as a data-only Go module and
generates a platform-constrained selector for internal/bundled.

Required:
  --library PATH          built liblance_c.so, liblance_c.dylib, or lance_c.dll
  --header PATH           matching include/lance/lance.h
  --target GOOS/GOARCH    linux, darwin, or windows on amd64 or arm64
  --out DIR               new output directory; must not already exist

Optional:
  --module-path PATH      generated module path
                          (default: github.com/lance-format/lance-go/artifacts/<goos>-<goarch>)
  --selector-package NAME package name for the generated selector (default: bundled)

The output directory contains:
  module/                 go.mod, artifact.go, and the compressed payload
  selector/               lance_bundle_<goos>_<goarch>.go

The commit and header hash recorded in the generated module are read from
internal/ffi/upstream.go. The header is verified against that pin. A library
built from a different commit is not detected because lance-c exposes no
runtime version symbol; provide a library and header from the pinned commit.
EOF
}

die() {
	printf 'sync-lance-c: %s\n' "$*" >&2
	exit 1
}

read_pin() {
	local name=$1 value count
	count=$(grep -c "^[[:space:]]*$name[[:space:]]*=" "$upstream_go" || true)
	[[ $count -eq 1 ]] || die "expected exactly one $name in $upstream_go, found $count"
	value=$(sed -n "s/^[[:space:]]*$name[[:space:]]*=[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p" "$upstream_go")
	[[ -n $value ]] || die "could not read $name from $upstream_go"
	printf '%s' "$value"
}

library=""
header=""
target=""
out_dir=""
module_path=""
selector_package="bundled"

while [[ $# -gt 0 ]]; do
	case "$1" in
	--library)
		[[ $# -ge 2 ]] || {
			usage >&2
			exit 2
		}
		library=$2
		shift 2
		;;
	--header)
		[[ $# -ge 2 ]] || {
			usage >&2
			exit 2
		}
		header=$2
		shift 2
		;;
	--target)
		[[ $# -ge 2 ]] || {
			usage >&2
			exit 2
		}
		target=$2
		shift 2
		;;
	--out)
		[[ $# -ge 2 ]] || {
			usage >&2
			exit 2
		}
		out_dir=$2
		shift 2
		;;
	--module-path)
		[[ $# -ge 2 ]] || {
			usage >&2
			exit 2
		}
		module_path=$2
		shift 2
		;;
	--selector-package)
		[[ $# -ge 2 ]] || {
			usage >&2
			exit 2
		}
		selector_package=$2
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		printf 'sync-lance-c: unknown argument %q\n' "$1" >&2
		usage >&2
		exit 2
		;;
	esac
done

[[ -n $library ]] || die "--library is required"
[[ -n $header ]] || die "--header is required"
[[ -n $target ]] || die "--target is required"
[[ -n $out_dir ]] || die "--out is required"
[[ -n $selector_package ]] || die "--selector-package must not be empty"
command -v go >/dev/null 2>&1 || die "required command not found: go"

# Absolute operands keep names beginning with '-' from becoming options to
# commands invoked below, and make the output location independent of the
# repository-root cd needed for go run.
case "$library" in
/*) ;;
*) library="$PWD/$library" ;;
esac
case "$header" in
/*) ;;
*) header="$PWD/$header" ;;
esac
case "$out_dir" in
/*) ;;
*) out_dir="$PWD/$out_dir" ;;
esac

[[ ! -e "$out_dir" && ! -L "$out_dir" ]] || die "output already exists: $out_dir; choose a new --out directory"

upstream_repo=$(read_pin upstreamRepo)
upstream_commit=$(read_pin upstreamCommit)
header_sha256=$(read_pin upstreamHeaderSHA256)
[[ $upstream_commit =~ ^[0-9a-f]{40}$ ]] || die "pinned upstreamCommit is not a full SHA-1: $upstream_commit"
[[ $header_sha256 =~ ^[0-9a-f]{64}$ ]] || die "pinned upstreamHeaderSHA256 is not a SHA-256: $header_sha256"

if [[ -z $module_path ]]; then
	module_path="github.com/lance-format/lance-go/artifacts/${target//\//-}"
fi

cd "$repo_root"
go run ./scripts/sync-lance-c-gen \
	-library "$library" \
	-header "$header" \
	-target "$target" \
	-out "$out_dir" \
	-module-path "$module_path" \
	-selector-package "$selector_package" \
	-upstream-repo "$upstream_repo" \
	-upstream-commit "$upstream_commit" \
	-header-sha256 "$header_sha256"

cat <<EOF

sync-lance-c: wrote $out_dir
  module:   $out_dir/module
  selector: $out_dir/selector/lance_bundle_${target//\//_}.go

Next steps:
  1. Publish module/ at $module_path and add it to the root module requirements.
  2. Copy the generated selector into internal/bundled and rebuild the root module.
  3. Repeat for each target, then verify a cold-cache consumer build on NieR.
EOF
