# RSS and lifecycle soak benchmark

`benchmarks/soak` is an opt-in, bounded, Linux-only benchmark for long-running
use of the public lance-go API. It repeatedly exercises dataset, scanner, raw
file, Blob and writer lifetimes while sampling total process residency. It is
not run as a long benchmark in CI. The package is compiled and its tests run
with `go test ./...`, including a short Linux native journal regression when
the library is available.

This command is supplementary evidence. Existing lifetime and ownership tests
remain the primary regression coverage for those contracts. A run of this
benchmark does not prove that native memory cannot leak.

## Requirements

- Linux. Process RSS comes from `/proc/self/smaps_rollup`; on other operating
  systems the command returns an unsupported error instead of reporting zero.
- Go 1.23 or newer.
- A loadable `liblance_c`: the default bundled `linux/amd64` artifact, or an
  external library selected with `LANCE_LIB_PATH`.
- No cgo. Build and run with `CGO_ENABLED=0`.

## Build on the target host

Run these commands on the host that will execute the benchmark, for example
NieR, from the lance-go checkout to review:

```sh
CGO_ENABLED=0 go build -o /tmp/lance-go-soak ./benchmarks/soak
```

Default build (bundled `linux/amd64` artifact):

```sh
/tmp/lance-go-soak -warmup=10s -duration=30s -sample-interval=1s \
  -concurrency=2 -rows=2048 -jsonl=/tmp/soak-smoke.jsonl
```

External library build:

```sh
export LANCE_LIB_PATH=/absolute/path/to/liblance_c.so
CGO_ENABLED=0 go build -tags lance_external_lib -o /tmp/lance-go-soak ./benchmarks/soak
/tmp/lance-go-soak -warmup=10s -duration=30s -sample-interval=1s \
  -concurrency=2 -rows=2048 -jsonl=/tmp/soak-smoke.jsonl
```

Both build forms honor `LANCE_LIB_PATH` when it is set. The default build also
contains the bundled `linux/amd64` artifact and falls back to it when the
variable is unset. The `lance_external_lib` build omits that embedded artifact,
so the variable must remain set to an existing library file when the binary
runs. `LANCE_LIB_PATH` must name the actual library file, not a directory. The
journal records the resolved `loader.Load().Path()` and its SHA-256, so a run
can be tied to the bytes that were loaded. The environment variable itself is
not copied into the journal.

## Run sizes

Short smoke, for command and journal wiring:

```sh
/tmp/lance-go-soak -warmup=10s -duration=30s -sample-interval=1s \
  -concurrency=2 -rows=2048 -jsonl=/tmp/soak-smoke.jsonl
```

Actual long run, at least 30 minutes:

```sh
/tmp/lance-go-soak -warmup=2m -duration=30m -sample-interval=10s \
  -concurrency=4 -jsonl=/tmp/soak-long.jsonl
```

A short smoke only demonstrates that the harness works. It is not long-run
evidence. The operator chooses the final duration; the command does not impose
a leak-free threshold.

Optional fixture retention (the directory must be empty and is kept):

```sh
/tmp/lance-go-soak -warmup=2m -duration=30m -sample-interval=10s \
  -dir=/tmp/lance-go-soak-fixtures -jsonl=/tmp/soak-long.jsonl
```

Omit `-dir` to use a temporary directory that is removed at the end. Keep the
JSONL file outside a retained fixture directory.

Optional caller budget, in bytes, on the late-window mean RSS minus the
early-window mean RSS:

```sh
/tmp/lance-go-soak -warmup=2m -duration=30m -sample-interval=10s \
  -max-growth-bytes=16777216 -jsonl=/tmp/soak-budget.jsonl
```

A budget failure marks this run as failed. It does not prove that a leak
exists, and a passing budget does not prove that no leak exists.

## Workloads and fixture

Defaults: 8192 rows, 256-byte payload, scan/raw batch size 1024, concurrency 4,
30-second warmup, 5-minute measurement and 5-second sampling. The measurement
duration must be at least four sampling intervals, and at least four measurement
samples are required.

The fixture is created once and is never appended to or versioned during
measurement. It consists of:

- `main.lance`: `id` (int64, non-null), `name` (utf8, non-null) and `payload`
  (binary, non-null) rows. Names and payload bytes are deterministic; a fixed
  FNV-1a sum over all rows is the scan oracle.
- `raw.bin`: the same rows written through the raw `.lance` file API.
- `blob.lance`: at most 16 rows with a Blob field; every fourth row is null and
  the inline threshold is 1 MiB. Each non-null payload is distinct.
- `write/`: per-worker write directories used by the write workload. A worker
  removes and recreates only its own directory, only between completed write
  operations.

Defaults therefore retain only the capped write batch and the small Blob value
set; fixture creation briefly holds the main payload batches in Go memory. The
command refuses configurations whose estimated fixture memory exceeds 1 GiB.
Blob rows and write rows are capped independently of `-rows`.

Selected workloads (`-workloads=all` by default):

| Name | Operations per epoch |
| --- | --- |
| `open-close` | Create a 1 MiB session, open with it, count rows, close dataset, close session. |
| `scan` | Open, full projected scan, verify every row and total hash, close. |
| `early-break` | Open, scan, verify one delivered batch, break, close scanner. |
| `cancel` | Open, scan, verify at least one delivered batch, cancel, close scanner. |
| `retain` | Open, scan, retain the first batch, close scanner, verify the retained batch, release it. |
| `write` | Create from the retained fixture batch, close, reopen, verify all rows and hash, close, remove its directory. |
| `rawfile` | Open `raw.bin`, read all batches, verify rows and hash, release reader, close. |
| `blob` | Open the Blob dataset, read the bounded row set, retain the first batch, release the reader, verify retained bytes, release batch, close. |

