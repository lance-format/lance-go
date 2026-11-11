# Packaged lance-c artifacts

This directory documents the packaging interface between the loader and the
release artifacts. It does not contain a native library or a generated
artifact module.

The repository currently ships the cache and artifact-generator
infrastructure. It does **not** yet ship compiled-in artifact modules or the
selectors that activate them, so a default build still discovers a system
`liblance_c` or an explicit `LANCE_LIB_PATH`. Full zero-configuration for a
consumer requires the release packaging described below.

## Layout produced by the generator

```text
<out>/
  module/
    go.mod
    artifact.go        # embeds the payload and declares metadata
    liblance_c.so.zst  # or liblance_c.dylib.zst
  selector/
    lance_bundle_<goos>_<goarch>.go
```

`module/` is a data-only Go module. Its `artifact.go` imports only `embed`; it
does not link, load, or execute native code. The generated `go.mod` targets Go
1.23 and declares no requirements.

`selector/` contains a file for the root package. Its build constraint is
`!lance_external_lib && <goos> && <goarch>`. It imports the artifact module
and registers the descriptor with `internal/loader` in an `init` function. Copy
the selector into the root package after publishing the artifact module and
adding the module requirement. That activation step belongs to the release
process, not to this generator.

## Generating an artifact

Build lance-c from the exact commit pinned in `internal/ffi/upstream.go`, then
package that build. Do not substitute a release archive with a different
header: the pinned commit and the `v0.1.9` archive have different headers.

```sh
scripts/sync-lance-c.sh \
  --library /path/to/liblance_c.so \
  --header /path/to/include/lance/lance.h \
  --target linux/amd64 \
  --out /tmp/lance-go-artifact-linux-amd64
```

The script reads `upstreamRepo`, `upstreamCommit`, and
`upstreamHeaderSHA256` from `internal/ffi/upstream.go`. It verifies the header
hash, target architecture, 64-bit ELF/dylib kind, and library basename, then
refuses to overwrite an existing output directory. Supported targets are
Linux and macOS on amd64 and arm64.

The commit and header hash written into the generated module are
packaging-time provenance claims. lance-c exposes no runtime version symbol,
so the loader cannot prove from the shared library which commit built it. The
packaging host and the release process must supply a library built from the
pinned commit.

## Runtime cache

In a default build with a registered bundle, `loader.Load` verifies the
compressed payload and materializes the library under:

```text
<os.UserCacheDir>/lance-go/<uncompressed-sha256>/<libraryName>
```

The cache directory and entry directory are private (`0700`); the library is
read-only (`0500`). A `.lock` file coordinates extraction across processes.
Existing bytes are checked for exact size and SHA-256 before `dlopen`. A
corrupt regular entry is removed and replaced under the lock. Extraction
writes a temporary file in the same directory, verifies the decompressed size
and hash, syncs it, and publishes it with an atomic rename. Failed extraction
never publishes a loadable file.

`LANCE_LIB_PATH` takes precedence over a registered bundle. Once a bundle is
selected, cache, extraction, and `dlopen` errors are returned with the module
and target; the loader does not silently fall back to a system copy.

## Dependencies for root integration

The loader's default build adds one direct dependency:

```text
require github.com/klauspost/compress v1.18.0
```

Matching `go.sum` entries:

```text
github.com/klauspost/compress v1.18.0 h1:c/Cqfb0r+Yi+JtIEq73FWXVkRonBlf0CRNYc8Zttxdo=
github.com/klauspost/compress v1.18.0/go.mod h1:2Pp+KzxcywXVXMr50+X0Q/Lsb43OQHYWRCY2AiWywWQ=
```

`v1.18.0` is the newest non-retracted release compatible with the module's Go
1.23.0 requirement. `v1.18.1` is retracted; `v1.19.x` requires Go 1.24. The
generated artifact module itself has no requirements, and the generator uses
only that same compression package plus the standard library.

`lance_external_lib` builds compile out `bundle.go`, `cache.go`,
`lock_unix.go`, and the zstd dependency path. They still use the platform
loader and `LANCE_LIB_PATH` support in `loader.go`.

## Verification still owned by the release process

The loader and generator tests do not require a real native library: the
loader tests use a tiny arbitrary payload to exercise cache and extraction
behavior, and the generator tests use synthetic ELF/Mach-O headers for target
validation. They do not prove that a packaged library loads or that the public
API works.

After generating real modules, the release process must:

1. Publish the artifact module and add its requirement to the root module.
2. Copy each selector into the root package and build with the corresponding
   platform constraint.
3. On a cold consumer cache, verify that an untagged import loads the bundled
   library without `LANCE_LIB_PATH` and without a system installation.
4. Verify the `lance_external_lib` build has no bundle payload and still uses
   only explicit/system discovery.
5. Run the public API acceptance suite on NieR with the real stripped library.

Until those steps pass, this work delivers cache and generator infrastructure,
not activated zero-configuration artifacts.
