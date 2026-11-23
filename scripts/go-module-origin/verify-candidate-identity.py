#!/usr/bin/env python3
"""Verify downloaded candidate files against the pinned identity manifest."""

from __future__ import annotations

import hashlib
import json
import pathlib
import sys


def main() -> int:
    if len(sys.argv) != 3:
        print("usage: verify-candidate-identity.py IDENTITY CANDIDATES", file=sys.stderr)
        return 2
    identity = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
    root = pathlib.Path(sys.argv[2])
    targets = identity.get("targets")
    if not isinstance(targets, dict) or not targets:
        print("identity has no targets", file=sys.stderr)
        return 1
    for target, expected in targets.items():
        module = root / target / "module"
        if not module.is_dir():
            print(f"missing candidate module: {module}", file=sys.stderr)
            return 1
        payloads = list(module.glob("*.zst"))
        if len(payloads) != 1:
            print(f"{target}: expected one payload, found {len(payloads)}", file=sys.stderr)
            return 1
        checks = [
            ("artifact.go", expected["candidate_artifact_go_sha256"]),
            ("go.mod", expected["candidate_go_mod_sha256"]),
            (payloads[0].name, expected["candidate_payload_sha256"]),
        ]
        for name, want in checks:
            path = module / name
            if not path.is_file():
                print(f"missing candidate file: {path}", file=sys.stderr)
                return 1
            got = hashlib.sha256(path.read_bytes()).hexdigest()
            if got != want:
                print(f"{target}: {name} sha256 {got}, want {want}", file=sys.stderr)
                return 1
    print("candidate identity: ok")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
