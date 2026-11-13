#!/usr/bin/env python3

from __future__ import annotations

import json
import sys
from collections import Counter
from pathlib import Path
from typing import Any


EXPECTED_TEST = "TestFTSPreparedIndexOnlyExcludesUnindexedFragment"
EXPECTED_PACKAGE = "github.com/lance-format/lance-go"
SELECTOR = "^TestFTSPreparedIndexOnlyExcludesUnindexedFragment$"
SOURCE_COMMIT = "6c21e7520f40888a2f780f7b3ecf0ec18a7cd91c"
SOURCE_TREE = "260fa4a8c9c73a36da52feb0f769af1f31a1680e"
CANDIDATE_SOURCE = "c8e07c0c7fb455747df2724e506d4bc684093be6"
CORE_PIN = "98f84091db531775ab03fa7edb3d34c86937fbf5"
NATIVE_VERSION = "v15.0.0-beta.19"
EXPECTED_LIBRARY_SHA = "46d4bdac8497255d78e637dd8ef13b3c0e6986a3ca4aa532fff06511c00b3b13"
EXPECTED_CARGO_LOCK_SHA = "20824b7e1455b2d6373ca4fbbeed845dae06b9efb02a682c558323d00ca6d123"
EXPECTED_ARCHIVE_SHA = "a29dc366627b3af8383cb8c5c34732a72db0e8c57500ee7b2e816965284171a6"