The cancellation workload requires an actual native batch delivery before it
asks for cancellation. The scanner may already hold buffered batches, so the
operation accepts either a `context.Canceled` result or a clean completion; the
two outcomes are counted separately. A zero-batch delivery is an error.

## Measurement contract

Each epoch starts exactly one operation on every worker and waits for all of
them to finish. Operations close their resources before reporting completion.
Every measurement epoch checks the registered Go callback handle count against the warmed
baseline. At sampling boundaries the harness also reads:

- RSS and the available `smaps_rollup` fields: PSS, anonymous, shared/private
  clean and dirty, PSS file-backed and swap.
- Go `runtime.MemStats` heap allocation, heap system bytes, total runtime system
  bytes and GC count.
- Goroutine count, `internal/handle.Len()`, file descriptor count and OS thread
  count where available.

Warmup samples are excluded from the measurement summary. The command rejects
a warmup and measurement schedule that would retain more than 100,000 samples,
so harness-local evidence storage stays bounded. The warmed handle count is the
baseline; every measurement epoch and every measurement sample must still match
it, otherwise the run fails loudly. Every selected workload must report at least one
operation. A selector that runs nothing cannot produce a success.

`internal/handle` counts Go callback state, including Arrow release state. It
does not count every native dataset, session or scanner object.

An epoch waits for all workers. Native calls that cannot be interrupted can
extend an epoch beyond the requested duration. Run unattended measurements
under an external process timeout; a timeout is a failed, incomplete run, and
the journal retains samples written before the stall.

No forced GC, `FreeOSMemory` or malloc trimming runs during warmup or
measurement. One `runtime.GC()` sample is taken after measurement, labelled
`post_gc`; it is not part of the primary evidence.

The journal is JSONL with one object per line. Phases are `config`, `warmup`,
`measurement`, `post_gc`, `diagnostic`, `error` and `final`. Samples include
phase-relative and run-relative timestamps, so the curve can be reconstructed
without trusting the final summary alone. The final summary reports:

- sample count and measured duration;
- initial, final and peak RSS;
- early and late post-warmup window means, minima and maxima;
- late-minus-early mean, and a least-squares RSS slope in bytes per second;
- baseline, maximum and delta registered Go callback handle counts;
- operations, completed operations, errors and per-workload cancellation
  outcomes.

## Diagnostic retention control

`-diagnostic-retain-bytes` activates a separate control after the measurement:

```sh
/tmp/lance-go-soak -warmup=10s -duration=1m -sample-interval=2s \
  -diagnostic-retain-bytes=268435456 -jsonl=/tmp/soak-diagnostic.jsonl
```

The control opens the main dataset, repeatedly calls the public `Take` API for
a small fixed row set, keeps the caller-owned result batches without releasing
them, reads one byte from every Arrow buffer page to establish real residency,
and samples RSS before, while held and after releasing all batches. It stops
after the requested byte budget, with at most one additional `Take` call of
overshoot; a call can return multiple batches.

This is deliberately independent of normal operation and is never enabled by
default. It is a sensitivity check for whether this harness and the allocator
expose retained native bytes at all. The Go heap is not used as a purported
native control.

Native allocator caches and mappings can be shared or reused. RSS may therefore
fail to rise while batches are deliberately retained, or may not fall after
release. The journal reports the actual observation; the control does not force
an expected RSS assertion.

## Interpreting RSS

`rss_bytes` is the total resident set of the whole process. It includes the Go
runtime, native lance-c allocations, shared library mappings, file-backed pages
and allocator metadata. It is not exact native-live-byte accounting, and
`rss_bytes - go_heap_alloc_bytes` must never be reported as native allocation.

RSS curves are affected by native allocator retention, mmap reuse, lazy page
commit, memory shared with other processes, the loaded library mapping and the
kernel's page cache accounting. A plateau may hide allocator-retained free
blocks, and a rise may come from mappings rather than live native objects. The
early/late comparison and slope are descriptive evidence for the specific run
and configuration, not a universal leak detector.

This evidence is Linux-specific. It does not establish behavior on Windows,
macOS or every allocator configuration.

The optional growth budget is a caller-selected screening threshold for one
run. It is not proof of absence of leaks and its failure is not proof of a leak.

## Journal and provenance for review

Keep the JSONL file and the built binary. On the run host:

```sh
sha256sum /tmp/lance-go-soak
go version -m /tmp/lance-go-soak > /tmp/lance-go-soak.modules.txt
```

`go version -m` is a metadata read and does not execute the binary. The journal
`config` record includes the build's Go version, toolchain and VCS settings,
SDK and dependency module versions, the loaded library path and SHA-256, and
the artifact module version that was linked. The library hash identifies the
bytes loaded on that host; it does not verify an upstream lance-c commit. The
pinned commit, when declared, is build-time provenance from the artifact module
and is distinct from the runtime file hash.

A useful review retains the short smoke, the long run, any diagnostic control
and any budget run as separate JSONL files, with the corresponding binary hash
and module metadata.

## What this does not cover

- It does not replace the existing short lifetime, ownership and error-code
  tests.
- It does not establish native-live-byte accounting or allocator statistics.
- It does not validate Windows or macOS runtime behavior.
- It does not add a CI job. Long runs are explicit operator actions on a target
  host.
