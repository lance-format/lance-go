# Phase 5 cross-language compatibility suite

This directory is an opt-in compatibility check for the Lance SDKs used by
lance-go. The Go CLI is compiled by `go test ./...`, but the four-language
runner is opt-in and is not executed by that command.

Each language has a small `produce` / `read` / `append` CLI:

- `go`: writes and reads through `lance-go` and the native bundle already
  pinned by the repository.
- `python`: writes and reads through pylance 14.0.2 and pyarrow 22.0.0.
- `java`: writes and reads through `org.lance:lance-core:14.0.2` and Arrow
  18.3.0.
- `rust`: writes and reads through the lance core checkout at
  `40f15ed1b71b66950961b175829fdf17a763f108` and Arrow 58.3.0.

The harness checks literal values, not just same-language round trips. It
validates the complete Arrow schema, including field and schema metadata,
before producing a canonical report. The fixture includes int64 values beyond
2^53, null versus empty UTF-8 and binary values, null versus empty lists,
nullable nested structs, fixed-size-list float32 vectors, decimal128(38,9),
and timestamp units/timezones. Reports use hex for strings, binary, and field
metadata; decimal strings for decimals; raw integer values for timestamps; and
float32 bit patterns for floats. No float JSON representation is used.

## Storage format

The common storage format selected for this suite is Lance 2.1. Acceptance
requires the complete matrix; the tested revision is recorded below. Every SDK
receives an explicit format; no SDK is allowed to fall back to its own default.
The runner records the requested format in `metadata.txt`. The same bounded
fixture is wired for an opt-in `--storage-version 2.2` run, but 2.2 is outside
the accepted scope of this change until that run has actually been executed.

## Accepted execution

On 2026-11-13, SDK `807cb84305302dc3ff3b40809d817a876efd2909` passed all 56
storage 2.1 cells on Linux amd64 (Ubuntu 24.04.5, Go 1.23.12, CGO disabled,
external-library mode). The Go fixture loaded the published linux-amd64
`ac5955a` library from native source
`ac2634c553b450c7787577ffe964fc2b283b8334`: 322099824 bytes, SHA-256
`e5571d6da16e81a0434f2509ade5262785b63df0b37ff7155537642841dd9378`.

The run used the four SDK pins above. Rust and Java fixture binaries were
reused after their source identities were checked; Python 3.12.3 used pylance
14.0.2 and pyarrow 22.0.0 in an isolated environment matching the accepted
package freeze. The Go fixture was rebuilt from the exact tested SDK source.
All four producers, 16 cross-reader cells and six bidirectional append paths
passed. Independent recount of all 56 cell logs found no missing cells,
nonzero exits, empty reports or literal-golden mismatches.

Retained evidence on NieR is
`/home/xuanwo/Code/lance-go-acceptance/artifacts/compat-ac263-807cb84/`
(`RESULT.md`, per-cell logs, tool/library provenance and `MANIFEST.sha256`).
This result does not cover storage 2.2, other operating systems, cloud storage
or a released root tag. It supplements the earlier native88 result rather
than changing that result's provenance.

## Prerequisites

Run the suite on a host with all four toolchains. Do not run it on the Adam
static-inspection host.

- Go 1.23 or newer, with the repository's existing Arrow v18.4.1 dependency.
- Python 3.10 or newer.
- Java 11 or newer and Maven. Pass `--add-opens=java.base/java.nio=ALL-UNNAMED`
  to the JVM as shown below; Arrow requires access to direct buffers on JDK 17.
- Rust 1.96.1 (the acceptance toolchain), with the lance core git revision above
  available to Cargo. The committed lockfile requires at least Rust 1.95;
  the core crate's lower MSRV alone does not satisfy this dependency graph.
- The native library paired with the tested SDK revision. The current pin is
  `ac2634c553b450c7787577ffe964fc2b283b8334`; verify the selected library hash
  against the corresponding artifact metadata. Historical native07/native88
  compatibility results do not establish execution against a different library.

### Python

Create an ordinary project-local virtual environment; do not install into the
global interpreter:

```bash
python3 -m venv compat/.venv
compat/.venv/bin/pip install -r compat/requirements.txt
```

