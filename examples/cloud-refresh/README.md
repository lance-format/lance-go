# Cloud credential refresh acceptance

This example is an opt-in acceptance runner for real AWS S3 credential rotation
through the public `lance-go` storage-options accessor. It is not part of the
SDK, it has no AWS SDK dependency, and it does not create buckets, change IAM,
or delete a caller-supplied prefix.

## What it checks

The runner creates a tiny dataset with a seed credential (A), keeps the native
dataset store alive after closing the public accessor, waits for the native
early-refresh window, and then performs a `Delete` through the retained dataset.
The S3 request re-enters the native accessor and must fetch a different actual
`AccessKeyId` (B). The runner then waits for the committed version and verifies:

- the old dataset snapshot still has version 1 and rows `[10, 20, 30]`;
- the mutation result has version 2 and rows `[10, 30]`, with one row deleted;
- a fresh accessor/provider reopens the same URI and sees version 2, rows
  `[10, 30]`, with only B fetched by native;
- every native fetch after the refresh uses one fingerprint, and that
  fingerprint differs from the seed A fingerprint.

A repeated callback that returns A is not rotation. The run fails if native
does not fetch a different fingerprint before the overall deadline.

A is fetched directly by the runner from the operator executable as a seed.
Only B is proven to have been fetched by native. The runner asks native to
refresh early using a real `refresh_offset_millis` derived from A's actual
`Expiration`; it does not prove that an already-expired credential is rejected.

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
for acceptance testing, not a production account acceptance claim.

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
- The first invocation, used for the seed, must return A. Invocations made by
  native after the refresh window must return B. The helper must not use a
  shell or write credentials to the report.

The helper is controlled by the operator. A typical helper selects the current
short-lived credential from an external rotation state; this example does not
include or require a credential server, a cache, or a refresh goroutine.

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

Required flags are `--uri`, `--region`, `--credential-executable`, and
`--deadline`. Repeat `--credential-arg` for each argument. `--hold` defaults to
30 seconds, `--fetch-timeout` defaults to 30 seconds, and `--report` defaults to
stdout (`-`). Keep the report; it is written even when the run fails.

The deadline bounds credential subprocesses and all runner waits. It must be at
least the hold window plus one fetch timeout plus 30 seconds of verification
reserve, and at most two hours. A native S3 call that is already in progress is
synchronous and is not interruptible by this runner; the deadline is checked
before native calls and after they return. If A's remaining lifetime is short, the runner reduces the hold
to one third of the remaining lifetime. The actual hold, native refresh offset,
and refresh due time are recorded in the report; they are not fabricated.

## Report

The report is JSON and contains no AccessKeyId, secret access key, or session
token. It records:

- safe status, stage, and error category;
- start/end/deadline timestamps, counters, and the exact `run_uri`;
- the SHA-256 fingerprint of A's `AccessKeyId`, A's expiration, and whether A
  had a session token;
- native fetch records (fingerprint, time, expiration, token presence, and
  duration) for the mutation and reopen;
- the native refresh due time and the actual `refresh_offset_millis`;
- the Delete predicate, deleted row count, old/committed version and rows;
- which resources stayed open across the refresh and which were recreated;
- explicit limitations: no expired-A rejection claim, no cloud-native
  namespace vending, and no production-account support.

A successful run exits zero. A failed run exits non-zero and still writes the
report. The report's exact `run_uri` is owned by the parent/operator for cleanup;
the runner does not remove the supplied prefix or any pre-existing objects.

## Resource lifecycle

The public accessor is closed before the wait, while the created dataset and
its native store stay open. The mutation is performed through that retained
dataset, so its write is not satisfied only by the public snapshot, a manual
fetch, or a reopen. The old dataset and the mutation result are both closed
after verification. The reopen uses a new public accessor whose initial options
contain only the region, plus a new provider with the same stable provider ID.
All datasets and accessors are closed on success and failure.

## Out of scope

This runner does not test expired-access-key rejection, cloud-native namespace
credential vending, IAM policy behavior, multi-process coordination, or a
production AWS account. It is a bounded acceptance check for native S3
credential refresh using an operator-controlled rotation.
