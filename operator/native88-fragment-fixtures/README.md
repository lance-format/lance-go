# native88 fragment-mutation fixture producer

Task-only tooling for the later NieR acceptance run of the retained native88
C11 and C++17 fragment-mutation consumers. This directory is excluded from the
delivery PR and is not part of the SDK.

The command creates ten independent, disposable Lance datasets from the
upstream fixture recipe and validates each one after reopening it. It does not
compile or run any consumer, and it does not claim consumer acceptance. The
parent owns the NieR runtime steps below.

## Provenance

- Upstream fixture source: `lance-c` commit `88c07cf8ccafebf579e769416251be370b45180e`
- Retained native header SHA-256: `c23406d5c683133eb647cfeb150fa05182ffc8ba7da283b3902dbb4bf0926c6c`
- Fixture behavior: `tests/compile_and_run_test.rs` `create_fragment_mutation_dataset_on_disk` and `test_native_fragment_mutation_consumers`
- Consumer sources (used unmodified): `tests/cpp/test_fragment_mutation.c` and `tests/cpp/test_fragment_mutation.cpp`

The producer writes only through the existing public `Create`, `Append`,
`DropColumns` and `Delete` operations. It does not use the fragment mutation
APIs that the consumers will exercise.

## What is generated

`-output` must name a directory that does not exist. The producer refuses to
reuse or delete an existing path.

```text
<output>/c-add      <output>/cpp-add
<output>/c-merge    <output>/cpp-merge
<output>/c-update   <output>/cpp-update
<output>/c-replace  <output>/cpp-replace
<output>/c-delete   <output>/cpp-delete
<output>/manifest.json
```

Each fixture is written as:

1. create ids `1,2,3,4` with `max_rows_per_file=4` and stable row IDs enabled
2. append ids `5,6,7,8`
3. append ids `9,10,11,12`
4. drop column `stale`
5. delete `id IN (2, 10)`

The expected final state is version 5, 10 live rows, and 3 fragments. The
`id` field is non-null Int64 with metadata `role=id`; `value` is nullable Int32
with metadata `role=value`; `profile` is a nullable struct with metadata
`role=nested` containing nullable `score` Int32 (metadata `nested=score`) and
nullable `tag` Utf8. The original field IDs are `id=0`, `value=1`,
`profile=3`, `profile.score=4`, `profile.tag=5`; the gap at 2 is retained after
dropping `stale`. Live ids are `[1,3,4,5,6,7,8,9,11,12]`, with
`value=id*10`, `score=id*100`, `tag=tag-<id>`, and the stable row ID of id 3 is
2.

The producer validates all of these facts on every reopened copy, including
fragment order and per-fragment physical/deleted/logical rows
(`4/1/3`, `4/0/4`, `4/1/3`), before writing `manifest.json`. Rows are collected
by id, not by scan order. Any mismatch stops the run with a non-zero exit and
no manifest. A failed run leaves partial data for inspection; use a new output
path on the next attempt.

`manifest.json` records the pinned upstream commit and header checksum, the
validated schema with field IDs, and for each fixture its URI, version, row
count, fragment facts and sorted live rows with stable row IDs. The checksum
is the source provenance recorded by this tool, not a measurement of the
binary that runs later; the acceptance steps hash the retained header and
library separately.

## Build (registered module checkout)

Run these from the registered `lance-go` module checkout, not from
`operator/`:

```bash
cd /path/to/lance-go          # contains go.mod
mkdir -p /path/to/fixture-bin
go build -trimpath -o /path/to/fixture-bin/native88-fragment-fixtures \
  ./operator/native88-fragment-fixtures
```

The default linux/amd64 build embeds the packaged native library from the
`artifacts/linux-amd64` module. That bundle targets Ubuntu 24.04.5 / glibc
2.39. No Go build, test, vet or run is performed on the Adam staging host.

If the NieR run must use the operator-provided retained library instead of the
embedded bundle, build with the external tag and point `LANCE_LIB_PATH` at the
retained `liblance_c.so`:

```bash
go build -tags lance_external_lib -trimpath \
  -o /path/to/fixture-bin/native88-fragment-fixtures \
  ./operator/native88-fragment-fixtures

LANCE_LIB_PATH=/operator/provided/native88/lib/liblance_c.so \
  /path/to/fixture-bin/native88-fragment-fixtures -output /fresh/path/fixtures
```

Do not mix the header from one native build with a library from another.

## Generate the ten fixtures

The leaf output directory must be fresh. Creating a temporary parent is safe
because only the leaf is passed to `-output`:

```bash
FIXTURE_PARENT=$(mktemp -d)
/path/to/fixture-bin/native88-fragment-fixtures \
  -output "$FIXTURE_PARENT/fixtures"
FIXTURES="$FIXTURE_PARENT/fixtures"

test -f "$FIXTURES/manifest.json"
python3 -m json.tool "$FIXTURES/manifest.json" >/dev/null
```

Successful output ends with `consumer acceptance: NOT RUN`. That is expected:
the producer only prepares and validates the datasets.