### Build the CLIs

From the repository root:

```bash
go build -o /tmp/lance-compat-go ./compat/go
(cd compat/java && mvn -q -DskipTests package)
cargo build --locked --release --manifest-path compat/rust/Cargo.toml
```

The Rust build must use the normal cargo target/cache selected by the local
toolchain. Do not set a custom `CARGO_TARGET_DIR` for this suite.

## Run

The runner requires POSIX process groups and runs serially. It creates a new
run directory, writes one log per cell,
and compares each cell's stdout with the committed reports in `expected/`.
Use a directory outside the repository, for example:

```bash
python3 compat/run.py \
  --run-dir /tmp/lance-compat-run-2.1 \
  --storage-version 2.1 \
  --go-cmd "/tmp/lance-compat-go" \
  --python-cmd "compat/.venv/bin/python compat/python/compat.py" \
  --java-cmd "java --add-opens=java.base/java.nio=ALL-UNNAMED -jar compat/java/target/lance-compat-java.jar" \
  --rust-cmd "cargo run --locked --quiet --release --manifest-path compat/rust/Cargo.toml --"
```

The default commands use `go run`, the system `python3`, the shaded Java jar,
and `cargo run`; the explicit commands above make the selected tools
unambiguous. The runner refuses to use an existing run directory, so reruns
cannot overwrite prior evidence.

The 2.2 run is the same command with a different, new run directory and:

```bash
--storage-version 2.2
```

### What is executed

- Four producer cells write four independent base datasets, one per SDK.
- A 4x4 read matrix: every producer dataset is read by every SDK, including
  the producer itself. All 16 reads compare against `expected/base.report`.
- Six separate append paths cover both directions for
  Go<->Python, Go<->Java, and Go<->Rust:
  - Go writes the base dataset, the foreign SDK appends, and both SDKs read
    version 1 and version 2.
  - The foreign SDK writes the base dataset, Go appends, and both SDKs read
    version 1 and version 2.
  Each path is distinct, so an append cannot contaminate another scenario.
  The append command itself reopens version 1, appends, and checks the new
  version and row count. The post-append reads compare version 1 against
  `expected/base.report` and version 2 against `expected/latest.report`.

The total is 56 cells: 4 base writes, 16 base reads, and 36 append-scenario
cells (6 paths x 6 cells). A missing language or dependency is reported as
`not-run`, not as a pass. The runner returns nonzero if any cell fails or is
not run. `summary.txt` distinguishes `executed`, `passed`, `failed`, and
`not_run`.

## Evidence layout

Everything the runner creates is outside tracked source:

```text
<run-dir>/
  metadata.txt       toolchain versions, commands, git head, storage format
  summary.txt        per-cell status and summary counts
  logs/              stdout, stderr, exit status, and expected/actual diffs
  datasets/          on-disk Lance datasets for each independent scenario
```

A failed cell keeps its log and dataset. The runner never rewrites a failed
run in place; use a new run directory for a retry.

## Scope and limitations

- This is a local fixture. It does not test object stores,
  credentials, or distributed operations.
- It checks only capabilities present in all four selected SDK versions.
  A missing SDK operation must be reported as a concrete gap; it must not be
  simulated by taking another producer's Arrow IPC and relabeling it as a
  Lance dataset. Each SDK writes its report through its own Lance read path.
- The Java reader checks out the requested version with
  `Dataset.checkoutVersion`; the Go, Python, and Rust readers select the same
  version through their own dataset APIs.
- The fixture is deliberately tiny and literal. The expected reports are
  committed, so a change to a value or schema must update the golden files and
  is visible in review.

Runner-only regression checks (no SDK compilation or native loading):

```sh
python3 -B -m unittest discover -s compat -p test_run.py -v
```

A timed-out command terminates its process group, including SDK subprocesses
started by wrappers such as `go run` and `cargo run`. Its partial stdout and
stderr remain in the cell log with exit status 124. Preflight and version
probes use the same timeout behavior. The Go fixture/golden report regression
runs with the existing `go test ./...` entry point; it does not certify any
cross-language dataset compatibility.
