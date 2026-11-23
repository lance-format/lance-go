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

The first release inputs are pinned in
`v0.2.0-preview.1.identity.json`: version/time, beta.3 source commit, Unix run
`45123576018`, Windows run `45123578876`, candidate hashes, module h1 values,
and the staged manifest SHA-256. Workflow publication uses these fixed values
until a reviewed identity file replaces them.

## Local generation

Normalize artifact downloads into `candidates/<target>/module`, then:

```sh
CANDIDATES_DIR=/path/to/candidates \
OUT_DIR=$HOME/Code/experiments/lance-go-module-origin/v0.2.0-preview.1 \
scripts/build-go-module-origin.sh
```

The generator verifies each compressed and uncompressed payload against its
`artifact.go` constants and writes `.info`, `.mod`, `.zip`, `@v/list`,
discovery HTML, `manifest.json`, and `manifest.tsv`. The output is staging data;
do not commit payloads or ZIPs.

Verify locally:

```sh
ORIGIN_DIR=$OUT_DIR scripts/test-go-module-origin.sh
```

This checks the Go module protocol, local HTTP discovery, exact beta.3 library
hashes, and the SDK roundtrip with `CGO_ENABLED=0` and no `LANCE_LIB_PATH`.

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

All other release inputs are fixed in the workflow and identity file. Do not
dispatch the publisher, merge the PR, adopt modules, or create the root tag
until the leader has reviewed and authorized it. The real R2 acceptance probe
is performed after the handoff.

`.github/workflows/go-module-origin-checks.yml` runs generator unit tests,
shell/Python syntax checks, and a local mock publisher test on PRs. It does not
access R2.
