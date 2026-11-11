# Cloud credential refresh acceptance

This example is an opt-in acceptance runner for real AWS S3 credential rotation
through `lance-go`. It is not part of the SDK, has no AWS SDK dependency, and
does not create buckets, change IAM, or delete a caller-supplied prefix.

The runner has two explicit credential paths:

- `direct-accessor` (default): the runner installs a public
  `StorageOptionsAccessor` with an operator-controlled provider. This is the
  existing direct-accessor acceptance path.
- `namespace`: native opens the table through a `lance.Namespace` and installs
  its own namespace storage-options provider. The runner does not inject a
  direct provider on this path.

Both modes use real AWS S3. Neither mode is a commercial or cloud-native
namespace service, and neither proves namespace-managed table-version
publication.

## What it checks

The common contract is a tiny dataset, a seed credential A, and a later
credential B. The runner creates the dataset, retains a dataset/store across
the refresh window, and performs a `Delete` after the native early-refresh time.
The write must use B, not A. The runner then verifies:

- the old snapshot still has version 1 and rows `[10, 20, 30]`;
- the mutation result has version 2 and rows `[10, 30]`, with one row deleted;
- a fresh reopen sees version 2, rows `[10, 30]`, and an independent
  `CheckoutVersion(1)` sees version 1 and `[10, 20, 30]`;
- every native fetch after the refresh uses one fingerprint, and that
  fingerprint differs from the seed A fingerprint.

A repeated callback that returns A is not rotation. The run fails if native
does not fetch a different fingerprint before the overall deadline.

A is fetched directly by the runner from the operator executable as a seed.
Only B is proven to have been fetched by native. The runner asks native to
refresh early using a real `refresh_offset_millis` derived from A's actual
`Expiration`; it does not prove that an already-expired credential is rejected.

### Direct-accessor mode

The runner builds a `lance.StorageOptionsAccessor` from A and the provider,
creates the dataset with `WithWriteStorageOptionsAccessor`, closes the public
accessor before the refresh, and performs `Delete` through the retained
dataset. Native's accessor refresh calls the runner's provider, which invokes
the credential executable for B. The final reopen uses a fresh accessor and
provider against the same URI.

### Namespace mode

Namespace mode is an operator-owned, self-hosted table descriptor served over a
real loopback REST adapter:

1. The runner fetches A with the existing credential executable.
2. It creates the unique run URI directly with A and `WithSkipAutoCleanup(true)`.
3. It starts a `namespace.NewCustom` descriptor and a native
   `StartRESTAdapter("127.0.0.1", 0)`, then creates a real
   `namespace.NewRest` client at the ephemeral loopback port.
4. `lance.OpenFromNamespace` sends the initial `DescribeTable` with no
   `vend_credentials`. The descriptor validates the exact configured table ID,
   returns A in `storage_options`, the exact S3 `location`, and
   `ManagedVersioning=false`. Native installs those initial options and its
   `LanceNamespaceStorageOptionsProvider`.
5. The runner closes the public REST namespace and the custom namespace owner
   after the open. The retained Dataset keeps the native REST client and
   provider; the REST adapter keeps the descriptor backend alive.
6. After the native refresh window, `Delete` runs through the retained Dataset.
   Object-store credential acquisition sends `DescribeTable` with
   `vend_credentials=true` for the exact table ID over the loopback HTTP
   transport. The descriptor then invokes the credential executable for stable
   B and returns B's real expiration and storage options.
7. The runner verifies the versions and rows, then reopens with a fresh REST
   client against the same adapter. The first describe is non-vend and returns
   B, because the descriptor remembers the latest vended service state. It does
   not run a client-side refresh loop, cache, or reopen workaround.

The descriptor implements only `DescribeTable`. The three managed-version
methods return a typed `Unsupported` error; this example does not fake commit
coordination. The custom client rejects nil or foreign table IDs before touching
credentials and never returns credentials for a foreign ID. Its only service
state is the latest returned options, so a later initial describe serves B.

