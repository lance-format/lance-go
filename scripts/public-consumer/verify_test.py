#!/usr/bin/env python3
"""Synthetic boundary tests for the public-consumer harness.

These tests exercise the real command-line checks and driver assertions with
local fixtures. They do not build or run Go code, so they can run on a host that
is not Windows and do not imply that a Windows runtime path was executed.
"""

import contextlib
import hashlib
import io
import json
import os
import shutil
import struct
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import verify  # noqa: E402

RUN_SH = Path(__file__).resolve().parent / "run.sh"
OBSERVED_PSEUDO_VERSION = "v0.1.0-preview.1.0.20261113175157-976c6ddc6358"
OBSERVED_SOURCE_COMMIT = "976c6ddc6358c5058b2496ff982047093eceb788"


def run_verify(arguments):
    stdout = io.StringIO()
    stderr = io.StringIO()
    with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
        code = verify.main(arguments)
    return code, stdout.getvalue(), stderr.getvalue()


def read_report(path):
    return json.loads(Path(path).read_text(encoding="utf-8"))


def write_pe(path, machine, characteristics):
    data = bytearray(0x200)
    data[0:2] = b"MZ"
    struct.pack_into("<I", data, 0x3C, 0x80)
    data[0x80:0x84] = b"PE\0\0"
    struct.pack_into("<H", data, 0x84, machine)
    struct.pack_into("<H", data, 0x96, characteristics)
    struct.pack_into("<H", data, 0x98, 0x20B)
    Path(path).write_bytes(bytes(data))


def extract_function(text, name):
    lines = text.splitlines()
    marker = name + "() {"
    start = lines.index(marker)
    end = start + 1
    while end < len(lines) and lines[end] != "}":
        end += 1
    if end == len(lines):
        raise RuntimeError("unterminated function: " + name)
    return "\n".join(lines[start : end + 1])


def extract_run_sh_functions():
    text = RUN_SH.read_text(encoding="utf-8")
    names = ["fail", "record"]
    if "pseudo_version_prefix() {" in text:
        names.append("pseudo_version_prefix")
    names.append("assert_version_identity")
    return "\n\n".join(extract_function(text, name) for name in names)


def synthetic_tag_repository(root):
    repo = Path(root) / "tag-repo"
    repo.mkdir()
    subprocess.run(["git", "-C", str(repo), "init", "-q"], check=True)
    subprocess.run(
        ["git", "-C", str(repo), "config", "user.name", "synthetic"],
        check=True,
    )
    subprocess.run(
        ["git", "-C", str(repo), "config", "user.email", "synthetic@example.com"],
        check=True,
    )
    subprocess.run(
        [
            "git",
            "-C",
            str(repo),
            "-c",
            "commit.gpgSign=false",
            "commit",
            "-q",
            "--allow-empty",
            "-m",
            "synthetic",
        ],
        check=True,
    )
    source = subprocess.run(
        ["git", "-C", str(repo), "rev-parse", "HEAD"],
        check=True,
        text=True,
        stdout=subprocess.PIPE,
    ).stdout.strip()
    for tag in ("v1.2.3", "v1.2.3-deadbeefcafe"):
        subprocess.run(
            [
                "git",
                "-C",
                str(repo),
                "-c",
                "tag.gpgSign=false",
                "tag",
                tag,
            ],
            check=True,
        )
    return source, repo


