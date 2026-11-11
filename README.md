# lance-go

Pure-Go bindings for [Lance](https://lancedb.github.io/lance/), built on the
official `lance-c` C ABI without cgo.

## Status

This repository currently contains the foundation slice: library loading,
minimal bindings and native error handling. It has no public dataset API yet,
does not bundle native artifacts, and does not provide zero-config operation.

| Area | State |
| --- | --- |
| `internal/loader`: discover and load `liblance_c` on Linux/macOS | Implemented, unvalidated |
| `internal/ffi`: dataset open/close, native error extraction | Implemented, unvalidated, minimal |
| `internal/cdata`: Arrow C Data Interface | Not started |
| Public Go API | Not started |
| Embedded per-platform artifacts, zero-config operation | Not implemented; later slice |
| Windows | Not implemented (needs `LoadLibrary`; purego's `Dlopen` is Unix-only) |
| CI matrix | Not configured |

Until the public API exists, the packages in `internal/` are for contributors
and for the next slices of issue
[#1](https://github.com/lance-format/lance-go/issues/1).

## How loading works

`internal/loader` resolves the native library once per process:

1. `LANCE_LIB_PATH`, which must point at an existing regular file. When set,
   it is the only candidate. A bad value fails with the path in the error
   instead of falling back to another library.
2. Otherwise the platform soname (`liblance_c.so` on Linux,
   `liblance_c.dylib` on macOS) is opened through the system dynamic loader,
   honoring `LD_LIBRARY_PATH`, `DYLD_LIBRARY_PATH`, ldconfig and the usual
   system directories.

The library is never unloaded. `lance-c` owns a Tokio runtime, thread-local
error state and handles that may outlive a dataset, so unloading while native
threads or callbacks may still run is not safe. Changing `LANCE_LIB_PATH`
after a successful load has no effect in that process.

Build tags:

- Default builds carry no embedded artifacts yet, so they use the discovery
  order above.
- `-tags lance_external_lib` marks a build that must never embed artifacts.
  The tag currently only changes the diagnostics; it becomes load-bearing
  when the artifact modules land.

All Go code is `CGO_ENABLED=0` compatible. `purego` is used only for
`dlopen`/`dlsym` and call trampolines.

## Building liblance_c

The Go module does not vendor the C header or the native library. To build
both from the exact upstream revision this binding targets:

```console
./scripts/build-lance-c.sh
```

The script reads the pinned commit and header SHA-256 from
`internal/ffi/upstream.go`, clones/fetches `lance-c` into
`${XDG_CACHE_HOME:-$HOME/.cache}/lance-c-build/src`, checks out the pinned
commit, verifies the header hash, builds with CMake/Corrosion in Release mode,
and installs into `dist/lance-c`. Use `--out DIR` to choose another prefix and
`LANCE_C_WORK_DIR` to move the checkout/build cache.

Requirements: git, CMake 3.22+, a C compiler, Cargo with Rust 1.94.0 or newer,
network access for crates and CMake's Corrosion dependency. The first build
compiles all of Lance and takes a long time.

For a quick smoke test, the upstream `v0.1.9` Linux/macOS release tarball also
works: the five symbols bound here have identical signatures at the `v0.1.9`
tag and at the pinned commit. A source build from the pin is authoritative.

## Pinned ABI and verification

`internal/ffi/upstream.go` records:

- upstream repository and the latest release at pin time,
- the exact commit (`upstreamCommit`),
- the SHA-256 of `include/lance/lance.h` at that commit.

`lance-c` has no version symbol yet (tracked upstream as
[lance-format/lance-c#165](https://github.com/lance-format/lance-c/issues/165)), so a
loaded library cannot be identified at runtime. Provenance is checked at build
time instead:

```console
git -C /path/to/lance-c show 875c2569ac3467b56a721db0cf9e4dff9b4b2cc5:include/lance/lance.h > lance.h
LANCE_C_HEADER=./lance.h go run ./scripts/check-abi-symbols.go
```

The checker refuses a header whose SHA-256 differs from the pin, then verifies
that every bound symbol is declared, that C and Go agree on arity, and that
parameters and return values are pointer or integer classes that `purego` maps
compatibly. It does not model struct layout or distinguish same-width integer
types beyond `int32`/`uint32`/`uint64`.

## Native error contract

`lance-c` reports failures through Rust thread-local storage, not return
values:

1. The failing call runs while the goroutine is locked to its OS thread.
2. `lance_last_error_code()` is read first.
3. `lance_last_error_message()` is then read; it takes the pending error out
   of TLS and returns an owned C string.
4. The message is copied into Go memory and released with
   `lance_free_string()`.

Reading the message before the code would always observe `LANCE_OK`, and using
the message after the free would be a use-after-free. `internal/ffi` owns this
order in `(*bindings).takeError`; keep native calls that can fail on the same
locked thread until the error has been consumed.

`Dataset.Close` is idempotent: only the first call reaches
`lance_dataset_close`, so double closes cannot free the same handle twice.

## Testing

Unit tests run without the native library. Native tests skip unless
`LANCE_LIB_PATH` names a built `liblance_c`; a configured but broken library
fails instead of skipping.

```console
gofmt -l .
go vet ./...
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go build -tags lance_external_lib ./...
go test ./...

export LANCE_LIB_PATH="$PWD/dist/lance-c/lib/liblance_c.so"   # or .dylib
go test ./internal/loader ./internal/ffi -v
```

The native tests cover `dlopen`/`dlsym`, the singleton loader, the
thread-local error semantics on one locked OS thread, and failed
`lance_dataset_open` calls. They do not yet open a valid dataset: `lance-c`
has no in-memory dataset fixture, and the write path belongs to a later slice.

Validation status: the commands above have not been run on the authoring host;
it intentionally does not compile Go code. They are the acceptance commands for
the remote validation worker. Do not treat this slice as verified until their
output is recorded.

## Layout

```text
internal/loader/   discovery, dlopen, symbol lookup, process-wide singleton
internal/ffi/      purego bindings, dataset open/close, native error extraction
scripts/           build-lance-c.sh, check-abi-symbols.go
```

Planned boundaries: `internal/cdata` will own Arrow C Data structures and
release callbacks; it must not be imported by `internal/loader` or
`internal/ffi`. Native calls and error extraction stay in `internal/ffi`.

## Known gaps

- No runtime library version check; see upstream #165.
- No `lance_c_version` or `lance_clear_last_error`, so a failed call cannot be
  probed without consuming its error.
- No embedded artifacts or zero-config download yet. Both build strategies
  currently rely on `LANCE_LIB_PATH` or the system dynamic loader; the
  `lance_external_lib` tag is the switch artifact modules will key off.
- No Windows loader.
- No native test that opens a real dataset; that needs a fixture or the write
  path.