The REST client uses `$` as its table-ID delimiter to match this runner's
`--namespace-table-id` syntax. The adapter binds `127.0.0.1` on an ephemeral
port. No traffic leaves the loopback interface for the namespace control plane;
dataset reads and writes still go to real AWS S3.

Namespace mode does not publish namespace-managed table versions. Normal file
manifests and native publication remain authoritative because the descriptor
returns `ManagedVersioning=false`. The scope is explicitly operator-owned and
self-hosted, not a commercial or cloud-native namespace service.

## AWS prerequisites

Use a dedicated, non-root prefix in an existing bucket. The runner appends a
cryptographically random suffix and creates only that new run URI. It does not
overwrite existing data and does not recursively delete the supplied prefix.

The runner needs only prefix-scoped object access plus a list restricted to the
same prefix:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": "s3:ListBucket",
      "Resource": "arn:aws:s3:::BUCKET",
      "Condition": {
        "StringLike": {
          "s3:prefix": ["PREFIX/*"]
        }
      }
    },
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:PutObject",
        "s3:DeleteObject"
      ],
      "Resource": "arn:aws:s3:::BUCKET/PREFIX/*"
    }
  ]
}
```

No `s3:CreateBucket`, `s3:ListAllMyBuckets`, or IAM permission is required.
The bucket and prefix must already exist. Use an account and bucket intended
for acceptance testing, not a production account acceptance claim. Do not put
real account IDs, bucket credentials, or credential-process output in Git.

## Credential executable contract

The executable is invoked directly with its argument vector. The runner does
not use a shell, does not evaluate strings, and does not log or persist the
command's stdout or stderr. The executable must print one AWS
`credential_process` version 1 JSON document on stdout:

```json
{
  "Version": 1,
  "AccessKeyId": "ASIA...",
  "SecretAccessKey": "...",
  "SessionToken": "...",
  "Expiration": "2030-01-02T03:19:05Z"
}
```

Requirements:

- `Version` must be `1`.
- `AccessKeyId`, `SecretAccessKey`, and `SessionToken` must be non-empty and
  must not have surrounding whitespace.
- `Expiration` must be RFC3339 and must be in the future at the time of the
  fetch. The runner requires at least 20 seconds of remaining lifetime.
- The first invocation, used for the seed, returns A. The later invocation made
  by native after the refresh window returns stable B. B must remain stable
  through the mutation and reopen. The helper must not use a shell or write
  credentials to the report.

The helper is controlled by the operator. A typical helper selects the current
short-lived credential from an external rotation state; this example does not
include or require a credential server, a cache, or a refresh goroutine. The
namespace descriptor deliberately does not call the helper for the initial
describe; it serves the seed A that the runner already fetched. It calls the
helper only for native `vend_credentials=true`.

## Run

From the repository root, with an existing bucket and dedicated prefix:

```sh
go run ./examples/cloud-refresh \
  --uri 's3://BUCKET/PREFIX' \
  --region 'us-east-1' \
  --credential-executable /absolute/path/to/credential-process \
  --credential-arg '--profile' \
  --credential-arg 'acceptance' \
  --deadline 8m \
  --hold 30s \
  --fetch-timeout 30s \
  --report /tmp/cloud-refresh-report.json
```

That is direct-accessor mode. Add the namespace path explicitly:

```sh
go run ./examples/cloud-refresh \
  --mode namespace \
  --namespace-table-id 'acceptance$cloud-refresh' \
  --uri 's3://BUCKET/PREFIX' \
  --region 'us-east-1' \
  --credential-executable /absolute/path/to/credential-process \
  --credential-arg '--profile' \
  --credential-arg 'acceptance' \
  --deadline 8m \
  --hold 30s \
  --fetch-timeout 30s \
  --report /tmp/cloud-refresh-namespace-report.json
