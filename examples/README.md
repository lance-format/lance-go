# Examples

These examples track `main`; the published release tags contain the same
examples.

Run the local examples from the repository root with the normal zero-config
bundle on a supported target:

```sh
CGO_ENABLED=0 go run ./examples/vector
CGO_ENABLED=0 go run ./examples/fts
CGO_ENABLED=0 go run ./examples/mutations
CGO_ENABLED=0 go run ./examples/namespace
```

Each program creates a temporary dataset and removes it on exit. It prints a
short deterministic result so you can compare behavior with the testable
examples in the root package.

`examples/cloud` uses static object-store options and requires an explicit URI.
It never stores credentials or prints them. A local path is enough for a smoke
test:

```sh
tmp="$(mktemp -d)"
CGO_ENABLED=0 go run ./examples/cloud -uri "$tmp/data.lance"
CGO_ENABLED=0 go run ./examples/cloud -mode open -uri "$tmp/data.lance"
rm -rf "$tmp"
```

For S3, GCS or Azure, pass the same URI and canonical options described in the
[API guide](../docs/api-guide.md#static-object-store-options):

```sh
CGO_ENABLED=0 go run ./examples/cloud \
  -uri 's3://bucket/prefix/data.lance' \
  -storage-option "aws_access_key_id=$AWS_ACCESS_KEY_ID" \
  -storage-option "aws_secret_access_key=$AWS_SECRET_ACCESS_KEY" \
  -storage-option "aws_region=$AWS_REGION"
```

Use a dedicated test prefix. The cloud example creates a new dataset and does
not delete data. Values passed with `-storage-option` can be
visible to other local processes, so use short-lived credentials from your
secret manager and avoid shell history for long-lived secrets.

`examples/cloud-refresh` is a separate opt-in acceptance runner for real AWS S3
credential rotation. See its [README](cloud-refresh/README.md) for the required
AWS setup and report format.
