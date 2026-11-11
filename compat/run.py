#!/usr/bin/env python3
"""Serial cross-language Lance compatibility runner.

This runner is intentionally small and opt-in. It does not install tools or
change the Go module. Each producer writes its own Lance dataset, and every
consumer reads that dataset through its own Lance SDK. Every cell is captured
in a separate log and compared against the committed canonical reports.

Run this on the host that has all four toolchains installed. The Adam host is
only for static inspection; do not use this runner there.
"""

from __future__ import annotations

import argparse
import datetime as dt
import difflib
import os
import shlex
import shutil
import signal
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
COMPAT = ROOT / "compat"
EXPECTED = COMPAT / "expected"

LANGUAGES = ("go", "python", "java", "rust")
FOREIGN = ("python", "java", "rust")
DEFAULT_COMMANDS = {
    "go": "go run ./compat/go",
    "python": "python3 compat/python/compat.py",
    "java": "java --add-opens=java.base/java.nio=ALL-UNNAMED -jar compat/java/target/lance-compat-java.jar",
    "rust": "cargo run --quiet --manifest-path compat/rust/Cargo.toml --",
}
STORAGE_VERSION = "2.1"


@dataclass
class Language:
    name: str
    argv: list[str]
    available: bool
    reason: str


@dataclass
class Cell:
    name: str
    language: str
    status: str
    detail: str
    log_path: Path


