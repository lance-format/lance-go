#!/usr/bin/env python3
"""Fixture tests for verify.py binary module identity checks.

The fixtures follow the shapes `go version -m` records for this lane. Building
a package inside the public root module (the default and external consumers)
records the root module as `mod` and has no `dep` record for it. Building a
standalone consumer module (the namespace and Windows consumers) records the
consumer as `mod` and the root module as `dep`.

These tests only read and write small local files. They do not build, execute,
or inspect a real Go binary, so passing them is not runtime acceptance.
"""

import contextlib
import io
import json
import sys
import tempfile
import types
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import verify  # noqa: E402

ROOT = "github.com/lance-format/lance-go"
ROOT_VERSION = "v0.0.0-20261112090442-99cf6d137940"
ROOT_SUM = "h1:TaAzPvfhCFuQ8PIn+BJ9Acn1o+7s+A0GGR3u3uHp8MI="
ARTIFACT = ROOT + "/artifacts/linux-amd64"
ARTIFACT_VERSION = "v0.0.0-20261111235843-ac5955a095d0"
ARTIFACT_SUM = "h1:tOJ/owlrb9wrZ1Yr18N3yXakU0+JCuSNutOu3sREkJ4="
ARROW = "github.com/apache/arrow-go/v18"
ARROW_VERSION = "v18.4.1"
ARROW_SUM = "h1:q/jVkBWCJOB9reDgaIZIdruLQUb1kbkvOnOFezVH1C4="
PAYLOAD = b"\x28\xb5\x2f\xfd\x20lance-payload-fixture"

BUILD_LINES = [
    "build\t-buildmode=exe",
    "build\t-compiler=gc",
    "build\tCGO_ENABLED=0",
    "build\tGOARCH=amd64",
    "build\tGOOS=linux",
]


def metadata_text(lines):
    return "fixture-binary: go1.23.0\n" + "".join(
        "\t%s\n" % line for line in lines
    )


def main_module_lines(package, module, version, module_sum):
    return [
        "path\t%s" % package,
        "mod\t%s\t%s\t%s" % (module, version, module_sum),
    ]


def root_main_lines():
    return (
        main_module_lines(
            ROOT + "/examples/roundtrip", ROOT, ROOT_VERSION, ROOT_SUM
        )
        + [
            "dep\t%s\t%s\t%s" % (ARROW, ARROW_VERSION, ARROW_SUM),
            "dep\t%s\t%s\t%s" % (ARTIFACT, ARTIFACT_VERSION, ARTIFACT_SUM),
        ]
        + BUILD_LINES
    )


def namespace_lines():
    return (
        main_module_lines(
            "example.com/public-consumer-namespace",
            "example.com/public-consumer-namespace",
            "(devel)",
            "",
        )
        + [
            "dep\t%s\t%s\t%s" % (ARROW, ARROW_VERSION, ARROW_SUM),
            "dep\t%s\t%s\t%s" % (ROOT, ROOT_VERSION, ROOT_SUM),
            "dep\t%s\t%s\t%s" % (ARTIFACT, ARTIFACT_VERSION, ARTIFACT_SUM),
        ]
        + BUILD_LINES
    )


def external_lines():
    return (
        main_module_lines(
            ROOT + "/examples/roundtrip", ROOT, ROOT_VERSION, ROOT_SUM
        )
        + [
            "dep\t%s\t%s\t%s" % (ARROW, ARROW_VERSION, ARROW_SUM),
            "build\t-tags=lance_external_lib",
        ]
        + BUILD_LINES
    )


def artifact_ref():
    return "%s@%s" % (ARTIFACT, ARTIFACT_VERSION)


def binary_arguments(**overrides):
    values = {
        "binary": None,
        "metadata": None,
        "payload": None,
        "out": None,
        "root_module": ROOT,
        "expect_root_version": ROOT_VERSION,
        "expect_root_sum": ROOT_SUM,
        "expect_artifact": artifact_ref(),
        "expect_sum": ARTIFACT_SUM,
        "expect_tag": "",
        "expect_payload": "present",
        "goos": "linux",
        "goarch": "amd64",
    }
    values.update(overrides)
    return types.SimpleNamespace(**values)


class BinaryMetadataTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.directory = Path(temporary.name)

    def write(self, name, content):
        path = self.directory / name
        if isinstance(content, bytes):
            path.write_bytes(content)
        else:
            path.write_text(content, encoding="utf-8")
        return path

    def check(self, lines, binary_payload=PAYLOAD, **overrides):
        metadata = self.write("version-m.txt", metadata_text(lines))
        binary = self.write(
            "consumer-bin", b"\x7fELF" + b"fixture" * 4 + binary_payload
        )
        payload = self.write("payload.zst", PAYLOAD)
        out = self.directory / "binary.json"
        arguments = binary_arguments(
            binary=str(binary),
            metadata=str(metadata),
            payload=str(payload),
            out=str(out),
            **overrides
        )
        stderr = io.StringIO()
        stdout = io.StringIO()
        with contextlib.redirect_stderr(stderr), contextlib.redirect_stdout(stdout):
            code = verify.command_binary(arguments)
        report = json.loads(out.read_text(encoding="utf-8"))
        return code, report, stderr.getvalue()

    def test_root_main_module_matches_public_identity(self):
        code, report, _ = self.check(root_main_lines())
        self.assertEqual(code, 0, report["errors"])
        self.assertEqual(report["package_path"], ROOT + "/examples/roundtrip")
        self.assertEqual(report["root_source"], "mod")
        self.assertTrue(report["root_present"])
        self.assertEqual(report["main_module"]["path"], ROOT)
        self.assertEqual(report["root_dependency"]["version"], ROOT_VERSION)
        self.assertEqual(report["root_dependency"]["sum"], ROOT_SUM)
        self.assertEqual(
            report["artifact_dependencies"],
            [
                {
                    "path": ARTIFACT,
                    "version": ARTIFACT_VERSION,
                    "sum": ARTIFACT_SUM,
                }
            ],
        )
        self.assertGreater(report["payload_offset"], 0)
        self.assertEqual(report["errors"], [])

    def test_root_dependency_shape_matches(self):
        code, report, _ = self.check(namespace_lines())
        self.assertEqual(code, 0, report["errors"])
        self.assertEqual(report["root_source"], "dep")
        self.assertEqual(
            report["main_module"]["path"], "example.com/public-consumer-namespace"
        )
        self.assertEqual(report["main_module"]["version"], "(devel)")
        self.assertEqual(report["main_module"]["sum"], "")
        self.assertEqual(report["root_dependency"]["path"], ROOT)
        self.assertEqual(report["root_dependency"]["sum"], ROOT_SUM)

    def test_wrong_root_version_fails(self):
        wrong = "v0.0.0-20260101000000-000000000000"
        lines = [
            "mod\t%s\t%s\t%s" % (ROOT, wrong, ROOT_SUM)
            if line.startswith("mod\t")
            else line
            for line in root_main_lines()
        ]
        code, report, _ = self.check(lines)
        self.assertEqual(code, 1)
        self.assertEqual(
            report["errors"],
            [
                "root dependency version is %s, want %s"
                % (wrong, ROOT_VERSION)
            ],
        )

    def test_missing_root_sum_fails(self):
        lines = [
            "mod\t%s\t%s\t" % (ROOT, ROOT_VERSION)
            if line.startswith("mod\t")
            else line
            for line in root_main_lines()
        ]
        code, report, _ = self.check(lines)
        self.assertEqual(code, 1)
        self.assertEqual(
            report["errors"],
            ["root dependency sum is , want %s" % ROOT_SUM],
        )

    def test_missing_root_module_fails(self):
        lines = [
            line
            for line in namespace_lines()
            if not line.startswith("dep\t%s\t" % ROOT)
        ]
        code, report, _ = self.check(lines)
        self.assertEqual(code, 1)
        self.assertEqual(
            report["errors"],
            ["root package/module is absent from binary metadata"],
        )
        self.assertEqual(report["root_source"], "none")

    def test_artifact_main_module_is_not_a_dependency(self):
        lines = (
            main_module_lines(
                ARTIFACT + "/cmd/probe",
                ARTIFACT,
                ARTIFACT_VERSION,
                ARTIFACT_SUM,
            )
            + ["dep\t%s\t%s\t%s" % (ROOT, ROOT_VERSION, ROOT_SUM)]
            + BUILD_LINES
        )
        code, report, _ = self.check(lines)
        self.assertEqual(code, 1)
        self.assertEqual(report["root_source"], "dep")
        self.assertEqual(report["main_module"]["path"], ARTIFACT)
        self.assertEqual(report["artifact_dependencies"], [])
        self.assertEqual(
            report["errors"],
            ["expected one artifact dependency, found 0"],
        )

    def test_unexpected_artifact_dependency_fails(self):
        code, report, _ = self.check(
            root_main_lines(), expect_artifact="none", expect_sum=""
        )
        self.assertEqual(code, 1)
        self.assertEqual(
            report["errors"],
            [
                "binary lists artifact dependency: %s@%s"
                % (ARTIFACT, ARTIFACT_VERSION)
            ],
        )

    def test_wrong_artifact_version_fails(self):
        wrong = "v0.0.0-20261111235843-000000000000"
        lines = [
            "dep\t%s\t%s\t%s" % (ARTIFACT, wrong, ARTIFACT_SUM)
            if line.startswith("dep\t%s\t" % ARTIFACT)
            else line
            for line in root_main_lines()
        ]
        code, report, _ = self.check(lines)
        self.assertEqual(code, 1)
        self.assertEqual(
            report["errors"],
            [
                "artifact dependency is %s@%s, want %s@%s"
                % (ARTIFACT, wrong, ARTIFACT, ARTIFACT_VERSION)
            ],
        )

    def test_replaced_root_module_fails(self):
        lines = [
            "mod\t%s\t%s" % (ROOT, ROOT_VERSION)
            if line.startswith("mod\t")
            else line
            for line in root_main_lines()
        ]
        fork = "github.com/example/lance-go-fork"
        replacement = "=>\t%s\tv0.0.1\t%s" % (fork, ARTIFACT_SUM)
        lines.insert(2, replacement)
        code, report, _ = self.check(lines, expect_root_sum="")
        self.assertEqual(code, 1)
        self.assertEqual(
            report["errors"],
            [
                "mod module %s is replaced by %s; replacements are not accepted"
                % (ROOT, fork)
            ],
        )

    def test_external_build_tag_without_artifact_matches(self):
        code, report, _ = self.check(
            external_lines(),
            binary_payload=b"build without the compressed payload",
            expect_artifact="none",
            expect_sum="",
            expect_payload="absent",
            expect_tag="lance_external_lib",
        )
        self.assertEqual(code, 0, report["errors"])
        self.assertEqual(report["root_source"], "mod")
        self.assertEqual(report["artifact_dependencies"], [])
        self.assertEqual(report["tags"], "lance_external_lib")
        self.assertEqual(report["payload_offset"], -1)
        self.assertEqual(report["errors"], [])


if __name__ == "__main__":
    unittest.main()
