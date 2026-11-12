#!/usr/bin/env python3
"""Small, dependency-free evidence checks for the public-consumer lane.

The shell driver records commands and downloads. This helper only inspects
files, archives, binary metadata, and the loader cache. It deliberately does
not invoke Go, so a passing check cannot hide a failed download or a skipped
probe.
"""

import argparse
import hashlib
import json
import mmap
import os
import stat
import subprocess
import sys
import zipfile
from pathlib import Path


def sha256_file(path):
    digest = hashlib.sha256()
    with open(path, "rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def file_size(path):
    return os.stat(path).st_size


def write_json(path, payload):
    Path(path).write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n")


def report_errors(kind, errors):
    for error in errors:
        print("%s: %s" % (kind, error), file=sys.stderr)
    return 1


def git_tree(repo, commit):
    raw = subprocess.run(
        ["git", "-C", repo, "ls-tree", "-rz", "--full-tree", commit],
        check=True,
        stdout=subprocess.PIPE,
    ).stdout
    entries = {}
    for record in raw.split(b"\0"):
        if not record:
            continue
        metadata, path = record.split(b"\t", 1)
        fields = metadata.decode("ascii").split()
        if len(fields) != 3:
            raise RuntimeError("unexpected git ls-tree record: %r" % metadata)
        mode, object_type, object_id = fields
        if object_type != "blob":
            raise RuntimeError("archive comparison only supports blobs: %s" % path.decode())
        entries[path.decode()] = {
            "path": path.decode(),
            "mode": "%03o" % (int(mode, 8) & 0o777),
            "object": object_id,
            "sha256": None,
            "size": None,
        }

    if entries:
        request = "".join("%s\n" % item["object"] for item in entries.values()).encode()
        output = subprocess.run(
            ["git", "-C", repo, "cat-file", "--batch-check=%(objectname) %(objectsize)"],
            input=request,
            check=True,
            stdout=subprocess.PIPE,
        ).stdout.decode("ascii")
        sizes = {}
        for line in output.splitlines():
            object_id, size = line.split()
            sizes[object_id] = int(size)
        for item in entries.values():
            item["size"] = sizes[item["object"]]
            item["sha256"] = sha256_blob(repo, item["object"])
            del item["object"]
    return entries


def sha256_blob(repo, object_id):
    process = subprocess.Popen(
        ["git", "-C", repo, "cat-file", "blob", object_id],
        stdout=subprocess.PIPE,
    )
    digest = hashlib.sha256()
    for chunk in iter(lambda: process.stdout.read(1024 * 1024), b""):
        digest.update(chunk)
    if process.wait() != 0:
        raise RuntimeError("git cat-file failed for %s" % object_id)
    return digest.hexdigest()


def archive_entries(zip_path, module, version):
    prefix = "%s@%s/" % (module, version)
    entries = {}
    with zipfile.ZipFile(zip_path) as archive:
        names = archive.namelist()
        outside = [name for name in names if not name.startswith(prefix)]
        if outside:
            raise RuntimeError(
                "archive member outside %s: %s" % (prefix, outside[0])
            )
        for info in archive.infolist():
            if info.is_dir():
                continue
            if not info.filename.startswith(prefix):
                raise RuntimeError("archive member outside root: %s" % info.filename)
            relative = info.filename[len(prefix):]
            data = archive.read(info)
            mode = (info.external_attr >> 16) & 0o777
            mode_known = mode != 0
            if not mode_known:
                mode = 0o644
            entries[relative] = {
                "path": relative,
                "mode": "%03o" % mode,
                "mode_known": mode_known,
                "sha256": hashlib.sha256(data).hexdigest(),
                "size": len(data),
            }
    return entries


def excluded_module(path, nested_modules):
    for nested in nested_modules:
        if path == nested or path.startswith(nested + "/"):
            return True
    return False


def command_archive(args):
    errors = []
    differences = []
    try:
        tree = git_tree(args.repo, args.source_commit)
        archive = archive_entries(args.zip, args.module, args.version)
    except (RuntimeError, subprocess.CalledProcessError, zipfile.BadZipFile) as exc:
        errors.append(str(exc))
        report = {
            "kind": "archive",
            "module": args.module,
            "version": args.version,
            "zip": args.zip,
            "source_commit": args.source_commit,
            "errors": errors,
        }
        write_json(args.out, report)
        return report_errors("archive", errors)

    expected = {
        path: item
        for path, item in tree.items()
        if not excluded_module(path, args.nested_module)
    }
    missing = sorted(set(expected) - set(archive))
    extra = sorted(set(archive) - set(expected))
    for path in missing:
        differences.append("missing from archive: %s" % path)
    for path in extra:
        differences.append("unexpected in archive: %s" % path)
    for path in sorted(set(expected) & set(archive)):
        want = expected[path]
        got = archive[path]
        if want["sha256"] != got["sha256"]:
            differences.append("content mismatch: %s" % path)
        if want["size"] != got["size"]:
            differences.append(
                "size mismatch %s: git=%s archive=%s"
                % (path, want["size"], got["size"])
            )

    report = {
        "kind": "archive",
        "module": args.module,
        "version": args.version,
        "zip": args.zip,
        "source_commit": args.source_commit,
        "nested_modules_excluded": args.nested_module,
        "expected_files": len(expected),
        "archive_files": len(archive),
        "differences": differences,
        "errors": errors,
    }
    write_json(args.out, report)
    if differences:
        return report_errors("archive", differences)
    print(
        "archive matches git: module=%s version=%s files=%d"
        % (args.module, args.version, len(expected))
    )
    return 0


def parse_go_version_m(path):
    dependencies = []
    build = {}
    tags = None
    with open(path, "r", encoding="utf-8", errors="replace") as handle:
        for line in handle:
            fields = line.rstrip("\n").split("\t")
            if len(fields) >= 5 and fields[1] == "dep":
                dependencies.append(
                    {"path": fields[2], "version": fields[3], "sum": fields[4]}
                )
            elif len(fields) >= 3 and fields[1] == "build":
                item = fields[2]
                if item.startswith("-tags="):
                    tags = item[len("-tags="):]
                elif "=" in item:
                    key, value = item.split("=", 1)
                    build[key] = value
                else:
                    build[item] = ""
    return dependencies, build, tags


def command_binary(args):
    errors = []
    dependencies, build, tags = parse_go_version_m(args.metadata)
    root_dependency = next(
        (item for item in dependencies if item["path"] == args.root_module), None
    )
    artifact_dependencies = [
        item
        for item in dependencies
        if item["path"].startswith(args.root_module + "/artifacts/")
    ]
    root_present = root_dependency is not None

    if not root_present:
        errors.append("root package/module is absent from binary metadata")
    elif args.expect_root_version and root_dependency["version"] != args.expect_root_version:
        errors.append(
            "root dependency version is %s, want %s"
            % (root_dependency["version"], args.expect_root_version)
        )
    if root_present and args.expect_root_sum and root_dependency["sum"] != args.expect_root_sum:
        errors.append(
            "root dependency sum is %s, want %s"
            % (root_dependency["sum"], args.expect_root_sum)
        )

    if args.expect_artifact == "none":
        if artifact_dependencies:
            errors.append(
                "binary lists artifact dependency: %s"
                % ", ".join(
                    "%s@%s" % (item["path"], item["version"])
                    for item in artifact_dependencies
                )
            )
    else:
        want_path, want_version = args.expect_artifact.rsplit("@", 1)
        if len(artifact_dependencies) != 1:
            errors.append(
                "expected one artifact dependency, found %d"
                % len(artifact_dependencies)
            )
        else:
            actual = artifact_dependencies[0]
            if actual["path"] != want_path or actual["version"] != want_version:
                errors.append(
                    "artifact dependency is %s@%s, want %s@%s"
                    % (actual["path"], actual["version"], want_path, want_version)
                )
            if args.expect_sum and actual["sum"] != args.expect_sum:
                errors.append(
                    "artifact module sum is %s, want %s"
                    % (actual["sum"], args.expect_sum)
                )

    want_goos = args.goos
    want_goarch = args.goarch
    if build.get("GOOS") != want_goos:
        errors.append("binary GOOS is %r, want %r" % (build.get("GOOS"), want_goos))
    if build.get("GOARCH") != want_goarch:
        errors.append(
            "binary GOARCH is %r, want %r" % (build.get("GOARCH"), want_goarch)
        )
    if build.get("CGO_ENABLED") != "0":
        errors.append(
            "binary CGO_ENABLED is %r, want '0'" % build.get("CGO_ENABLED")
        )
    if args.expect_tag:
        actual_tags = []
        if tags:
            actual_tags = [item.strip() for item in tags.split(",")]
        if args.expect_tag not in actual_tags:
            errors.append(
                "binary build tags %r do not include %s"
                % (tags, args.expect_tag)
            )

    payload = Path(args.payload).read_bytes()
    if not payload:
        errors.append("payload file is empty")
    offset = -1
    if payload:
        with open(args.binary, "rb") as handle:
            with mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as mapped:
                offset = mapped.find(payload)
    payload_present = offset >= 0
    if args.expect_payload == "present" and not payload_present:
        errors.append("compressed payload is absent from binary")
    if args.expect_payload == "absent" and payload_present:
        errors.append("compressed payload is present in binary at offset %d" % offset)

    report = {
        "kind": "binary",
        "binary": args.binary,
        "metadata": args.metadata,
        "binary_size": file_size(args.binary),
        "root_module": args.root_module,
        "root_dependency": root_dependency,
        "root_present": root_present,
        "artifact_dependencies": artifact_dependencies,
        "build": build,
        "tags": tags,
        "payload_sha256": hashlib.sha256(payload).hexdigest(),
        "payload_size": len(payload),
        "payload_offset": offset,
        "errors": errors,
    }
    write_json(args.out, report)
    if errors:
        return report_errors("binary", errors)
    print(
        "binary metadata matches: artifact=%s payload=%s"
        % (args.expect_artifact, args.expect_payload)
    )
    return 0


def command_payload(args):
    errors = []
    member = "%s@%s/%s" % (args.module, args.version, args.name)
    with zipfile.ZipFile(args.zip) as archive:
        names = [info for info in archive.infolist() if info.filename == member]
        if len(names) != 1:
            errors.append("archive has %d exact members %s" % (len(names), member))
            data = b""
        else:
            data = archive.read(names[0])
    payload_size = len(data)
    payload_sha256 = hashlib.sha256(data).hexdigest()
    if args.expect_size is not None and payload_size != args.expect_size:
        errors.append(
            "payload size is %d, want %d" % (payload_size, args.expect_size)
        )
    if args.expect_sha256 and payload_sha256 != args.expect_sha256:
        errors.append(
            "payload sha256 is %s, want %s" % (payload_sha256, args.expect_sha256)
        )
    artifact_report = None
    if args.artifact_file:
        artifact_size = file_size(args.artifact_file)
        artifact_sha256 = sha256_file(args.artifact_file)
        artifact_report = {
            "path": args.artifact_file,
            "size": artifact_size,
            "sha256": artifact_sha256,
        }
        if artifact_size != payload_size:
            errors.append(
                "artifact file size is %d, payload is %d"
                % (artifact_size, payload_size)
            )
        if artifact_sha256 != payload_sha256:
            errors.append("artifact file bytes differ from archive payload")

    report = {
        "kind": "payload",
        "zip": args.zip,
        "zip_size": file_size(args.zip),
        "zip_sha256": sha256_file(args.zip),
        "module": args.module,
        "version": args.version,
        "member": args.name,
        "payload_size": payload_size,
        "payload_sha256": payload_sha256,
        "artifact_file": artifact_report,
        "errors": errors,
    }
    write_json(args.out, report)
    if errors:
        return report_errors("payload", errors)
    print(
        "payload matches: size=%d sha256=%s" % (payload_size, payload_sha256)
    )
    return 0


def mode_of(path, errors, label):
    info = os.lstat(path)
    if not stat.S_ISREG(info.st_mode) and not stat.S_ISDIR(info.st_mode):
        errors.append("%s is not a regular file or directory: %s" % (label, path))
    return stat.S_IMODE(info.st_mode)


def command_cache(args):
    errors = []
    base = Path(args.root) / "lance-go"
    if args.expect == "absent":
        if os.path.lexists(base):
            entries = []
            if os.path.isdir(base):
                entries = sorted(os.listdir(base))[:20]
            errors.append(
                "SDK cache already exists at %s (entries: %s)"
                % (base, ",".join(entries) if entries else "unreadable or non-directory")
            )
        report = {
            "kind": "cache",
            "expect": "absent",
            "base": str(base),
            "exists": os.path.lexists(base),
            "errors": errors,
        }
        write_json(args.out, report)
        if errors:
            return report_errors("cache", errors)
        print("cache is absent: %s" % base)
        return 0

    for argument in ("sha", "library", "size"):
        if getattr(args, argument) is None:
            errors.append("--%s is required for --expect present" % argument)
    if errors:
        write_json(args.out, {"kind": "cache", "base": str(base), "errors": errors})
        return report_errors("cache", errors)

    library_path = base / args.sha / args.library
    report = {
        "kind": "cache",
        "expect": "present",
        "base": str(base),
        "entry": str(base / args.sha),
        "library": str(library_path),
        "expected_sha256": args.sha,
        "expected_size": args.size,
        "errors": errors,
    }
    if not base.is_dir():
        errors.append("cache base is not a directory: %s" % base)
    else:
        base_mode = mode_of(base, errors, "cache base")
        if base_mode != 0o700:
            errors.append("cache base mode is %03o, want 0700" % base_mode)
        entries = sorted(os.listdir(base))
        if entries != [args.sha]:
            errors.append("cache entries are %r, want [%s]" % (entries, args.sha))
        entry = base / args.sha
        if not entry.is_dir():
            errors.append("cache entry is not a directory: %s" % entry)
        else:
            entry_mode = mode_of(entry, errors, "cache entry")
            if entry_mode != 0o700:
                errors.append("cache entry mode is %03o, want 0700" % entry_mode)
            names = sorted(os.listdir(entry))
            if names != [".lock", args.library]:
                errors.append(
                    "cache entry contents are %r, want ['.lock', %r]"
                    % (names, args.library)
                )
            lock = entry / ".lock"
            if not os.path.lexists(lock):
                errors.append("cache lock is missing: %s" % lock)
            else:
                lock_mode = mode_of(lock, errors, "cache lock")
                if lock_mode != 0o600:
                    errors.append("cache lock mode is %03o, want 0600" % lock_mode)
            if not library_path.is_file():
                errors.append("cached library is missing: %s" % library_path)
            else:
                library_mode = mode_of(library_path, errors, "cached library")
                actual_size = file_size(library_path)
                actual_sha256 = sha256_file(library_path)
                report["actual_size"] = actual_size
                report["actual_sha256"] = actual_sha256
                report["library_mode"] = "%03o" % library_mode
                if library_mode != 0o500:
                    errors.append(
                        "cached library mode is %03o, want 0500" % library_mode
                    )
                if actual_size != args.size:
                    errors.append(
                        "cached library size is %d, want %d"
                        % (actual_size, args.size)
                    )
                if actual_sha256 != args.sha:
                    errors.append(
                        "cached library sha256 is %s, want %s"
                        % (actual_sha256, args.sha)
                    )
    write_json(args.out, report)
    if errors:
        return report_errors("cache", errors)
    print("cache matches: %s" % library_path)
    return 0


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)

    archive = subparsers.add_parser("archive")
    archive.add_argument("--repo", required=True)
    archive.add_argument("--zip", required=True)
    archive.add_argument("--module", required=True)
    archive.add_argument("--version", required=True)
    archive.add_argument("--source-commit", required=True)
    archive.add_argument("--nested-module", action="append", default=[])
    archive.add_argument("--out", required=True)
    archive.set_defaults(func=command_archive)

    binary = subparsers.add_parser("binary")
    binary.add_argument("--binary", required=True)
    binary.add_argument("--metadata", required=True)
    binary.add_argument("--payload", required=True)
    binary.add_argument("--root-module", required=True)
    binary.add_argument("--expect-root-version", default="")
    binary.add_argument("--expect-root-sum", default="")
    binary.add_argument("--expect-artifact", required=True)
    binary.add_argument("--expect-sum", default="")
    binary.add_argument("--expect-payload", choices=("present", "absent"), required=True)
    binary.add_argument("--expect-tag", default="")
    binary.add_argument("--goos", required=True)
    binary.add_argument("--goarch", required=True)
    binary.add_argument("--out", required=True)
    binary.set_defaults(func=command_binary)

    payload = subparsers.add_parser("payload")
    payload.add_argument("--zip", required=True)
    payload.add_argument("--module", required=True)
    payload.add_argument("--version", required=True)
    payload.add_argument("--name", required=True)
    payload.add_argument("--artifact-file", default="")
    payload.add_argument("--expect-size", type=int)
    payload.add_argument("--expect-sha256", default="")
    payload.add_argument("--out", required=True)
    payload.set_defaults(func=command_payload)

    cache = subparsers.add_parser("cache")
    cache.add_argument("--root", required=True)
    cache.add_argument("--expect", choices=("present", "absent"), required=True)
    cache.add_argument("--sha", default=None)
    cache.add_argument("--library", default=None)
    cache.add_argument("--size", type=int, default=None)
    cache.add_argument("--out", required=True)
    cache.set_defaults(func=command_cache)

    args = parser.parse_args(argv)
    try:
        return args.func(args)
    except (OSError, RuntimeError, subprocess.CalledProcessError, zipfile.BadZipFile) as exc:
        print("verify.py: %s" % exc, file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