```

`--namespace-table-id` is the operator-owned descriptor identity, not the S3
path. Separate its segments with `$`; each segment is limited to letters,
digits, dot, underscore, and hyphen. The S3 location is the generated run URI.
`--namespace-table-id` is rejected unless `--mode namespace` is set.

Required flags are `--uri`, `--region`, `--credential-executable`, and
`--deadline`. Repeat `--credential-arg` for each argument. `--mode` defaults to
`direct-accessor`; `--hold` defaults to 30 seconds; `--fetch-timeout` defaults
to 30 seconds; and `--report` defaults to stdout (`-`). Keep the report; it is
written even when the run fails.

The deadline bounds credential subprocesses and all runner waits. It must be at
least the hold window plus one fetch timeout plus 30 seconds of verification
reserve, and at most two hours. In namespace mode the callback runs inside a
synchronous native `DescribeTable` request; a native call already in progress
is not interruptible by this runner. The deadline is checked before native calls
and after they return. The REST adapter shutdown also waits for active
requests, so the runner closes it only after all datasets are closed.

If A's remaining lifetime is short, the runner reduces the hold to one third of
the remaining lifetime. The actual hold, native refresh offset, and refresh due
time are recorded in the report; they are not fabricated.

The helper must keep credential B stable through mutation and reopen. A helper
that mints a different credential on every invocation does not satisfy this
runner's two-credential contract. Native fixes the refresh offset from the
initial map; when B expires near A, repeated native accesses can invoke the
helper repeatedly. Keep the helper bounded and account for those calls in the
deadline. The runner compares sorted row IDs and requires exactly one deletion.

## Report

The report is JSON and contains no AccessKeyId, secret access key, or session
token. It records:

- mode, safe status, stage, and error category;
- start/end/deadline timestamps, counters, and the exact `run_uri`;
- the SHA-256 fingerprint of A's `AccessKeyId`, A's expiration, and whether A
  had a session token;
- direct-accessor native fetch records and reconnect evidence;
- namespace initial/vend describe records with `vend_credentials` flags, exact
  table ID, duration, fingerprint, expiration, and token presence only;
- the native refresh due time and the actual `refresh_offset_millis`;
- the Delete predicate, deleted row count, old/committed version and rows;
- which resources stayed open across the refresh and which were recreated;
- explicit limitations: no expired-A rejection claim, no commercial or
  cloud-native namespace vending, no namespace-managed version publication, and
  no production-account support.

All response and error text is sanitized with the known credential values before
it reaches the report. The namespace evidence records fingerprints, not keys or
storage-option maps.

A successful run exits zero. A failed run exits non-zero and still writes the
report. In both modes the exact `run_uri` is owned by the parent/operator for
cleanup; the runner does not remove the supplied prefix or any pre-existing
objects. Namespace mode creates its fixture with auto-cleanup disabled, so the
report's `run_uri` is the exact object set to clean up after the run.

## Resource lifecycle

Direct-accessor mode closes the public accessor before the wait while the
created dataset and its native store stay open. Namespace mode closes the public
REST namespace and the custom namespace owner after `OpenFromNamespace`; the
retained Dataset's native REST client/provider and the REST adapter's descriptor
backend keep the path alive. The mutation runs through that retained Dataset,
so its write is not satisfied only by a public snapshot, a manual fetch, or a
reopen. The old dataset and the mutation result are closed after verification.
The final reopen uses a fresh REST client (namespace mode) or a fresh accessor
and provider (direct-accessor mode). Cleanup closes datasets first, then REST
namespace clients, then the adapter, then the custom namespace owner; the report
sets each outcome from the actual close result.

## Tests and scope

Unit tests in this package cover credential-process parsing, schedule
calculation, target-prefix parsing, namespace table-ID parsing, mode selection,
descriptor initial/vend state, exact/foreign table-ID rejection, redaction, and
typed unsupported managed-version methods. They do not execute native code,
open a REST adapter, or call AWS. They are not a substitute for the acceptance
run.

This runner does not test expired-access-key rejection, commercial or
cloud-native namespace credential vending, namespace-managed table-version
publication, IAM policy behavior, multi-process coordination, or a production
AWS account. It is a bounded acceptance check for native S3 credential refresh
using an operator-controlled rotation, with an optional operator-owned
self-hosted REST namespace path.
