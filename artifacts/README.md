# Packaged lance-c artifacts

This directory records metadata snapshots for the published artifact modules.
Each `artifacts/<target>/` snapshot contains `artifact.go`, `go.mod`, and
`LICENSE`, byte-identical to its pinned publication. Compressed payloads live
only in the complete modules at their publication commits; those commits and
branches must remain reachable. The root SDK tree does not duplicate them.

The snapshots are not independently buildable or publishable without their
payloads. SDK builds resolve the complete modules through the root `go.mod`
requirements. Public-consumer checks compare the downloaded metadata to these
snapshots and verify payload size and SHA-256 against that metadata, as well
as the public module checksum and actual loaded-library identity.

Keeping payloads out of the SDK tree limits the Git data the public Go proxy
must fetch before it can create the small root-module archive. Native workflows
produce complete, unpublished candidate modules separately; publish an accepted
candidate on its artifact branch, then adopt its metadata, requirement and
selector without merging the payload into the SDK tree.

## Supported consumer platforms

The supported consumer targets for current and future releases are
`linux/amd64`, `linux/arm64`, `darwin/arm64`, `windows/amd64`, and
`windows/arm64`. Consumers build with `CGO_ENABLED=0` and use the official
prebuilt library from the companion module for their SDK version and target.
Source builds and cgo-based consumption are outside the supported installation
paths. The published `v0.1.0-preview.1` also included `darwin/amd64`; that row
is historical after [#54](https://github.com/lance-format/lance-go/pull/54) and
no new release selects it. See the README
[supported consumer configuration](../README.md#supported-consumer-configuration)
and [platform and runtime scope](#platform-and-runtime-scope) below.

## Active Unix bundles

The root module requires these three published Unix data-only modules, plus
the two Windows modules described below:

```text
github.com/lance-format/lance-go/artifacts/linux-amd64   v0.1.0-preview.2
github.com/lance-format/lance-go/artifacts/linux-arm64   v0.1.0-preview.2
github.com/lance-format/lance-go/artifacts/darwin-arm64  v0.1.0-preview.2
```

The metadata snapshots are nested modules, so root builds resolve requirements
through the Go module proxy rather than using the local directories. Complete
sources and payloads remain at publication commits
`73cf23dc609cb81a7316fa07f91aca555b2c6e88` (Unix) and
`a0219baa9da8a6b282f18a9bf219ff4e9c919970` (Windows). The preview.1 companion
versions remain available at their original publication commits, branches,
and release notes; the current tree carries only metadata snapshots.

The Intel macOS module is retired for new releases. Its metadata snapshot
remains at `artifacts/darwin-amd64/` and its published module and tags remain
at their original revisions, but the root module no longer requires it and no
current selector registers it.

### linux/amd64 payload

The published `linux/amd64` module is
`github.com/lance-format/lance-go/artifacts/linux-amd64` at
`v0.1.0-preview.2`.

| Property | Value |
| --- | --- |
| Target | `linux/amd64` |
| Library | `liblance_c.so` |
| Compressed size | 87177503 bytes |
| Compressed SHA-256 | `1de588c93d23b5ecbe0288ce67e7ba236089d3b66e37d55f565812937b26eba0` |
| Uncompressed size | 342441416 bytes |
| Uncompressed SHA-256 | `8e00a4c9fd401d0e99723222d6dfeea47855c57a2e442409b1c32dfed0559c13` |
| Upstream release | `lance-c v0.2.0-beta.1` |
| Upstream commit | `e2564b89be36bb18f5beff6d673daf9dfff8c986` |
| Header SHA-256 | `90e52d19f7cedb6cd52a83024927453f0ded5f13e24ed2adae8684cd9d4b0d8d` |

### Published Unix companion payloads

The active companions use `UpstreamCommit`
`e2564b89be36bb18f5beff6d673daf9dfff8c986` and `HeaderSHA256`
`90e52d19f7cedb6cd52a83024927453f0ded5f13e24ed2adae8684cd9d4b0d8d`. The
`darwin/amd64` module is historical and is not part of this table.

| Target | Compressed size | Compressed SHA-256 | Uncompressed size | Uncompressed SHA-256 |
| --- | --- | --- | --- | --- |
| `linux/arm64` | 82989526 | `55c3727cf5f68815d36c887dc1aa9762303ff751e91dc9a019be5c5ff437203b` | 306949136 | `725e77b3bc86b5301750130bef7ff62cea004e9f71bd19552720cfe6b304d324` |
| `darwin/arm64` | 76875890 | `e34343a21ab26f62d657fb2a521e26526d87c4b933016271f0188c0e194102b4` | 288099632 | `221acab4931254991ff22549aac6f439b48ba9f2eac52e79bb43467aebf52385` |

The active generated selectors are
`internal/bundled/lance_bundle_linux_arm64.go` and
`internal/bundled/lance_bundle_darwin_arm64.go`. Their build constraints are
`!lance_external_lib && <goos> && <goarch>`.

`internal/bundled/lance_bundle_linux_amd64.go` is the generated selector.
Its build constraint is `!lance_external_lib && linux && amd64`. The `init`
function registers the descriptor, so a default `linux/amd64` import of the
root package or the standalone namespace package uses the bundle after
`LANCE_LIB_PATH` is absent and before the system loader. `LANCE_LIB_PATH` still takes precedence when set. Once a bundle
is registered, verification, extraction, and `dlopen` errors are returned with
the bundle module in the error; the loader does not fall back to a system copy.
There is no `darwin/amd64` selector: that target keeps the metadata snapshot
and published module, but a default build falls back to the explicit or system
loader path.

The compressed payload is data. Importing the artifact module does not link,
load, or execute native code; the loader decompresses and maps it on first use.

## Published Windows companions

The root module requires `artifacts/windows-amd64` and
`artifacts/windows-arm64` at `v0.1.0-preview.2`.
Both public proxy archives and checksum-database entries match publication
commit `a0219baa9da8a6b282f18a9bf219ff4e9c919970`. The four-file modules
contain `artifact.go`, `go.mod`, `LICENSE`, and `lance_c.dll.zst`.

| Target | Compressed size | Compressed SHA-256 | DLL size | DLL SHA-256 |
| --- | --- | --- | --- | --- |
| `windows/amd64` | 99581215 | `0fe6f237ee1e8e2303fb14353d9e9d3539c8051bc859cbfa7cd5525072688769` | 402987008 | `fecd7f52765fcefbb80a8157882eae83192485303dcc80c41dcd50c9f6d237f1` |
| `windows/arm64` | 89412335 | `ba266ee4306b847e515b5114796fed08758fdcbda975be19daefef508c12597e` | 329247744 | `5c76312e299ee1c68876f4176f2cb2f269511e37cd81a1cc262db716ffe6200c` |

Both use upstream `lance-c v0.2.0-beta.1` at
`e2564b89be36bb18f5beff6d673daf9dfff8c986`. Their generated selectors are
`internal/bundled/lance_bundle_windows_<arch>.go`, with build constraint
`!lance_external_lib && windows && <arch>`. `LANCE_LIB_PATH` retains
precedence over a registered bundle.

For the official `lance-c-v0.2.0-beta.1` Windows assets, both
`include/lance/lance.h` files are CRLF copies of the canonical LF header.
`scripts/sync-lance-c.sh` validates the supplied header bytes against the
canonical pin; it does not normalize them. Create a canonical LF copy before
packaging and verify it, while leaving the DLL extracted from the archive
unchanged:

```sh
python3 - "$src" "$dst" <<'PY'
import sys
from pathlib import Path

Path(sys.argv[2]).write_bytes(Path(sys.argv[1]).read_bytes().replace(b"\r\n", b"\n"))
PY
sha256sum "$dst"
# 90e52d19f7cedb6cd52a83024927453f0ded5f13e24ed2adae8684cd9d4b0d8d
```

The raw archive header hashes are
`649e88841ebf09faa97354cae797de1376e0ada2ea69189c6fd4c4c6631aad66` for both
`lance-c-v0.2.0-beta.1-x86_64-pc-windows-msvc.tar.xz` and
`lance-c-v0.2.0-beta.1-aarch64-pc-windows-msvc.tar.xz`. Packaging records the
canonical `HeaderSHA256`
`90e52d19f7cedb6cd52a83024927453f0ded5f13e24ed2adae8684cd9d4b0d8d`.

The published payloads decompress byte-for-byte to the DLLs in the official
`v0.2.0-beta.1` Windows archives. The C release workflow verified the staged
PE images and public export set; the Go public-consumer lane verifies the
loaded DLL, cache identity, OS-only import closure and external supplied-DLL
path. Older Windows versions and redistributable CRT deployment remain
unclaimed.

## Platform and runtime scope

Three Unix targets have active bundles. The official `lance-c v0.2.0-beta.1`
Linux packages are generic x86_64 and AArch64 builds with glibc 2.28 as the
verified floor; the `linux/amd64` and `linux/arm64` libraries passed the local
ELF/export/glibc checks against that floor. The macOS package uses deployment
target 11.0 and is tested on macOS 15.x hosts. The retired `darwin/amd64`
module remains published with its historical runtime evidence, but new
releases neither require nor select it. Do not assume these Unix payloads work
on older glibc/macOS, musl or other architectures. Windows uses separately
validated PE DLLs, as recorded above.

The cross-build matrix in `ci.yml` compiles other targets but does not run them
and does not publish artifacts. The Unix native workflow rebuilds candidate
libraries; publication and selector adoption are separate release steps.
Windows static-CRT packaging and staged-consumer validation are part of the
official C release; the Go public-consumer lane verifies the selected DLL
and its runtime closure.

A bundle is fetched through the normal Go module proxy when a default build for
a published target compiles its artifact package. This is not a
zero-download installation. Commands that load all modules, including `go get`
and `go mod tidy`, may download every companion archive because Go can load
packages for all build tags. The public-consumer lane records that installation
set separately, then measures each default and namespace target build from
fresh empty caches.

## Public-consumer acceptance

`scripts/public-consumer/run.sh` resolves an exact root revision from the
public proxy and builds standalone default, namespace-only, and external
consumers on one native runner. It verifies the root archive and source tree,
the artifact selected by the built binary, payload and extracted-library bytes,
safe cache modes, and the roundtrip output. It records the complete `go get`
download set against the root module graph, then requires each default and
namespace target build to fetch only the selected artifact payload archive from
fresh empty caches. External target downloads are recorded without a
zero-download claim. The three hosted matrix entries are Linux amd64 and arm64
plus macOS arm64; the `darwin/amd64` executions recorded above are historical
and old tags keep their original driver revisions. The lane is
public-distribution acceptance, not a native rebuild or a full runtime
regression. See [scripts/public-consumer/README.md](../scripts/public-consumer/README.md).

## External build contract

`-tags lance_external_lib` excludes the generated selector, `bundle.go`,
`cache.go`, `lock_unix.go`, the decompression dependency path, and the artifact
package from compilation and embedding. The artifact module remains in
`go.mod` and the module graph, and module-graph commands may still download its
archive. External builds use only `LANCE_LIB_PATH` and system discovery.

## Layout produced by the generator

```text
<out>/
  module/
    go.mod
    artifact.go        # embeds the payload and declares metadata
    liblance_c.so.zst  # or liblance_c.dylib.zst / lance_c.dll.zst
  selector/
    lance_bundle_<goos>_<goarch>.go
```

`module/` is a data-only Go module. Its `artifact.go` imports only `embed`; it
does not link, load, or execute native code. The generated `go.mod` targets Go
1.23 and declares no requirements.

The generator packages the library it is given; it does not strip or otherwise
modify that library. For a production candidate, strip a copy first as
described below and pass the stripped copy to the generator.

`selector/` contains a file for `internal/bundled`. Its build constraint is
`!lance_external_lib && <goos> && <goarch>`. It imports the artifact module
and registers the descriptor with `internal/loader` in an `init` function. Copy
the selector into `internal/bundled` after publishing the artifact module and
adding the module requirement. That activation step belongs to the release
process, not to this generator.

## Generating an artifact

Use the shared library and header from the official `lance-c` release archive
matching the tag, commit and header in `internal/ffi/upstream.go`. Verify the
archive checksum before extracting it. A source-built alternative is allowed
only when it is built from the same pinned commit and header. Do not substitute
a different release archive.

```sh
scripts/sync-lance-c.sh \
  --library /path/to/liblance_c.so \
  --header /path/to/include/lance/lance.h \
  --target linux/amd64 \
  --out /tmp/lance-go-artifact-linux-amd64
```

The script reads `upstreamRepo`, `upstreamCommit`, and
`upstreamHeaderSHA256` from `internal/ffi/upstream.go`. It verifies the header
hash, target architecture, library format and basename, then refuses to
overwrite an existing output directory. Supported targets are `linux/amd64`,
`linux/arm64`, `darwin/arm64`, `windows/amd64`, and `windows/arm64`. Linux
requires a 64-bit ELF shared object, macOS a thin 64-bit arm64 dylib, and
Windows a 64-bit PE DLL with the matching machine type.

The commit and header hash written into the generated module are
packaging-time provenance claims. lance-c exposes no runtime version symbol,
so the loader cannot prove from the shared library which commit built it. The
packaging host and the release process must supply a library built from the
pinned commit. The active bundles were packaged from
`e2564b89be36bb18f5beff6d673daf9dfff8c986`; their `artifact.go` records the
values in the tables above.

### Preparing a source-built production candidate

Official `v0.2.0` release archives are already stripped. Package their
published shared library directly after verifying the archive, header and
library bytes. The steps below apply only to source-built candidates, which
must be copied, stripped, and verified first:

```sh
out="$RUNNER_TEMP/production/linux-amd64"
scripts/strip-native-library.sh \
  --library "$LANCE_LIBRARY" \
  --header "$LANCE_HEADER" \
  --target linux/amd64 \
  --out "$out"
scripts/check-native-library.sh \
  --library "$out/library/liblance_c.so" \
  --header "$LANCE_HEADER" \
  --target linux/amd64
scripts/sync-lance-c.sh \
  --library "$out/library/liblance_c.so" \
  --header "$LANCE_HEADER" \
  --target linux/amd64 \
  --module-path github.com/lance-format/lance-go/artifacts/linux-amd64 \
  --out "$RUNNER_TEMP/candidate/linux-amd64"
```

The helper copies the exact accepted library into `<out>/library`, strips only
that copy, and fails unless the source library's size and SHA-256 are unchanged.
Linux uses `strip --strip-unneeded`; macOS uses `strip -x`. It records the raw
and stripped hashes and sizes, the strip tool path, hash, version, and argv,
and the source/header/target provenance. It does not rewrite dependencies,
install names, or RPATH. Re-running `check-native-library.sh` on the copy
revalidates the architecture, dependency contract, deployment floor, and every
bound export. The stripped copy is then passed to `sync-lance-c.sh`; the
candidate is not written into the checkout or the install cache.

## Runtime cache

In a default build with a registered bundle, `loader.Load` verifies the
compressed payload and materializes the library under:

```text
<LANCE_CACHE_DIR or os.UserCacheDir>/lance-go/<uncompressed-sha256>/<libraryName>
```

A non-empty `LANCE_CACHE_DIR` selects the cache root on every platform; an
empty value keeps the platform user cache. The layout, permissions, lock and
verification behavior are unchanged. The cache directory and entry directory
are private (`0700`) on Unix, where the library is also read-only (`0500`). On
Windows the cache lives under the per-user `LocalAppData` directory; Go cannot
express Unix permission bits there, so the cached DLL remains writable and
access is inherited from that per-user directory. A `.lock` file coordinates
extraction across processes:
`flock` on Unix and `LockFileEx` on Windows. Existing bytes are checked for
exact size and SHA-256 before the platform loader opens them. A corrupt
regular entry is removed and replaced under the lock. Extraction writes a
temporary file in the same directory, verifies the decompressed size and
hash, syncs it, and publishes it with an atomic rename. Failed extraction
never publishes a loadable file.

`LANCE_LIB_PATH` takes precedence over a registered bundle. Once a bundle is
selected, cache, extraction, and platform-loader errors are returned with the
module and target; the loader does not silently fall back to a system copy.

## Dependencies for root integration

The loader's default build adds two direct dependencies:

```text
require (
    github.com/klauspost/compress v1.18.0
    golang.org/x/sys v0.35.0
)
```

Matching `go.sum` entries:

```text
github.com/klauspost/compress v1.18.0 h1:c/Cqfb0r+Yi+JtIEq73FWXVkRonBlf0CRNYc8Zttxdo=
github.com/klauspost/compress v1.18.0/go.mod h1:2Pp+KzxcywXVXMr50+X0Q/Lsb43OQHYWRCY2AiWywWQ=
golang.org/x/sys v0.35.0 h1:vz1N37gP5bs89s7He8XuIYXpyY0+QlsKmzipCbUtyxI=
golang.org/x/sys v0.35.0/go.mod h1:BJP2sWEmIv4KK5OTEluFJCKSidICx8ciO85XgH3Ak8k=
```

`klauspost/compress` `v1.18.0` is the newest non-retracted release compatible
with the module's Go 1.23.0 requirement. `v1.18.1` is retracted; `v1.19.x`
requires Go 1.24. `golang.org/x/sys` supplies the Windows loader and file-lock
bindings. The generated artifact module itself has no requirements, and the
generator uses only that same compression package plus the standard library.

The root requirement for the artifact modules adds these `go.sum` entries:

```text
github.com/lance-format/lance-go/artifacts/darwin-arm64 v0.1.0-preview.2 h1:WLuakk1rUM2wjyLV9nxZETOIAIhCjUGLGFZbhzBnjFU=
github.com/lance-format/lance-go/artifacts/darwin-arm64 v0.1.0-preview.2/go.mod h1:fQfHodK/kxZC5w9/Q3YPXfAFpacMYvc2TshI9luW6Xw=
github.com/lance-format/lance-go/artifacts/linux-amd64 v0.1.0-preview.2 h1:WINPLF7iX+miMSFX+r1soIi3RypNZm5j7I7PicKWTGk=
github.com/lance-format/lance-go/artifacts/linux-amd64 v0.1.0-preview.2/go.mod h1:hmgfCi9X6+yByjEXjF26x8U4yNmD+NK5tFKAPQ2JXtI=
github.com/lance-format/lance-go/artifacts/linux-arm64 v0.1.0-preview.2 h1:pOdyshAyHjmZ9xTYlIEoyxl0KeKNnA7PkV/SnzyhL+M=
github.com/lance-format/lance-go/artifacts/linux-arm64 v0.1.0-preview.2/go.mod h1:/PZX6MQ09uUxT+97jTAFTnOmBb8Eaya+Fo6/Cx8oR10=
github.com/lance-format/lance-go/artifacts/windows-amd64 v0.1.0-preview.2 h1:lT/A1Yp8v4PTsb5gQigCH9QMW5CvntkKSvmtrkuyD3Q=
github.com/lance-format/lance-go/artifacts/windows-amd64 v0.1.0-preview.2/go.mod h1:poapOGoG8Ov6b55dD9mnGnIvYAtPMZorCUDbhiWhodQ=
github.com/lance-format/lance-go/artifacts/windows-arm64 v0.1.0-preview.2 h1:vqOfOwzGi2W/FqxWW4DRfBCFvrwMqLfV0UpRhjuFpLI=
github.com/lance-format/lance-go/artifacts/windows-arm64 v0.1.0-preview.2/go.mod h1:0TJauP68uQ0eNmpLzDvdN9HDjzS1DzYQXcElTM/mqYE=
```

`lance_external_lib` builds compile out `bundle.go`, `cache.go`,
`lock_unix.go`, `lock_windows.go`, and the zstd dependency path. They still use
the platform loader, `golang.org/x/sys/windows`, and `LANCE_LIB_PATH` support
where the platform needs them.

## Verification status and remaining gates

The adopted native input is the annotated `lance-c v0.2.0-beta.1` tag object
`f19335330b0c2b28bd370e94f5c867c60ce81ed9`, peeled to
`e2564b89be36bb18f5beff6d673daf9dfff8c986`, with canonical header SHA-256
`90e52d19f7cedb6cd52a83024927453f0ded5f13e24ed2adae8684cd9d4b0d8d`. The
header adds `LANCE_SCALAR_RTREE=9` and nine C exports, taking the source export
set from 878 to 887 with no removals. All 603 Go bindings match the new header.

The five release archives, their `.sha256`/`.sha512` pairs and `SHA512SUMS`
were verified against the published assets. Each companion payload decompressed
byte-for-byte to the shared library in the matching official archive, and each
`v0.1.0-preview.2` module resolved through `proxy.golang.org` and
`sum.golang.org`. Compressed sizes and hashes in this file come from
`scripts/verify-lance-c-artifact` and the public module ZIPs, not the earlier
pre-best-compression intermediate metadata log. Full per-target digests are
recorded in the preview.2 release evidence rather than duplicated here.

The preview.1 consumer evidence remains historical and records the older
six-target module set. The published preview.2 release passed the exact-tag
public-consumer gate on all five targets. The
[Unix run 44401996663](https://github.com/lance-format/lance-go/actions/runs/44401996663)
covered linux/amd64, linux/arm64 and darwin/arm64; the
[Windows run 44402002254](https://github.com/lance-format/lance-go/actions/runs/44402002254)
covered windows/amd64 and windows/arm64. All 15 default, namespace-only and
external modes passed, including the default-bundle RTree and raw v2.2 Blob
checks on every target. The
[release record](https://github.com/lance-format/lance-go/releases/tag/v0.1.0-preview.2)
links the checksummed post-tag evidence archive
(`fab6a791e58a392e396363dc62ae8bf85fa4ceb8747c36219e20324e577630c7`,
531242 bytes) and the main CI, Unix native and Windows runs.

The CI default Go lane exercises the real public path: it runs
`TestCreateReopenMetadataAndScan` with `LANCE_LIB_PATH` unset and a fresh
`XDG_CACHE_HOME`, requires a pass rather than a skip, and verifies the unpacked
cache entry's SHA-256. Loader and generator unit tests cover cache reuse and
corruption, cross-process locking, environment precedence, descriptor
validation, and synthetic target headers.

`go get` loads packages for all build tags, so its companion downloads measure
installation behavior, not target selection. The public-consumer lanes record
installation and each fresh-cache target build separately. The preview.1
[Unix run 43284438211](https://github.com/lance-format/lance-go/actions/runs/43284438211)
and [Windows run 43284438526](https://github.com/lance-format/lance-go/actions/runs/43284438526)
remain historical evidence for the older six-target module set. The exact-tag
preview.2 runs above are the current public-consumer record.

Windows module publication, selector activation and hosted cold-cache loading
were verified on the published tag by the Windows run above. Broader
glibc/musl, older-macOS and older-Windows runtime coverage remain unaccepted.
The preview.2 post-tag runs satisfy this tag's public-consumer gate; future
companion tags still need their own post-tag acceptance.

## Native Unix validation workflow

`.github/workflows/unix-native.yml` builds and tests the pinned lance-c source
on native runners for `linux/amd64`, `linux/arm64`, and `darwin/arm64`. The
runners are `ubuntu-24.04`, `ubuntu-24.04-arm`, and `macos-15`. The workflow
runs on pull requests and `main` pushes that touch native inputs, and on manual
dispatch.

Each job restores or builds the exact pinned source through
`scripts/build-lance-c.sh`, validates the install cache with
`scripts/lance-c-cache.sh`, and runs `scripts/check-native-library.sh` against
the installed library and header. That check verifies the pinned header hash,
object format and architecture, direct dependencies, the libc or macOS
deployment floor, and that every symbol in the Go binding table is exported. It
reports observed export and dependency counts; it does not assert that the
library exports only `lance_` symbols.

The workflow then runs `scripts/check-abi-symbols.go` and three test passes
against the supplied library: the default build, an external build
(`-tags lance_external_lib`) with `CGO_ENABLED=0`, and an external build with
the race detector. A skipped native test fails the job.

After the three test runs, the job copies the accepted install-cache library
into `$RUNNER_TEMP/production/<slug>/library`, outside the checkout and cache,
and runs `scripts/strip-native-library.sh` on that copy. The helper records the raw
and stripped sizes and SHA-256s, the strip tool path, hash, version, and argv,
and fails if the source library's bytes or hash change. Linux uses
`strip --strip-unneeded`; macOS uses `strip -x`. It does not rewrite
dependencies, install names, or RPATH. `scripts/check-native-library.sh` then
revalidates the stripped copy against the matching header, including the
architecture, dependency contract, deployment floor, and every bound export.

The job runs `examples/roundtrip` with `CGO_ENABLED=0`,
`-tags lance_external_lib`, and `LANCE_LIB_PATH` set explicitly to the stripped
copy. The external tag excludes the generated selector and bundle path, and
the explicit path prevents system discovery from being used. The step requires
the exact three-row output, not just exit status. The raw library still receives
the default, external, and race test passes above.

The job then passes the stripped copy to `scripts/sync-lance-c.sh` with the
production module path `github.com/lance-format/lance-go/artifacts/<goos>-<goarch>`.
The generated selector keeps the
`!lance_external_lib && <goos> && <goarch>` constraint and imports that
production path, but the workflow does not copy it into `internal/bundled`,
change `go.mod`/`go.sum`, or write into the checkout.
`scripts/verify-lance-c-artifact` checks the compressed payload hash and size,
decompresses it, compares it byte for byte with the stripped copy, and checks
the uncompressed hash, size, pin, module path, exact embed target, and file
allowlist. Darwin export inspection must produce a symbol table; unavailable
export evidence fails verification.

The candidate module and evidence are uploaded as
`unix-native-candidate-<slug>` and `unix-native-evidence-<slug>` with seven-day
retention. These workflow candidates remain unpublished. The tracked companion
modules were published after review and their selectors are active; their
exact-tag cold public-consumer acceptance is recorded above. The workflow
publishes the validated install cache only after the
stripped candidate is staged and verified and all consumers of the cache
staging path finish; the cache continues to hold the unstripped
accepted library, and candidates are never cached. The workflow asserts that
the checkout is unchanged, so it cannot activate a selector.

A successful run establishes native build, test, strip, and candidate
verification results for that commit only. Public proxy and sumdb
verification, cold default/external/namespace consumers of a published module,
selector activation, release/tag creation, and acceptance of any newly built
candidate remain separate release responsibilities. Existing published Unix
module acceptance does not automatically accept different workflow candidates.
The tested macOS runtime versions do not establish an older deployment floor
or completion of the platform roadmap.