@unittest.skipIf(
    os.name == "nt" or shutil.which("bash") is None,
    "requires bash",
)
class VersionIdentityTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.temporary.cleanup)
        cls.work = Path(cls.temporary.name)
        cls.tag_source, cls.tag_repo = synthetic_tag_repository(cls.work)
        cls.functions = extract_run_sh_functions()

    def invoke(self, name, revision_kind, root_version, root_revision, source):
        case_dir = self.work / name
        case_dir.mkdir()
        summary = case_dir / "summary.txt"
        script = case_dir / "case.sh"
        repo = (
            self.tag_repo
            if root_revision in ("v1.2.3", "v1.2.3-deadbeefcafe")
            else RUN_SH.parents[2]
        )
        script.write_text(
            "\n".join(
                [
                    "set -euo pipefail",
                    self.functions,
                    "",
                    "SUMMARY=%r" % str(summary),
                    "REPO_DIR=%r" % str(repo),
                    "REVISION_KIND=%r" % revision_kind,
                    "ROOT_REVISION=%r" % root_revision,
                    "ROOT_VERSION=%r" % root_version,
                    "SOURCE_COMMIT=%r" % source,
                    "assert_version_identity",
                    "printf 'VERSION_KIND=%s\\n' \"$VERSION_KIND\"",
                    "printf 'VERSION_COMMIT_PREFIX=%s\\n' \"$VERSION_COMMIT_PREFIX\"",
                ]
            )
            + "\n",
            encoding="utf-8",
        )
        return subprocess.run(
            ["bash", "--noprofile", "--norc", str(script)],
            cwd=case_dir,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )

    def fields(self, result):
        return dict(
            line.split("=", 1)
            for line in result.stdout.splitlines()
            if "=" in line
        )

    def test_supported_pseudo_version_forms_match_exact_source_prefix(self):
        versions = [
            "v0.0.0-20261113175157-976c6ddc6358",
            "v0.1.1-0.20261113175157-976c6ddc6358",
            "v2.0.1-0.20261113175157-976c6ddc6358+incompatible",
            OBSERVED_PSEUDO_VERSION,
            "v2.0.0-preview.1.0.20261113175157-976c6ddc6358+incompatible",
        ]
        for index, version in enumerate(versions):
            with self.subTest(version=version):
                result = self.invoke(
                    "pseudo-%d" % index,
                    "sha",
                    version,
                    OBSERVED_SOURCE_COMMIT,
                    OBSERVED_SOURCE_COMMIT,
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                fields = self.fields(result)
                self.assertEqual(fields["VERSION_KIND"], "pseudo")
                self.assertEqual(fields["VERSION_COMMIT_PREFIX"], "976c6ddc6358")

    def test_wrong_pseudo_version_prefix_fails(self):
        result = self.invoke(
            "wrong-prefix",
            "sha",
            "v0.1.0-preview.1.0.20261113175157-000000000000",
            OBSERVED_SOURCE_COMMIT,
            OBSERVED_SOURCE_COMMIT,
        )
        self.assertEqual(result.returncode, 1)
        self.assertIn(
            "does not match commit prefix 976c6ddc6358",
            result.stderr,
        )

    def test_real_tags_are_resolved_instead_of_misclassified_as_pseudo(self):
        cases = [
            ("sha", "v1.2.3-deadbeefcafe", self.tag_source),
            ("tag", "v1.2.3", self.tag_source),
        ]
        for index, (revision_kind, version, source) in enumerate(cases):
            with self.subTest(version=version):
                result = self.invoke(
                    "tag-%d" % index,
                    revision_kind,
                    version,
                    version,
                    source,
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                fields = self.fields(result)
                self.assertEqual(fields["VERSION_KIND"], "tag")
                self.assertEqual(fields["VERSION_COMMIT_PREFIX"], source)


class PeTests(unittest.TestCase):
    def test_amd64_dll_matches(self):
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / "lance_c.dll"
            out = Path(directory) / "report.json"
            write_pe(binary, 0x8664, 0x2000)
            code, _, _ = run_verify(
                [
                    "pe",
                    "--binary",
                    str(binary),
                    "--expect-machine",
                    "amd64",
                    "--expect-dll",
                    "--out",
                    str(out),
                ]
            )
            self.assertEqual(code, 0)
            report = read_report(out)
            self.assertEqual(report["machine"], "0x8664")
            self.assertTrue(report["expect_dll"])

    def test_wrong_machine_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / "lance_c.dll"
            out = Path(directory) / "report.json"
            write_pe(binary, 0x8664, 0x2000)
            code, _, _ = run_verify(
                [
                    "pe",
                    "--binary",
                    str(binary),
                    "--expect-machine",
                    "arm64",
                    "--out",
                    str(out),
                ]
            )
            self.assertEqual(code, 1)
            self.assertIn("PE machine", read_report(out)["errors"][0])

    def test_non_dll_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / "consumer.exe"
            out = Path(directory) / "report.json"
            write_pe(binary, 0x8664, 0x0002)
            code, _, _ = run_verify(
                [
                    "pe",
                    "--binary",
                    str(binary),
                    "--expect-machine",
                    "amd64",
                    "--expect-dll",
                    "--out",
                    str(out),
                ]
            )
            self.assertEqual(code, 1)
            self.assertIn("IMAGE_FILE_DLL", read_report(out)["errors"][0])

    def test_non_pe_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / "consumer.exe"
            out = Path(directory) / "report.json"
            binary.write_bytes(b"not a PE image")
            code, _, _ = run_verify(
                [
                    "pe",
                    "--binary",
                    str(binary),
                    "--expect-machine",
                    "amd64",
                    "--out",
                    str(out),
                ]
            )
            self.assertEqual(code, 1)
            self.assertIn("MZ", read_report(out)["errors"][0])


class CachePlatformTests(unittest.TestCase):
    def make_cache(self, root):
        data = b"synthetic cached library"
        sha = hashlib.sha256(data).hexdigest()
        base = Path(root) / "lance-go"
        entry = base / sha
        entry.mkdir(parents=True)
        (entry / ".lock").write_bytes(b"")
        (entry / "lance_c.dll").write_bytes(data)
        os.chmod(base, 0o755)
        os.chmod(entry, 0o755)
        os.chmod(entry / ".lock", 0o644)
        os.chmod(entry / "lance_c.dll", 0o644)
        return sha, len(data)

    def cache_args(self, root, out, platform, sha, size):
        return [
            "cache",
            "--root",
            str(root),
            "--expect",
            "present",
            "--platform",
            platform,
            "--sha",
            sha,
            "--library",
            "lance_c.dll",
            "--size",
            str(size),
            "--out",
            str(out),
        ]

    def test_windows_accepts_inherited_acl_modes_and_records_them(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "LocalAppData"
            out = Path(directory) / "report.json"
            sha, size = self.make_cache(root)
            code, _, _ = run_verify(
                self.cache_args(root, out, "windows", sha, size)
            )
            self.assertEqual(code, 0)
            report = read_report(out)
            self.assertEqual(report["platform"], "windows")
            self.assertFalse(report["base_mode_enforced"])
            self.assertFalse(report["library_mode_enforced"])
            self.assertEqual(report["actual_size"], size)

    @unittest.skipIf(os.name == "nt", "POSIX mode fixture is not meaningful on Windows")
    def test_unix_still_rejects_inherited_acl_modes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "home"
            out = Path(directory) / "report.json"
            sha, size = self.make_cache(root)
            code, _, _ = run_verify(self.cache_args(root, out, "unix", sha, size))
            self.assertEqual(code, 1)
            errors = read_report(out)["errors"]
            self.assertTrue(any("want 0700" in error for error in errors))
            self.assertTrue(any("want 0500" in error for error in errors))

    def test_windows_wrong_sha_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "LocalAppData"
            out = Path(directory) / "report.json"
            sha, size = self.make_cache(root)
            cached = Path(root) / "lance-go" / sha / "lance_c.dll"
            cached.write_bytes(b"X" * size)
            code, _, _ = run_verify(
                self.cache_args(root, out, "windows", sha, size)
            )
            self.assertEqual(code, 1)
            self.assertTrue(any("cached library sha256" in error for error in read_report(out)["errors"]))

    def test_windows_missing_library_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "LocalAppData"
            out = Path(directory) / "report.json"
            sha, size = self.make_cache(root)
            (Path(root) / "lance-go" / sha / "lance_c.dll").unlink()
            code, _, _ = run_verify(
                self.cache_args(root, out, "windows", sha, size)
            )
            self.assertEqual(code, 1)
            self.assertTrue(any("cached library is missing" in error for error in read_report(out)["errors"]))


if __name__ == "__main__":
    unittest.main()
