# Static Go module origin

This directory generates and publishes the per-target Lance-C artifact modules
from an existing root checkout. The SDK root stays
`github.com/lance-format/lance-go`; artifact modules are ordinary Go modules
served from a static origin, not Git tags.

## Public layout

```text
liblance/<target>                                      # go-import HTML
modules/go.lance.org/liblance/<target>/@v/list
modules/go.lance.org/liblance/<target>/@v/<version>.info
modules/go.lance.org/liblance/<target>/@v/<version>.mod
modules/go.lance.org/liblance/<target>/@v/<version>.zip
```

The discovery object contains `go-import` metadata with
`go.lance.org/liblance/<target> mod https://go.lance.org/modules`. The query
string is ignored by the static object; no Worker is required.

The repository records the `v0.2.0-preview.1.identity.json` publication:
version/time, SDK source commit, upstream `lance-c` release, workflow runs,
candidate hashes, module h1 values, and the staged manifest SHA-256.
`verify-candidate-identity.py` compares candidate files with a supplied identity
file; it does not discover a new one. A later native version needs a new reviewed
identity file from its hosted candidate runs and updated publish-workflow
inputs; the recorded file does not describe later versions.

## Local generation

Normalize the exact hosted extraction trees with `stage-candidates.sh` (Unix artifacts have `module/` at the artifact root; Windows artifacts have `<target>/module/`), then set the roles explicitly. `SOURCE_COMMIT` is the SDK commit the candidates were built from, not the upstream native commit, and `SOURCE_RELEASE` is the upstream `lance-c` release:

```sh
: "${ARTIFACT_VERSION:?new companion module version}"
: "${SDK_SOURCE_COMMIT:?SDK commit the candidates were built from}"
: "${NATIVE_SOURCE_RELEASE:?upstream lance-c release}"
: "${RELEASE_TIME:?origin .info time in RFC3339}"
: "${CANDIDATES:?normalized candidates directory}"

scripts/go-module-origin/stage-candidates.sh /path/to/downloads "$CANDIDATES"
python3 scripts/go-module-origin/verify-candidate-identity.py \
  "scripts/go-module-origin/${ARTIFACT_VERSION}.identity.json" "$CANDIDATES"
VERSION="$ARTIFACT_VERSION" \
SOURCE_COMMIT="$SDK_SOURCE_COMMIT" \
SOURCE_RELEASE="$NATIVE_SOURCE_RELEASE" \
RELEASE_TIME="$RELEASE_TIME" \
CANDIDATES_DIR="$CANDIDATES" \
OUT_DIR="$HOME/Code/experiments/lance-go-module-origin/$ARTIFACT_VERSION" \
scripts/build-go-module-origin.sh
```

`scripts/build-go-module-origin.sh` defaults to the recorded
`v0.2.0-preview.1` publication; every value above must be set for a new version.

The generator verifies each compressed and uncompressed payload against its
`artifact.go` constants and writes `.info`, `.mod`, `.zip`, `@v/list`,
discovery HTML, `manifest.json`, and `manifest.tsv`. The output is staging data;
do not commit payloads or ZIPs.

Verify locally:

```sh
ORIGIN_DIR=$OUT_DIR scripts/test-go-module-origin.sh
```

This checks the Go module protocol, local HTTP discovery, the recorded
`v0.2.0-preview.1` version, the five recorded library hashes, and the SDK
roundtrip with `CGO_ENABLED=0` and no `LANCE_LIB_PATH`. The version and hash
assertions are fixed to that publication; a later native version requires a
reviewed update to this test, the identity file and the workflow inputs before
the origin can be accepted.

## Publisher

```sh
scripts/publish-go-module-origin.sh \
  --manifest "$OUT_DIR/manifest.json" \
  --bucket "$R2_BUCKET" \
  --endpoint "$R2_S3_ENDPOINT"
```

The publisher validates every manifest key, path, size, and SHA-256 before any
network request. Immutable `.info`, `.mod`, `.zip`, and discovery objects use
create-only `If-None-Match: *`; a conflict aborts without overwrite. `@v/list`
is merged with existing versions and updated with `If-Match` using the exact
quoted ETag returned by R2, with bounded retries. There is no unquoted ETag or
overwrite fallback.

## CI publication

`.github/workflows/publish-go-module-origin.yml` is manual
`workflow_dispatch` only, on `refs/heads/main`. Its staging job downloads the
exact same-repo candidate artifacts from the pinned runs, verifies the pinned
identity file, regenerates the origin, checks the exact manifest SHA-256, and
uploads the staging tree. The protected `publish` job runs only with
`publish=true`, has `environment: go-module-origin`, downloads its own staging
artifact, and maps the environment's R2 secrets to AWS CLI environment
variables only on the publish step. It never reads credentials in the staging
job or in a PR.

First dispatch arguments:

```text
ref: main
publish: true
```

All other release inputs are fixed in the workflow and identity file. The
workflow inputs (`ORIGIN_VERSION`, `ORIGIN_TIME`, `ORIGIN_SOURCE_COMMIT`,
`ORIGIN_SOURCE_RELEASE`, `ORIGIN_MANIFEST_SHA256`, `UNIX_RUN_ID`,
`WINDOWS_RUN_ID`) and the identity file describe the recorded
`v0.2.0-preview.1` publication. The workflow checks out `main`, so its generator
is whatever main contains at dispatch; the recorded manifest came from the
publication-time generator (`9f3ba53`, go 1.23.0). A later native version
requires a new reviewed identity file, new pinned candidate runs, updated inputs
and a generator whose Go floor is the reviewed one; there is no generic publish
path that discovers them. Do not dispatch the publisher, merge the PR, adopt
modules, or create the root tag until the leader has reviewed and authorized it.
The real R2 acceptance probe is performed after the handoff.

`.github/workflows/go-module-origin-checks.yml` runs generator unit tests,
shell/Python syntax checks, and a local mock publisher test on PRs. It does not
access R2.
