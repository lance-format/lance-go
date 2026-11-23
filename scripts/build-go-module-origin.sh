#!/usr/bin/env bash
# Build the static Go module origin tree for the Lance-C artifact modules.
#
# The output is a staging tree, not a published release. Do not commit payloads.
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
candidates=${CANDIDATES_DIR:?CANDIDATES_DIR must point at the normalized candidates directory}
out=${OUT_DIR:-$HOME/Code/experiments/lance-go-module-origin/v0.2.0-preview.1}
version=${VERSION:-v0.2.0-preview.1}
source_commit=${SOURCE_COMMIT:-f0f1e987f298e4b41c4cf60e6a455c2249e0a5eb}
source_release=${SOURCE_RELEASE:-v0.2.1-beta.3}
release_time=${RELEASE_TIME:-2026-11-23T06:10:00Z}
module_prefix=${MODULE_PREFIX:-go.lance.org/liblance}
base_url=${BASE_URL:-https://go.lance.org}
license=${LICENSE_FILE:-$repo_root/LICENSE}

(cd "$repo_root/scripts/go-module-origin/generator" && \
  go run . \
    -candidates "$candidates" \
    -out "$out" \
    -version "$version" \
    -time "$release_time" \
    -module-prefix "$module_prefix" \
    -base-url "$base_url" \
    -license "$license" \
    -source-commit "$source_commit" \
    -source-release "$source_release")

echo "origin staging: $out"
echo "manifest:       $out/manifest.json"
