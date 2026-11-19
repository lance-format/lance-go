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
github.com/lance-format/lance-go/artifacts/linux-amd64   v0.1.0
github.com/lance-format/lance-go/artifacts/linux-arm64   v0.1.0
github.com/lance-format/lance-go/artifacts/darwin-arm64  v0.1.0
```

The metadata snapshots are nested modules, so root builds resolve requirements
through the Go module proxy rather than using the local directories. Complete
sources and payloads remain at publication commits
`45a631d866d419d3046fdf3a70a801ebf8b7e87f` (Unix) and
`b0990720744a7ad9de644bf018f3a80496e205cc` (Windows), tagged
`artifacts/<target>/v0.1.0`. The preview.1 companion
versions remain available at their original publication commits, branches,
and release notes; the current tree carries only metadata snapshots.

The Intel macOS module is retired for new releases. Its metadata snapshot
remains at `artifacts/darwin-amd64/` and its published module and tags remain
at their original revisions, but the root module no longer requires it and no
current selector registers it.

### linux/amd64 payload

The published `linux/amd64` module is
`github.com/lance-format/lance-go/artifacts/linux-amd64` at
`v0.1.0`.

| Property | Value |
| --- | --- |
| Target | `linux/amd64` |
| Library | `liblance_c.so` |
| Compressed size | 87405187 bytes |
| Compressed SHA-256 | `8a6af22b6604a2525a04416ddd76441bd020a8cba0a2c3e50ad7fce003ce1b7f` |
| Uncompressed size | 342712936 bytes |
| Uncompressed SHA-256 | `4617c425e7e7660ef6d0d77f2e14dba48359400d78261b0072cd46e48e87e2ac` |
| Upstream release | `lance-c v0.2.0` |
| Upstream commit | `facb369c9fa98b7a3e5ce2ed12e2d8be090627ce` |
| Header SHA-256 | `947a912a14cb53f97237dbc7c03e5c09ef4337d46b459edc600d4d3c2c888d5c` |

### Published Unix companion payloads

The active companions use `UpstreamCommit`
`facb369c9fa98b7a3e5ce2ed12e2d8be090627ce` and `HeaderSHA256`
`947a912a14cb53f97237dbc7c03e5c09ef4337d46b459edc600d4d3c2c888d5c`. The
`darwin/amd64` module is historical and is not part of this table.

| Target | Compressed size | Compressed SHA-256 | Uncompressed size | Uncompressed SHA-256 |
| --- | --- | --- | --- | --- |
| `linux/arm64` | 83284195 | `e0565b2beacf5a77d06202e6b53c4d022d25a7bbd0694c2f40b4e284ab7938b1` | 307212464 | `24d3ab583b6ba60c82c49db946cc641309ca3eb3b519250eda23e2a1ff837146` |
| `darwin/arm64` | 77047524 | `586eb4b804b796e343b22f904bed3d7356dbe14f85e302f121373e43b248aa4c` | 288398992 | `818c0a912c13b756b84907edeaa0e51319cc123360a813d4cdd4ce1a85600463` |

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
`artifacts/windows-arm64` at `v0.1.0`.
Both public proxy archives and checksum-database entries match publication
commit `b0990720744a7ad9de644bf018f3a80496e205cc`. The four-file modules
contain `artifact.go`, `go.mod`, `LICENSE`, and `lance_c.dll.zst`.

| Target | Compressed size | Compressed SHA-256 | DLL size | DLL SHA-256 |
| --- | --- | --- | --- | --- |
| `windows/amd64` | 99776578 | `800876361b44256e2480217d57810a95d75663cd1e9a54a67b9936eb19790faf` | 402994176 | `8e825f7a0b7d0f5facfc1e62a379884236010e8d681955810eb48a643f430e1a` |
| `windows/arm64` | 89386379 | `faa3ac6549172cccd33f8637d32ba281e2f91b590e6493273cbe4f4bed8b6ed9` | 329314304 | `51e927bf7660862331884115c7205c04a44d85c961b4e03cddefcbbf285c7f12` |

Both use upstream `lance-c v0.2.0` at
`facb369c9fa98b7a3e5ce2ed12e2d8be090627ce`. Their generated selectors are
`internal/bundled/lance_bundle_windows_<arch>.go`, with build constraint
`!lance_external_lib && windows && <arch>`. `LANCE_LIB_PATH` retains
precedence over a registered bundle.

For the official `lance-c-v0.2.0` Windows assets, both
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
# 947a912a14cb53f97237dbc7c03e5c09ef4337d46b459edc600d4d3c2c888d5c
```

The raw archive header hashes are
`4db7674d59edba47e920ecf091c6135e077f060e8ec9b00c0b508a02d487338b` for both
`lance-c-v0.2.0-x86_64-pc-windows-msvc.tar.xz` and
`lance-c-v0.2.0-aarch64-pc-windows-msvc.tar.xz`. Packaging records the
canonical `HeaderSHA256`
`947a912a14cb53f97237dbc7c03e5c09ef4337d46b459edc600d4d3c2c888d5c`.

