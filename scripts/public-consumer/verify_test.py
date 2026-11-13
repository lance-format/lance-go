#!/usr/bin/env python3
"""Synthetic boundary tests for verify.py.

These tests exercise the real command-line checks with local fixtures. They do
not build or run Go code, so they can run on a host that is not Windows and do
not imply that a Windows runtime path was executed.
"""

import contextlib
import hashlib
import io
import json
import os
import struct
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import verify  # noqa: E402


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
