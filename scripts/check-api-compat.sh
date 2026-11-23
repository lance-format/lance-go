#!/usr/bin/env bash
# Compare the exported API of the public packages with the latest stable
# release tag using golang.org/x/exp/cmd/apidiff.
#
# The report always shows every change against the latest stable tag. An
# incompatible change fails the check unless it is listed as reviewed in
# apidiff/<base-tag>.txt. That makes the documented patch-release promise
# enforceable (within a minor line, published Go APIs keep source
# compatibility), while an approved minor release can record its reviewed
# migrations once and let unrelated pull requests pass.
#
# The approval file is scoped to its base tag: when a new stable tag becomes
# the base, the previous file stops applying. It belongs to an approved minor
# release only; never add one for a patch line. Necessary breaking changes
# belong to a new minor release and are reviewed against its migration notes;
# the report from this script is that review material.
#
# Run from a full clone: the script needs the release tag and creates a
# temporary worktree. The comparison uses the lance_external_lib build tag so
# it stays independent of the bundled native payloads; no public package
# changes its exported API under that tag.
#
# Usage: ./scripts/check-api-compat.sh [base-tag]

set -euo pipefail

# Pinned to x/exp 2026-11-21. Newer pins must still build with the stable Go
# toolchain the CI workflow selects.
APIDIFF_VERSION=v0.0.0-20261121134650-e1094eeca1b5

PUBLIC_PACKAGES=(
  "lance:."
  "compaction:./compaction"
  "file:./file"
  "index:./index"
  "namespace:./namespace"
)

root=$(git rev-parse --show-toplevel)
cd "$root"

base=${1:-}
if [[ -z $base ]]; then
  base=$(git tag --list 'v[0-9]*' | grep -v -- '-' | sort -V | tail -n 1)
fi
if [[ -z $base ]] || ! git rev-parse --verify --quiet "refs/tags/$base" >/dev/null; then
  echo "check-api-compat: no stable release tag found; run 'git fetch --tags' first" >&2
  exit 1
fi

allowlist="$root/apidiff/$base.txt"

package_names=()
for entry in "${PUBLIC_PACKAGES[@]}"; do
  package_names+=("${entry%%:*}")
done

