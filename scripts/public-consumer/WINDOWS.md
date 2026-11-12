# Windows public consumers

`run-windows.ps1` is the Windows acceptance harness for the published
`artifacts/windows-<arch>` modules. It is deliberately separate from
`run.sh`: Windows has a different cache contract, needs a PE check, and can
report the module that the loader actually mapped.

## Prerequisite

The lane is preparative until the following exist on the public proxy at the
same root revision:

- the root module version;
- `artifacts/windows-amd64` or `artifacts/windows-arm64`, published from a
  static-CRT build and adopted by the root `go.mod` and platform selector;
- the matching `internal/bundled` selector for the target architecture.

A configured workflow is not acceptance. If the downloaded root `go.mod` does
not require the Windows artifact, or the built consumer has no Windows artifact
dependency, the lane stops with an explicit error. It does not skip, fake a
module, or turn a missing artifact into a passing run.

The static-CRT eligibility comes from the candidate artifact provenance. The
harness records every loaded module so a borrowed redistributable runtime is
visible. The OS components `msvcrt.dll`, `msvcp_win.dll`, and `ucrtbase.dll`
are not rejected; `vcruntime*`, `msvcp<digit>*`, `concrt*`, `mfc*`, `vcomp*`,
and `ucrtbased*` are rejected. This is an observation of the actual module
closure, not a new product security contract.

## Workflow

`.github/workflows/windows-public-consumers.yml` runs:

- `windows-2022` for native amd64;
- `windows-11-vs2026-arm` for native arm64.

It uses the exact PR head commit, or an explicit full commit SHA or tag passed
to manual dispatch. The revision is passed through environment variables, not
interpolated into a shell command. Go is configured with `CGO_ENABLED=0`,
`GOTOOLCHAIN=local`, `GOWORK=off`, and the public `proxy.golang.org` /
`sum.golang.org` endpoints. The workflow never runs `go mod tidy`, `go mod
verify`, a native build, Rust, or protoc. It disables Git line-ending
conversion before checkout so the tracked artifact bytes can be compared with
the published module bytes.

## Consumers

Each mode receives a fresh module cache, build cache, `LOCALAPPDATA`, and
temporary directory:

- **default**: the ordinary roundtrip example, with `LANCE_LIB_PATH` unset. The
  cache must be cold before the run and must contain the selected DLL after
  extraction. The literal oracle is `Created, reopened and scanned 3 rows: [10
  20 30]`.
- **namespace**: imports only `namespace`, calls `NewDirectory`, `ID`,
  `ListNamespaces`, and `Close`, and must not pull in the root Go package. The
  cache is cold before the run and contains the same selected artifact after
  extraction.
- **external**: builds with `lance_external_lib`. It may download the artifact
  module, but must not link, embed, or extract it. The supplied DLL is copied
  from the verified default cache and passed with `LANCE_LIB_PATH`.

The identity helper in `windows-consumer/` uses `x/sys/windows` to enumerate
loaded modules. For default and namespace runs, the loaded `lance_c.dll` must
be the verified cache file. For external runs it must be the explicitly
supplied copy. A DLL under the repository or build tree is a failure. The
driver also checks the PE machine of every built consumer and of the cached
DLL.

## Windows cache contract

`verify.py cache --platform windows` checks the cache structure, entry name,
lock presence, size, and SHA-256. It records the inherited LocalAppData ACL,
file attributes, read-only bit, and writability, but does not assert POSIX
`0700`, `0600`, or `0500` modes. The native loader intentionally leaves the
cached DLL writable and relies on the per-user LocalAppData ACL. Unix behavior
is unchanged and still enforces the POSIX modes.

The harness only moves its own isolated, freshly-created cache roots aside
after inspection. It never deletes a user cache.

## Evidence

Every run writes commands, environment and host identity, module-cache
inventory, artifact metadata, required artifact lines, `verify.py` JSON reports,
runtime logs, loaded-module inventory, and cache ACL facts. The workflow
uploads the evidence even when the lane fails. On failure the temporary work
directory is kept locally for further inspection; it is not uploaded.

## Local static checks

These checks do not need Go or a native build:

```powershell
python scripts/public-consumer/verify_test.py -v
pwsh -File scripts/public-consumer/windows-harness-test.ps1
```

The Python tests exercise PE and cache acceptance/rejection. The PowerShell
test parses the real driver, checks the command boundary, and invokes the real
`verify.py` PE and cache paths with synthetic files. Actual Windows runtime
acceptance only happens on the hosted Windows runners; local static checks
cannot substitute for it.