The published payloads decompress byte-for-byte to the DLLs in the official
`v0.2.0` Windows archives. The C release workflow verified the staged
PE images and public export set; the Go public-consumer lane verifies the
loaded DLL, cache identity, OS-only import closure and external supplied-DLL
path. Older Windows versions and redistributable CRT deployment remain
unclaimed.

## Platform and runtime scope

Three Unix targets have active bundles. The official `lance-c v0.2.0`
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
a different release archive. `internal/tools/fetch-lance-c-release` downloads,
verifies, and extracts the archive for one target.

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
`facb369c9fa98b7a3e5ce2ed12e2d8be090627ce`; their `artifact.go` records the
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
github.com/lance-format/lance-go/artifacts/darwin-arm64 v0.1.0 h1:mBBFnJw6IZm4jrz/IfNcyc7jN3Fc3pvXHKmQ10BK4mY=
github.com/lance-format/lance-go/artifacts/darwin-arm64 v0.1.0/go.mod h1:fQfHodK/kxZC5w9/Q3YPXfAFpacMYvc2TshI9luW6Xw=
github.com/lance-format/lance-go/artifacts/linux-amd64 v0.1.0 h1:+PqbXm9kZXdzJEfjhnhBiqOPXiIHy8g9qerusomD/Ck=
github.com/lance-format/lance-go/artifacts/linux-amd64 v0.1.0/go.mod h1:hmgfCi9X6+yByjEXjF26x8U4yNmD+NK5tFKAPQ2JXtI=
github.com/lance-format/lance-go/artifacts/linux-arm64 v0.1.0 h1:vC8sh+LjsOkqy5h6XIm52pkH48KcpcllytA7GZvNMew=
github.com/lance-format/lance-go/artifacts/linux-arm64 v0.1.0/go.mod h1:/PZX6MQ09uUxT+97jTAFTnOmBb8Eaya+Fo6/Cx8oR10=
github.com/lance-format/lance-go/artifacts/windows-amd64 v0.1.0 h1:WZryR01CNOaRcxhQsH6NRkeo55unY5mS/jLQa7y1vSk=
github.com/lance-format/lance-go/artifacts/windows-amd64 v0.1.0/go.mod h1:poapOGoG8Ov6b55dD9mnGnIvYAtPMZorCUDbhiWhodQ=
github.com/lance-format/lance-go/artifacts/windows-arm64 v0.1.0 h1:DgMfh+lnjWMk6r1dkAFAnyzpTv6a8L5NH4eFV+4G124=
github.com/lance-format/lance-go/artifacts/windows-arm64 v0.1.0/go.mod h1:0TJauP68uQ0eNmpLzDvdN9HDjzS1DzYQXcElTM/mqYE=
```

`lance_external_lib` builds compile out `bundle.go`, `cache.go`,
`lock_unix.go`, `lock_windows.go`, and the zstd dependency path. They still use
the platform loader, `golang.org/x/sys/windows`, and `LANCE_LIB_PATH` support
where the platform needs them.

## Verification status and remaining gates

The adopted native input is the annotated `lance-c v0.2.0` tag object
`84a7d564a961a6de6d68e6fef206e91d9363ab0a`, peeled to
`facb369c9fa98b7a3e5ce2ed12e2d8be090627ce`, with canonical header SHA-256
`947a912a14cb53f97237dbc7c03e5c09ef4337d46b459edc600d4d3c2c888d5c`. The
header declares three new exports — `lance_c_version`, `lance_dataset_uri` and
`lance_init_logging` — taking the source export set from 887 to 890. All 607 Go
bindings match it.

The five release archives, their `.sha256`/`.sha512` pairs and `SHA512SUMS`
were verified against the published assets. Each companion payload decompressed
byte-for-byte to the shared library in the matching official archive, and each
`v0.1.0` module resolved through `proxy.golang.org` and `sum.golang.org` with a
ZIP payload identical to the generated one. Compressed sizes and hashes in this
file come from `internal/tools/verify-lance-c-artifact` and the public module
ZIPs. Full per-target digests are recorded in the v0.1.0 release evidence
rather than duplicated here.

The preview.1 consumer evidence remains historical and records the older
six-target module set. The pre-publication candidate for v0.1.0 passed the
native lanes on all five targets: the
[Unix run 44603962255](https://github.com/lance-format/lance-go/actions/runs/44603962255)
covered linux/amd64, linux/arm64 and darwin/arm64, and the
[Windows run 44603966063](https://github.com/lance-format/lance-go/actions/runs/44603966063)
covered windows/amd64 and windows/arm64 (OS-only import closure, CRT guards,
411/427 top-level tests without skips, and the windows/amd64 tagged race
suite). The v0.1.0 exact-tag public-consumer runs and their checksummed
evidence archive are recorded in the
[v0.1.0 release](https://github.com/lance-format/lance-go/releases/tag/v0.1.0).

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
remain historical evidence for the older six-target module set.

Windows module publication, selector activation and hosted cold-cache loading
were verified on the published tag by the Windows run above. Broader
glibc/musl, older-macOS and older-Windows runtime coverage remain unaccepted.
The v0.1.0 post-tag runs satisfy this tag's public-consumer gate; future
companion tags still need their own post-tag acceptance.

## Native Unix validation workflow

`.github/workflows/unix-native.yml` tests the pinned lance-c release on native
runners for `linux/amd64`, `linux/arm64`, and `darwin/arm64`. The runners are
`ubuntu-24.04`, `ubuntu-24.04-arm`, and `macos-15`. The workflow runs on pull
requests and `main` pushes that touch native inputs, and on manual dispatch.

Each job downloads the archive named after `upstreamRelease` and the target
triple from the official `lance-c` release with
`internal/tools/fetch-lance-c-release`. The tool verifies the archive against
the per-asset `.sha256` published with the release, extracts only the header and
shared library, verifies the header against `upstreamHeaderSHA256`, and checks
the object format and machine for the target. The Windows archive ships the
same header with CRLF line endings; the tool writes the canonical LF form and
records both hashes. When `artifacts/<target>/artifact.go` records the same
upstream repository, commit, and header as the pin, the acquired library must
also equal that metadata's `UncompressedSHA256`, so a release archive and its
published checksum replaced together cannot silently change the bytes CI
tests. After a pin bump the existing snapshots describe the previous commit,
so the check is recorded as inactive until the new companion modules are
published and their metadata is adopted; candidate packaging still uses the
acquired bytes. A missing asset or a checksum or identity mismatch fails the
job; there is no source-build fallback. The acquisition record is uploaded
with the job evidence.

Each job then runs `scripts/check-native-library.sh` against the acquired
library and header. That check verifies the pinned header hash, object format
and architecture, direct dependencies, the libc or macOS deployment floor, and
that every symbol in the Go binding table is exported. It reports observed
export and dependency counts; it does not assert that the library exports only
`lance_` symbols.

The workflow then runs `internal/tools/check-abi-symbols` and three test passes
against the supplied library: the default build, an external build
(`-tags lance_external_lib`) with `CGO_ENABLED=0`, and an external build with
the race detector. A skipped native test fails the job.

After the three test runs, the job verifies the acquired library's SHA-256
against the acquisition record and uses those exact bytes as the production
packaging input. Published release libraries are already stripped, so no local
strip runs and the archive library is never modified.

The job runs `examples/roundtrip` with `CGO_ENABLED=0`,
`-tags lance_external_lib`, and `LANCE_LIB_PATH` set explicitly to the acquired
library. The external tag excludes the generated selector and bundle path, and
the explicit path prevents system discovery from being used. The step requires
the exact three-row output, not just exit status. The same library still
receives the default, external, and race test passes above.

The job then passes the acquired library to `scripts/sync-lance-c.sh` with the
production module path `github.com/lance-format/lance-go/artifacts/<goos>-<goarch>`.
The generated selector keeps the
`!lance_external_lib && <goos> && <goarch>` constraint and imports that
production path, but the workflow does not copy it into `internal/bundled`,
change `go.mod`/`go.sum`, or write into the checkout.
`internal/tools/verify-lance-c-artifact` checks the compressed payload hash and
size, decompresses it, compares it byte for byte with the acquired library, and
checks the uncompressed hash, size, pin, module path, exact embed target, and
file allowlist. Darwin export inspection must produce a symbol table;
unavailable export evidence fails verification.

The candidate module and evidence are uploaded as
`unix-native-candidate-<slug>` and `unix-native-evidence-<slug>` with seven-day
retention. These workflow candidates remain unpublished. The tracked companion
modules were published after review and their selectors are active; their
exact-tag cold public-consumer acceptance is recorded above. The workflow
asserts that the checkout is unchanged, so it cannot activate a selector.

A successful run establishes test and candidate verification results for the
published release bytes at that pin only. Public proxy and sumdb verification,
cold default/external/namespace consumers of a published module, selector
activation, release/tag creation, and acceptance of any newly built candidate
remain separate release responsibilities. Existing published Unix module
acceptance does not automatically accept different workflow candidates. The
tested macOS runtime versions do not establish an older deployment floor or
completion of the platform roadmap.

## Native Windows validation workflow

`.github/workflows/windows-native.yml` acquires the official release archive
for `windows/amd64` and `windows/arm64` and verifies the PE identity, header and
export agreement, the actual dependency closure, a native C consumer, the full
Go SDK suite, and a external-tag roundtrip against the exact staged DLL.
The external-tag suite records `windows/arm64` race coverage as unsupported rather
than skipped. After the dependency closure is OS-only, the workflow packages an
unpublished companion candidate from the unmodified DLL; it does not publish a
module or activate a selector. A missing or mismatched release asset fails the
job.