# approved["<package>|<change>"] marks one reviewed incompatible change.
declare -A approved=()
if [[ -f $allowlist ]]; then
  while IFS= read -r line || [[ -n $line ]]; do
    line=${line%$'\r'}
    if [[ $line =~ ^[[:space:]]*(#|$) ]]; then
      continue
    fi
    if [[ $line != *": "* ]]; then
      echo "check-api-compat: $allowlist: expected '<package>: <apidiff change>', got: $line" >&2
      exit 1
    fi
    entry_pkg=${line%%:*}
    entry_msg=${line#*: }
    if [[ " ${package_names[*]} " != *" $entry_pkg "* ]]; then
      echo "check-api-compat: $allowlist: unknown package '$entry_pkg'" >&2
      exit 1
    fi
    approved["$entry_pkg|$entry_msg"]=1
  done <"$allowlist"
fi

tmp=$(mktemp -d)
cleanup() {
  git worktree remove --force "$tmp/base" >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT

echo "check-api-compat: comparing HEAD against $base"
if [[ -f $allowlist ]]; then
  echo "check-api-compat: applying reviewed migrations from apidiff/$base.txt"
else
  echo "check-api-compat: no reviewed migrations for $base; any incompatible change fails"
fi
git worktree add --detach "$tmp/base" "$base" >/dev/null

if [[ -n ${APIDIFF_BIN:-} ]]; then
  # Escape hatch for local diagnosis and failure-path verification. CI never
  # sets it, so the pinned install below is the normal path.
  apidiff=$APIDIFF_BIN
  if [[ ! -x $apidiff ]]; then
    echo "check-api-compat: APIDIFF_BIN=$apidiff is not executable" >&2
    exit 1
  fi
  echo "check-api-compat: using APIDIFF_BIN=$apidiff instead of the pinned tool"
else
  if ! GOBIN="$tmp/bin" go install "golang.org/x/exp/cmd/apidiff@$APIDIFF_VERSION"; then
    echo "check-api-compat: could not install pinned apidiff@$APIDIFF_VERSION" >&2
    exit 1
  fi
  apidiff="$tmp/bin/apidiff"
fi

# run_apidiff writes stdout to the named file and fails the gate on a nonzero
# exit. A failed run may have written empty or partial output; it is discarded
# and never parsed as a report.
run_apidiff() {
  local output=$1
  shift
  local status=0
  "$apidiff" "$@" >"$output" 2>"$output.stderr" || status=$?
  if ((status != 0)); then
    echo "check-api-compat: apidiff failed with exit status $status: apidiff $*" >&2
    if [[ -s $output.stderr ]]; then
      sed 's/^/  /' "$output.stderr" >&2
    fi
    echo "check-api-compat: the failed run's stdout is discarded; partial output is never parsed" >&2
    exit 1
  fi
}

unapproved_total=()
stale_total=()
approved_total=0
any_changes=0
for entry in "${PUBLIC_PACKAGES[@]}"; do
  name=${entry%%:*}
  pkg=${entry#*:}

  (cd "$tmp/base" && GOFLAGS="-tags=lance_external_lib" run_apidiff "$tmp/$name.base.export" -w "$tmp/$name.base" "$pkg")
  GOFLAGS="-tags=lance_external_lib" run_apidiff "$tmp/$name.head.export" -w "$tmp/$name.head" "$pkg"

  run_apidiff "$tmp/$name.report" "$tmp/$name.base" "$tmp/$name.head"
  run_apidiff "$tmp/$name.incompatible" -incompatible "$tmp/$name.base" "$tmp/$name.head"

  report=$(cat "$tmp/$name.report")
  if [[ -n $report ]]; then
    any_changes=1
  fi
  actual=()
  while IFS= read -r line; do
    [[ -z $line ]] && continue
    actual+=("${line#- }")
  done <"$tmp/$name.incompatible"

  echo "=== $name ($base -> HEAD) ==="
  if [[ -n $report ]]; then
    printf '%s\n' "$report"
  else
    echo "no exported API changes"
  fi

  if ((${#actual[@]} > 0)); then
    echo "Incompatible changes:"
    for msg in "${actual[@]}"; do
      if [[ -n ${approved["$name|$msg"]+x} ]]; then
        echo "  approved:   $msg"
        approved_total=$((approved_total + 1))
      else
        echo "  UNAPPROVED: $msg"
        unapproved_total+=("$name: $msg")
      fi
    done
  fi

  for key in "${!approved[@]}"; do
    if [[ ${key%%|*} != "$name" ]]; then
      continue
    fi
    change=${key#*|}
    found=0
    for msg in "${actual[@]}"; do
      if [[ $msg == "$change" ]]; then
        found=1
        break
      fi
    done
    if ((!found)); then
      stale_total+=("$name: $change")
    fi
  done
done

if ((${#unapproved_total[@]} > 0)); then
  echo "check-api-compat: unapproved incompatible changes:" >&2
  printf '  %s\n' "${unapproved_total[@]}" >&2
  echo "Review each change against the target release's migration notes. An approved" >&2
  echo "minor migration is recorded in apidiff/$base.txt; a patch line must not add one." >&2
  exit 1
fi
if ((${#stale_total[@]} > 0)); then
  echo "check-api-compat: reviewed entries in apidiff/$base.txt no longer match the API:" >&2
  printf '  %s\n' "${stale_total[@]}" >&2
  echo "Remove or update the stale entries so every approval stays reviewable." >&2
  exit 1
fi
if ((approved_total > 0)); then
  echo "check-api-compat: $approved_total incompatible change(s) are recorded as reviewed against $base"
elif ((any_changes == 0)); then
  echo "check-api-compat: no exported API changes against $base"
else
  echo "check-api-compat: no incompatible changes against $base (compatible changes reported above)"
fi