## Native consumer acceptance (NieR, parent-owned)

The retained native88 include and library directories are operator-provided.
Set them explicitly; this runbook makes no assumption about their paths. Both
must come from the retained native88 build whose header is pinned above.

```bash
NATIVE88_INCLUDE=/operator/provided/native88/include   # contains lance/lance.h
NATIVE88_LIB=/operator/provided/native88/lib           # contains liblance_c.so
LANCE_C_REPO=/path/to/lance-c                          # any checkout; content is read by commit
WORK=$(mktemp -d)
LOGS="$WORK/logs"
SRC="$WORK/src"
mkdir -p "$LOGS" "$SRC"

sha256sum "$NATIVE88_INCLUDE/lance/lance.h" | tee "$LOGS/header.sha256"
# The digest must be c23406d5c683133eb647cfeb150fa05182ffc8ba7da283b3902dbb4bf0926c6c.
sha256sum "$NATIVE88_LIB/liblance_c.so" | tee "$LOGS/library.sha256"

git -C "$LANCE_C_REPO" show \
  88c07cf8ccafebf579e769416251be370b45180e:tests/cpp/test_fragment_mutation.c \
  > "$SRC/test_fragment_mutation.c"
git -C "$LANCE_C_REPO" show \
  88c07cf8ccafebf579e769416251be370b45180e:tests/cpp/test_fragment_mutation.cpp \
  > "$SRC/test_fragment_mutation.cpp"
```

Compile only those two original sources. Do not rebuild Rust, do not use
`cargo`, and do not change the consumer sources:

```bash
clang -std=c11 -Wall -Wextra \
  -o "$WORK/fragment_mutation_c" "$SRC/test_fragment_mutation.c" \
  -I"$NATIVE88_INCLUDE" -L"$NATIVE88_LIB" -llance_c \
  -Wl,-rpath,"$NATIVE88_LIB"

c++ -std=c++17 -Wall -Wextra \
  -o "$WORK/fragment_mutation_cpp" "$SRC/test_fragment_mutation.cpp" \
  -I"$NATIVE88_INCLUDE" -L"$NATIVE88_LIB" -llance_c \
  -Wl,-rpath,"$NATIVE88_LIB"
```

Record the resolved libraries before running either main:

```bash
ldd "$WORK/fragment_mutation_c"   > "$LOGS/ldd-c.txt"   2>&1
ldd "$WORK/fragment_mutation_cpp" > "$LOGS/ldd-cpp.txt" 2>&1
```

Run the C main first, then the C++ main. Each main mutates the five datasets
given to it, so run each fixture exactly once and regenerate the whole set for
a rerun. The five dataset arguments are `add`, `merge`, `update`, `replace`
and `delete`, in that order.

```bash
set +e
LD_DEBUG=libs LD_DEBUG_OUTPUT="$LOGS/c-loader" \
  "$WORK/fragment_mutation_c" \
  "$FIXTURES/c-add" "$FIXTURES/c-merge" "$FIXTURES/c-update" \
  "$FIXTURES/c-replace" "$FIXTURES/c-delete" \
  > "$LOGS/c.stdout" 2> "$LOGS/c.stderr"
c_exit=$?
set -e
printf '%s\n' "$c_exit" | tee "$LOGS/c.exit"

set +e
LD_DEBUG=libs LD_DEBUG_OUTPUT="$LOGS/cpp-loader" \
  "$WORK/fragment_mutation_cpp" \
  "$FIXTURES/cpp-add" "$FIXTURES/cpp-merge" "$FIXTURES/cpp-update" \
  "$FIXTURES/cpp-replace" "$FIXTURES/cpp-delete" \
  > "$LOGS/cpp.stdout" 2> "$LOGS/cpp.stderr"
cpp_exit=$?
set -e
printf '%s\n' "$cpp_exit" | tee "$LOGS/cpp.exit"

if [ "$c_exit" -ne 0 ] || [ "$cpp_exit" -ne 0 ]; then
  echo "native88 consumer failure: c=$c_exit cpp=$cpp_exit; inspect $LOGS" >&2
  exit 1
fi
```

`LD_DEBUG_OUTPUT` writes loader traces as `c-loader.<pid>` and
`cpp-loader.<pid>` in `$LOGS`, keeping the consumer output separate. Acceptance
requires both recorded exit codes to be 0 and both full main outputs to show
their checks passing. Preserve `manifest.json`, the four output streams, the
exit files, `ldd-*.txt`, the loader traces, the header/library digests, and the
compiled binaries' build logs.

If a consumer fails, capture the full failing assertion and keep the fixture
copy. Do not patch the producer, the manifest, the fixtures, or the original
consumer sources to force a pass; report the mismatch to the parent.

## Boundary

- Producer runtime: parent-owned on NieR; never run on Adam during this task.
- Consumer compilation and execution: parent-owned on NieR.
- Start this acceptance sequence only after the parent confirms the active
  NieR 9912 worker has settled.
- No Rust rebuild and no fixture-harness debugging are part of acceptance.
- This directory and its binary are task artifacts, not delivery SDK content.