def run_process(argv: list[str], cwd: Path, timeout: int) -> subprocess.CompletedProcess[str]:
    # Wrappers such as go run and cargo run spawn the actual SDK process.
    # Kill their entire session group before moving on to another cell.
    with subprocess.Popen(
        argv, cwd=cwd, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        text=True, start_new_session=True,
    ) as proc:
        try:
            stdout, stderr = proc.communicate(timeout=timeout)
        except subprocess.TimeoutExpired:
            try:
                os.killpg(proc.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            stdout, stderr = proc.communicate()
            return subprocess.CompletedProcess(
                argv, 124, stdout, stderr + f"\ntimed out after {timeout}s\n",
            )
        except BaseException:
            try:
                os.killpg(proc.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            proc.communicate()
            raise
        return subprocess.CompletedProcess(argv, proc.returncode, stdout, stderr)


def write_log(path: Path, header: str, proc: subprocess.CompletedProcess[str] | None, detail: str = "") -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    lines = [header, ""]
    if detail:
        lines.extend(["detail:", detail, ""])
    if proc is not None:
        lines.extend(
            [
                f"exit={proc.returncode}",
                "--- stdout ---",
                proc.stdout,
                "--- stderr ---",
                proc.stderr,
            ]
        )
    path.write_text("\n".join(lines))


def command_exists(argv: list[str]) -> tuple[bool, str]:
    if not argv:
        return False, "empty command"
    executable = argv[0]
    if os.sep in executable:
        path = Path(executable)
        if not path.is_absolute():
            path = ROOT / path
        if not path.exists():
            return False, f"missing executable {path}"
        return True, ""
    found = shutil.which(executable)
    if found is None:
        return False, f"{executable!r} not found in PATH"
    return True, ""


def java_jar_from_argv(argv: list[str]) -> Path | None:
    if "-jar" not in argv:
        return None
    index = argv.index("-jar")
    if index + 1 >= len(argv):
        return None
    path = Path(argv[index + 1])
    if not path.is_absolute():
        path = ROOT / path
    return path


def preflight(commands: dict[str, list[str]], timeout: int) -> dict[str, Language]:
    languages: dict[str, Language] = {}
    for name, argv in commands.items():
        ok, reason = command_exists(argv)
        if not ok:
            languages[name] = Language(name, argv, False, reason)
            continue
        if name == "java":
            jar = java_jar_from_argv(argv)
            if jar is not None and not jar.exists():
                languages[name] = Language(name, argv, False, f"missing Java jar {jar}; build compat/java first")
                continue
        if name == "python":
            probe = run_process(
                [
                    argv[0],
                    "-c",
                    "import lance, pyarrow; "
                    "assert lance.__version__ == '14.0.2', lance.__version__; "
                    "assert pyarrow.__version__ == '22.0.0', pyarrow.__version__; "
                    "print(lance.__version__, pyarrow.__version__)",
                ],
                ROOT,
                timeout,
            )
            if probe.returncode != 0:
                detail = (probe.stderr or probe.stdout).strip()
                languages[name] = Language(name, argv, False, f"Python dependencies are not installed: {detail}")
                continue
        languages[name] = Language(name, argv, True, "")
    return languages


def record_versions(run_dir: Path, languages: dict[str, Language], timeout: int) -> None:
    commands: dict[str, list[list[str]]] = {
        "go": [["go", "version"]],
        "python": [[languages["python"].argv[0], "--version"]],
        "java": [["java", "-version"]],
        "rust": [["rustc", "--version"], ["cargo", "--version"]],
    }
    lines = [
        f"started={dt.datetime.now().astimezone().isoformat()}",
        f"repo_root={ROOT}",
        f"storage_version={STORAGE_VERSION}",
    ]
    git_head = run_process(["git", "rev-parse", "HEAD"], ROOT, timeout)
    git_status = run_process(["git", "status", "--short"], ROOT, timeout)
    lines.append(f"git_head={git_head.stdout.strip()}")
    lines.append("git_status_begin")
    lines.extend(git_status.stdout.splitlines())
    lines.append("git_status_end")
    for name in LANGUAGES:
        language = languages[name]
        lines.append(f"language={name}")
        lines.append(f"command={shlex.join(language.argv)}")
        lines.append(f"available={language.available}")
        if language.reason:
            lines.append(f"reason={language.reason}")
        for version_cmd in commands[name]:
            try:
                proc = run_process(version_cmd, ROOT, timeout)
            except OSError as exc:
                lines.append(f"version_command={shlex.join(version_cmd)} error={exc}")
                continue
            lines.append(f"version_command={shlex.join(version_cmd)} exit={proc.returncode}")
            lines.extend((proc.stdout + proc.stderr).strip().splitlines())
    if languages["python"].available:
        probe = run_process(
            [
                languages["python"].argv[0],
                "-c",
                "import lance, pyarrow; print('lance=' + lance.__version__); print('pyarrow=' + pyarrow.__version__)",
            ],
            ROOT,
            timeout,
        )
        lines.append(f"python_packages_exit={probe.returncode}")
        lines.extend((probe.stdout + probe.stderr).strip().splitlines())
    (run_dir / "metadata.txt").write_text("\n".join(lines) + "\n")


def cell(
    name: str,
    language: Language,
    args: list[str],
    expected: str,
    run_dir: Path,
    timeout: int,
    blocked: str | None = None,
) -> Cell:
    log_path = run_dir / "logs" / (name.replace("/", "__") + ".log")
    header = f"cell={name}\nlanguage={language.name}\ncommand={shlex.join(language.argv + args)}"
    reason = blocked or (None if language.available else language.reason)
    if reason is not None:
        write_log(log_path, header, None, reason)
        print(f"[NOT-RUN] {name}: {reason}")
        return Cell(name, language.name, "not-run", reason, log_path)
    try:
        proc = run_process(language.argv + args, ROOT, timeout)
    except OSError as exc:
        detail = f"could not start command: {exc}"
        write_log(log_path, header, None, detail)
        print(f"[FAIL] {name}: {detail}")
        return Cell(name, language.name, "failed", detail, log_path)

    if proc.returncode != 0:
        detail = f"exit {proc.returncode}"
    elif proc.stdout != expected:
        diff = "".join(
            difflib.unified_diff(
                expected.splitlines(keepends=True),
                proc.stdout.splitlines(keepends=True),
                fromfile="expected",
                tofile="actual",
            )
        )
        detail = "canonical report mismatch\n" + diff
    else:
        detail = ""
    write_log(log_path, header, proc, detail)
    if proc.returncode != 0:
        print(f"[FAIL] {name}: exit {proc.returncode}")
        return Cell(name, language.name, "failed", detail, log_path)
    if proc.stdout != expected:
        print(f"[FAIL] {name}: canonical report mismatch")
        return Cell(name, language.name, "failed", detail, log_path)
    print(f"[PASS] {name}")
    return Cell(name, language.name, "passed", "", log_path)


def produce(language: Language, path: Path, run_dir: Path, timeout: int) -> Cell:
    return cell(
        f"base/{language.name}/produce",
        language,
        ["produce", "--dataset", str(path), "--storage-version", STORAGE_VERSION],
        (EXPECTED / "base.report").read_text(),
        run_dir,
        timeout,
    )


def read(language: Language, path: Path, version: int, expected_name: str, run_dir: Path, timeout: int, name: str) -> Cell:
    return cell(
        name,
        language,
        ["read", "--dataset", str(path), "--version", str(version), "--storage-version", STORAGE_VERSION],
        (EXPECTED / expected_name).read_text(),
        run_dir,
        timeout,
    )


def append(language: Language, path: Path, run_dir: Path, timeout: int, name: str) -> Cell:
    return cell(
        name,
        language,
        ["append", "--dataset", str(path), "--storage-version", STORAGE_VERSION],
        (EXPECTED / "append.report").read_text(),
        run_dir,
        timeout,
    )


def run_matrix(languages: dict[str, Language], run_dir: Path, timeout: int) -> list[Cell]:
    results: list[Cell] = []
    base_dir = run_dir / "datasets" / "base"
    base_dir.mkdir(parents=True, exist_ok=True)
    for producer_name in LANGUAGES:
        producer = languages[producer_name]
        path = base_dir / f"{producer_name}.lance"
        result = produce(producer, path, run_dir, timeout)
        results.append(result)
        blocked = None
        if result.status != "passed":
            blocked = f"producer {producer_name} did not create a dataset ({result.detail or result.status})"
        for consumer_name in LANGUAGES:
            consumer = languages[consumer_name]
            results.append(
                cell(
                    f"base/{producer_name}/read-{consumer_name}",
                    consumer,
                    ["read", "--dataset", str(path), "--version", "1", "--storage-version", STORAGE_VERSION],
                    (EXPECTED / "base.report").read_text(),
                    run_dir,
                    timeout,
                    blocked=blocked,
                )
            )
    return results


def run_append_pair(
    producer: Language,
    appender: Language,
    run_dir: Path,
    timeout: int,
) -> list[Cell]:
    name = f"{producer.name}-to-{appender.name}"
    path = run_dir / "datasets" / "append" / name / "dataset.lance"
    path.parent.mkdir(parents=True, exist_ok=True)
    results: list[Cell] = []
    produced = cell(
        f"append/{name}/{producer.name}-produce",
        producer,
        ["produce", "--dataset", str(path), "--storage-version", STORAGE_VERSION],
        (EXPECTED / "base.report").read_text(),
        run_dir,
        timeout,
    )
    results.append(produced)
    if produced.status != "passed":
        reason = f"producer {producer.name} did not create a dataset ({produced.detail or produced.status})"
        for cell_name, language in (
            (f"append/{name}/{appender.name}-append", appender),
            (f"append/{name}/{producer.name}-read-v1", producer),
            (f"append/{name}/{appender.name}-read-v1", appender),
            (f"append/{name}/{producer.name}-read-v2", producer),
            (f"append/{name}/{appender.name}-read-v2", appender),
        ):
            results.append(
                cell(cell_name, language, [], "", run_dir, timeout, blocked=reason)
            )
        return results
    results.append(append(appender, path, run_dir, timeout, f"append/{name}/{appender.name}-append"))
    results.append(
        read(
            producer,
            path,
            1,
            "base.report",
            run_dir,
            timeout,
            f"append/{name}/{producer.name}-read-v1",
        )
    )
    results.append(
        read(
            appender,
            path,
            1,
            "base.report",
            run_dir,
            timeout,
            f"append/{name}/{appender.name}-read-v1",
        )
    )
    results.append(
        read(
            producer,
            path,
            2,
            "latest.report",
            run_dir,
            timeout,
            f"append/{name}/{producer.name}-read-v2",
        )
    )
    results.append(
        read(
            appender,
            path,
            2,
            "latest.report",
            run_dir,
            timeout,
            f"append/{name}/{appender.name}-read-v2",
        )
    )
    return results


def run_append_checks(languages: dict[str, Language], run_dir: Path, timeout: int) -> list[Cell]:
    results: list[Cell] = []
    pairs = [("go", name) for name in FOREIGN] + [(name, "go") for name in FOREIGN]
    for producer_name, appender_name in pairs:
        producer = languages[producer_name]
        appender = languages[appender_name]
        pair_name = f"{producer_name}-to-{appender_name}"
        if not producer.available or not appender.available:
            unavailable = producer if not producer.available else appender
            reason = f"{unavailable.name} unavailable: {unavailable.reason}"
            for suffix, language in (
                (f"{producer_name}-produce", producer),
                (f"{appender_name}-append", appender),
                (f"{producer_name}-read-v1", producer),
                (f"{appender_name}-read-v1", appender),
                (f"{producer_name}-read-v2", producer),
                (f"{appender_name}-read-v2", appender),
            ):
                results.append(
                    cell(f"append/{pair_name}/{suffix}", language, [], "", run_dir, timeout, blocked=reason)
                )
            print(f"[NOT-RUN] append/{pair_name}: {unavailable.reason}")
            continue
        results.extend(run_append_pair(producer, appender, run_dir, timeout))
    return results


def main() -> int:
    global STORAGE_VERSION
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--run-dir", type=Path, required=True, help="new directory for reports, datasets, and logs")
    parser.add_argument("--storage-version", default="2.1", help="Lance data storage version (default: 2.1)")
    parser.add_argument("--timeout", type=int, default=900, help="per-cell timeout in seconds")
    parser.add_argument("--go-cmd", default=DEFAULT_COMMANDS["go"])
    parser.add_argument("--python-cmd", default=DEFAULT_COMMANDS["python"])
    parser.add_argument("--java-cmd", default=DEFAULT_COMMANDS["java"])
    parser.add_argument("--rust-cmd", default=DEFAULT_COMMANDS["rust"])
    args = parser.parse_args()
    if os.name != "posix":
        parser.error("the compatibility runner requires POSIX process groups")
    if args.timeout <= 0:
        parser.error("--timeout must be positive")

    STORAGE_VERSION = args.storage_version
    commands = {
        "go": shlex.split(args.go_cmd),
        "python": shlex.split(args.python_cmd),
        "java": shlex.split(args.java_cmd),
        "rust": shlex.split(args.rust_cmd),
    }
    for name in LANGUAGES:
        if not commands[name]:
            print(f"error: empty command for {name}", file=sys.stderr)
            return 2
    for expected in ("base.report", "latest.report", "append.report"):
        if not (EXPECTED / expected).exists():
            print(f"error: missing golden file {EXPECTED / expected}", file=sys.stderr)
            return 2
    run_dir = args.run_dir
    if run_dir.exists():
        print(f"error: run directory already exists: {run_dir}", file=sys.stderr)
        return 2
    (run_dir / "logs").mkdir(parents=True)

    languages = preflight(commands, args.timeout)
    record_versions(run_dir, languages, args.timeout)
    print(f"run_dir={run_dir}")
    print(f"storage_version={STORAGE_VERSION}")

    results = run_matrix(languages, run_dir, args.timeout)
    results.extend(run_append_checks(languages, run_dir, args.timeout))

    passed = sum(1 for result in results if result.status == "passed")
    failed = sum(1 for result in results if result.status == "failed")
    not_run = sum(1 for result in results if result.status == "not-run")
    executed = passed + failed
    summary = [
        "summary:",
        f"  cells={len(results)}",
        f"  executed={executed}",
        f"  passed={passed}",
        f"  failed={failed}",
        f"  not_run={not_run}",
    ]
    print("\n".join(summary))
    (run_dir / "summary.txt").write_text(
        "\n".join(
            summary
            + ["", "cells:"]
            + [f"{result.status}\t{result.name}\t{result.detail.splitlines()[0] if result.detail else ''}" for result in results]
        )
        + "\n"
    )
    return 0 if failed == 0 and not_run == 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())