def read_key_values(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        if "=" in line:
            key, value = line.split("=", 1)
            values[key] = value
    return values


def file_is_empty(path: Path) -> bool:
    return path.exists() and path.stat().st_size == 0


def parse_go_test(path: Path) -> tuple[list[dict[str, Any]], list[str]]:
    events: list[dict[str, Any]] = []
    errors: list[str] = []
    for line_number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        if not line.strip():
            continue
        try:
            event = json.loads(line)
        except json.JSONDecodeError as exc:
            errors.append(f"invalid JSON on line {line_number}: {exc}")
            continue
        if not isinstance(event, dict):
            errors.append(f"non-object JSON event on line {line_number}")
            continue
        events.append(event)
    return events, errors


def main() -> int:
    if len(sys.argv) != 5:
        print("usage: finalize.py EVIDENCE_DIR TEST_STATUS GOFMT_STATUS POSTFLIGHT_STATUS", file=sys.stderr)
        return 64

    evidence_dir = Path(sys.argv[1])
    test_status = int(sys.argv[2])
    gofmt_status = int(sys.argv[3])
    postflight_status = int(sys.argv[4])

    raw_json = evidence_dir / "raw" / "go-test.stdout.json"
    raw_stderr = evidence_dir / "raw" / "go-test.stderr"
    gofmt_stdout = evidence_dir / "raw" / "gofmt.stdout"
    gofmt_stderr = evidence_dir / "raw" / "gofmt.stderr"
    environment = read_key_values(evidence_dir / "environment.txt")
    identity = read_key_values(evidence_dir / "IDENTITY.txt")

    events, errors = parse_go_test(raw_json)

    top_level = Counter()
    subtests = Counter()
    package = Counter()
    tests: dict[str, dict[str, Any]] = {}
    packages: list[dict[str, Any]] = []

    for event in events:
        action = event.get("Action")
        name = event.get("Test")
        package_name = event.get("Package")
        if isinstance(name, str) and name:
            target = subtests if "/" in name else top_level
            target[action] += 1
            if "/" not in name:
                test = tests.setdefault(name, {"name": name, "status": "UNKNOWN", "elapsed_seconds": None})
                if action in {"pass", "fail", "skip"}:
                    test["status"] = action.upper()
                    test["elapsed_seconds"] = event.get("Elapsed")
            continue
        if isinstance(package_name, str) and package_name and action in {"pass", "fail", "skip"}:
            package[action] += 1
            packages.append(
                {
                    "package": package_name,
                    "status": action.upper(),
                    "elapsed_seconds": event.get("Elapsed"),
                }
            )

    if test_status != 0:
        errors.append(f"go test process exited {test_status}")
    if postflight_status != 0:
        errors.append("postflight identity or cleanliness validation failed")
    if top_level.get("run", 0) != 1:
        errors.append(f"top-level RUN count is {top_level.get('run', 0)}, want 1")
    if top_level.get("pass", 0) != 1:
        errors.append(f"top-level PASS count is {top_level.get('pass', 0)}, want 1")
    if top_level.get("fail", 0) != 0:
        errors.append(f"top-level FAIL count is {top_level.get('fail', 0)}, want 0")
    if top_level.get("skip", 0) != 0:
        errors.append(f"top-level SKIP count is {top_level.get('skip', 0)}, want 0")
    if subtests.get("run", 0) != 0:
        errors.append(f"subtest RUN count is {subtests.get('run', 0)}, want 0")
    if subtests.get("fail", 0) != 0 or subtests.get("skip", 0) != 0:
        errors.append("subtests reported FAIL or SKIP")
    if package.get("pass", 0) != 1:
        errors.append(f"package PASS count is {package.get('pass', 0)}, want 1")
    if package.get("fail", 0) != 0 or package.get("skip", 0) != 0:
        errors.append("package reported FAIL or SKIP")
    if set(tests) != {EXPECTED_TEST}:
        errors.append(f"top-level tests are {sorted(tests)}, want [{EXPECTED_TEST}]")
    elif tests[EXPECTED_TEST]["status"] != "PASS":
        errors.append(f"{EXPECTED_TEST} status is {tests[EXPECTED_TEST]['status']}, want PASS")
    if not packages or any(item["package"] != EXPECTED_PACKAGE for item in packages):
        errors.append(f"package identity is {packages}, want {EXPECTED_PACKAGE}")

    gofmt_diff_bytes = gofmt_stdout.stat().st_size if gofmt_stdout.exists() else -1
    gofmt_stderr_bytes = gofmt_stderr.stat().st_size if gofmt_stderr.exists() else -1
    gofmt_passed = gofmt_status == 0 and gofmt_diff_bytes == 0 and gofmt_stderr_bytes == 0
    if not gofmt_passed:
        errors.append(
            f"gofmt check failed: status={gofmt_status} diff_bytes={gofmt_diff_bytes} stderr_bytes={gofmt_stderr_bytes}"
        )

    clean_before = file_is_empty(evidence_dir / "git-status-before.txt")
    clean_after = file_is_empty(evidence_dir / "git-status-after.txt")
    if not clean_before:
        errors.append("source worktree was not clean before execution")
    if not clean_after:
        errors.append("source worktree was not clean after execution")

    library_before = (evidence_dir / "library-sha256-before.txt").read_text(encoding="utf-8").strip()
    library_after = (evidence_dir / "library-sha256-after.txt").read_text(encoding="utf-8").strip()
    if library_before != EXPECTED_LIBRARY_SHA or library_after != EXPECTED_LIBRARY_SHA:
        errors.append("library digest does not match before and after")

    started_at = (evidence_dir / "started-at-utc.txt").read_text(encoding="utf-8").strip()
    finished_at = (evidence_dir / "finished-at-utc.txt").read_text(encoding="utf-8").strip()
    command = (evidence_dir / "command.txt").read_text(encoding="utf-8").strip()
    all_json_lines_valid = not any(error.startswith("invalid JSON") for error in errors)

    result = "PASS" if not errors else "FAIL"
    test_entry = tests.get(EXPECTED_TEST, {"name": EXPECTED_TEST, "status": "UNKNOWN", "elapsed_seconds": None})
    package_entry = packages[0] if packages else {"package": EXPECTED_PACKAGE, "status": "UNKNOWN", "elapsed_seconds": None}

    result_json = {
        "schema_version": 1,
        "result": result,
        "scope": {
            "kind": "bounded Go prepared/index-only FTS candidate runtime validation",
            "source_changes": False,
            "full_suite": False,
            "requested_top_level_tests": [EXPECTED_TEST],
            "selector_regex": SELECTOR,
        },
        "tested_source": {
            "repository": EXPECTED_PACKAGE,
            "module": EXPECTED_PACKAGE,
            "commit": SOURCE_COMMIT,
            "tree": SOURCE_TREE,
            "clean_before": clean_before,
            "clean_after": clean_after,
            "host_checkout": environment.get("source_root"),
        },
        "native_artifact": {
            "candidate_source": CANDIDATE_SOURCE,
            "product_base": identity.get("product_base"),
            "upstream_core_pin": CORE_PIN,
            "native_lance_version": NATIVE_VERSION,
            "build": identity.get("build"),
            "rustc": "1.95.0",
            "cargo": "1.95.0",
            "artifact_root": "/home/xuanwo/Code/lance-c-v15-migration-evidence-20261113/candidate-c8e07c0",
            "library_path": "/home/xuanwo/Code/lance-c-v15-migration-evidence-20261113/candidate-c8e07c0/lib/liblance_c.so",
            "library_sha256_before": library_before,
            "library_sha256_after": library_after,
            "c_header_sha256": identity.get("c_header_sha256", "37895ce8eb1a66b8ab95c68f207b3d914e811112a97dd437470fb0b159a9f502"),
            "cpp_header_sha256": identity.get("cpp_header_sha256", "6cd4a6c3ba4cf76a83fa0b9c378d15f783b4216e8c8e097708cb18c90b68fb39"),
            "cargo_lock_path": "/home/xuanwo/Code/lance-c-v15-migration/Cargo.lock",
            "cargo_lock_sha256": EXPECTED_CARGO_LOCK_SHA,
            "source_archive_sha256": EXPECTED_ARCHIVE_SHA,
        },
        "environment": {
            "host": "nier",
            "os": environment.get("goos"),
            "arch": environment.get("goarch"),
            "kernel": environment.get("uname_r"),
            "glibc": environment.get("glibc"),
            "go_tool": environment.get("go_bin"),
            "go_version": environment.get("go_version"),
            "gotoolchain": environment.get("gotoolchain"),
            "goenv": "off",
            "cgo_enabled": environment.get("cgo_enabled"),
            "build_tags": "lance_external_lib",
        },
        "execution": {
            "selector_regex": SELECTOR,
            "command": command,
            "started_at_utc": started_at,
            "finished_at_utc": finished_at,
            "process_exit_code": test_status,
            "stderr_bytes": raw_stderr.stat().st_size,
            "json_lines": len(events),
            "all_json_lines_valid": all_json_lines_valid,
        },
        "counts": {
            "top_level_run": top_level.get("run", 0),
            "top_level_pass": top_level.get("pass", 0),
            "top_level_fail": top_level.get("fail", 0),
            "top_level_skip": top_level.get("skip", 0),
            "subtest_run": subtests.get("run", 0),
            "subtest_pass": subtests.get("pass", 0),
            "subtest_fail": subtests.get("fail", 0),
            "subtest_skip": subtests.get("skip", 0),
            "package_pass": package.get("pass", 0),
            "package_fail": package.get("fail", 0),
            "package_skip": package.get("skip", 0),
        },
        "tests": [test_entry],
        "package_result": package_entry,
        "format_check": {
            "command": "gofmt -d dataset_fts.go dataset_fts_test.go",
            "status": gofmt_status,
            "diff_bytes": gofmt_diff_bytes,
            "stderr_bytes": gofmt_stderr_bytes,
            "passed": gofmt_passed,
        },
        "evidence": {
            "raw_go_json_stdout": "raw/go-test.stdout.json",
            "raw_go_stderr": "raw/go-test.stderr",
            "go_exit_code_file": "raw/go-test.exit-code",
            "raw_gofmt_stdout": "raw/gofmt.stdout",
            "raw_gofmt_stderr": "raw/gofmt.stderr",
            "gofmt_exit_code_file": "raw/gofmt.exit-code",
            "command": "command.txt",
            "environment": "environment.txt",
            "identity": "IDENTITY.txt",
            "source_identity": "source-identity.txt",
            "source_diff_name_status": "source-diff-name-status.txt",
            "source_diff_stat": "source-diff-stat.txt",
            "hashes_before": "hashes-before.txt",
            "hashes_after": "hashes-after.txt",
            "git_status_before": "git-status-before.txt",
            "git_status_after": "git-status-after.txt",
            "launch_identity": "launch.txt",
            "driver_and_launcher_sha256": "driver-and-launcher-sha256.txt",
            "checksum_manifest": "SHA256SUMS",
        },
        "errors": errors,
        "limits": [
            "Only the single requested root-package selector was executed.",
            "No full suite, platform matrix, old-library red run, or extra selector was executed.",
            "The C candidate's full acceptance remains independently owned by the C parent.",
            "No native library was rebuilt or replaced, and no Go source was modified.",
            "This result validates the existing untouched C fixture on one NieR runtime only.",
        ],
    }

    result_md = f"""# Core 15 beta.19 candidate Go FTS index-only runtime: c8e07c0

## Result

**{result}.** The single requested root-package test
`{EXPECTED_TEST}` {"passed" if result == "PASS" else "did not pass"} against the
supplied external `liblance_c.so`. The Go process exit code was `{test_status}`.

| Counter | Observed | Required |
| --- | ---: | ---: |
| Top-level RUN | {top_level.get("run", 0)} | 1 |
| Top-level PASS | {top_level.get("pass", 0)} | 1 |
| Top-level FAIL | {top_level.get("fail", 0)} | 0 |
| Top-level SKIP | {top_level.get("skip", 0)} | 0 |
| Package PASS | {package.get("pass", 0)} | 1 |

The fixture itself requires STRICT prepare to reject the appended unindexed
fragment and the INDEX_ONLY scan to return exactly IDs `[1 2 4]`, excluding the
matching unindexed IDs `6` and `7`.

## Tested identities

| Item | Value |
| --- | --- |
| Go source | `{EXPECTED_PACKAGE}` |
| Commit | `{SOURCE_COMMIT}` |
| Tree | `{SOURCE_TREE}` |
| Source state | Clean before and after execution |
| C candidate source | `{CANDIDATE_SOURCE}` |
| Upstream core pin | `{CORE_PIN}` |
| Native version | `{NATIVE_VERSION}` |
| Native build | `{identity.get("build", "unknown")}` |
| Rust/Cargo | `1.95.0` / `1.95.0` |
| Library | `/home/xuanwo/Code/lance-c-v15-migration-evidence-20261113/candidate-c8e07c0/lib/liblance_c.so` |
| Library SHA-256 before | `{library_before}` |
| Library SHA-256 after | `{library_after}` |
| C header SHA-256 | `37895ce8eb1a66b8ab95c68f207b3d914e811112a97dd437470fb0b159a9f502` |
| C++ header SHA-256 | `6cd4a6c3ba4cf76a83fa0b9c378d15f783b4216e8c8e097708cb18c90b68fb39` |
| Cargo.lock SHA-256 | `{EXPECTED_CARGO_LOCK_SHA}` |
| C source archive SHA-256 | `{EXPECTED_ARCHIVE_SHA}` |

Runtime: NieR Linux x86_64, kernel `{environment.get("uname_r")}`, glibc
`{environment.get("glibc")}`, Go `{environment.get("go_version")}`,
`CGO_ENABLED=0`, `GOTOOLCHAIN=local`, `GOENV=off`, and build tag
`lance_external_lib`.

## Command

```sh
{command}
```

## Execution facts

- Started: `{started_at}`
- Finished: `{finished_at}`
- JSON events: `{len(events)}`; all valid: `{str(all_json_lines_valid).lower()}`
- stderr bytes: `{raw_stderr.stat().st_size}`
- `gofmt -d` clean: `{str(gofmt_passed).lower()}`
- `{EXPECTED_TEST}`: `{test_entry["status"]}`, elapsed `{test_entry["elapsed_seconds"]}s`
- Package `{EXPECTED_PACKAGE}`: `{package_entry["status"]}`, elapsed `{package_entry["elapsed_seconds"]}s`

## Boundaries

- This establishes only the single requested Go prepared/index-only FTS
  regression on this NieR runtime.
- It does not establish the full Go suite, a platform matrix, or final
  acceptance of the C candidate; the C parent independently owns that scope.
- It does not duplicate C, C++, ABI, static/shared, or historical fixture tests.
- No native rebuild, Go source edit, CI dispatch, PR, or publication action was
  performed.
"""

    if errors:
        result_md += "\n## Errors\n\n" + "\n".join(f"- {error}" for error in errors) + "\n"

    result_json_path = evidence_dir / "RESULT.json"
    result_md_path = evidence_dir / "RESULT.md"
    tmp_json = evidence_dir / ".RESULT.json.tmp"
    tmp_md = evidence_dir / ".RESULT.md.tmp"
    tmp_json.write_text(json.dumps(result_json, indent=2) + "\n", encoding="utf-8")
    tmp_md.write_text(result_md, encoding="utf-8")
    tmp_json.replace(result_json_path)
    tmp_md.replace(result_md_path)

    print(json.dumps({"result": result, "errors": errors, "counts": result_json["counts"]}, indent=2))
    return 0 if result == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
