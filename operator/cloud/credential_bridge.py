#!/usr/bin/env python3
"""Task-only credential_process over a private Unix socket forwarded by SSH.

The first request receives A. All later requests receive one stable B. Secrets
exist only in process memory and protocol stdout, never in audit output/files.
"""

import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import socket
import stat
import struct
import subprocess
import sys
import time


LIMIT = 65536


def validate(data):
    obj = json.loads(data)
    if obj.get("Version") != 1:
        raise ValueError("protocol version")
    fields = ("AccessKeyId", "SecretAccessKey", "SessionToken", "Expiration")
    if any(not isinstance(obj.get(key), str) or not obj[key] for key in fields):
        raise ValueError("temporary credentials required")
    expiration = datetime.datetime.fromisoformat(obj["Expiration"].replace("Z", "+00:00"))
    if expiration <= datetime.datetime.now(datetime.timezone.utc):
        raise ValueError("expired credentials")
    return {"Version": 1, **{key: obj[key] for key in fields}}


def private_parent(path):
    parent = path.parent
    info = parent.stat()
    if info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o700:
        raise ValueError("socket parent must be owned and mode 0700")


def audit(event, **values):
    print(json.dumps({"event": event, **values}), flush=True)


def issue(command):
    proc = subprocess.run(command, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                          stderr=subprocess.DEVNULL, timeout=40, check=True)
    if len(proc.stdout) > LIMIT:
        raise ValueError("response too large")
    return validate(proc.stdout)


def fingerprint(credentials):
    return hashlib.sha256(credentials["AccessKeyId"].encode()).hexdigest()


def serve(args):
    path = Path(args.socket).absolute()
    private_parent(path)
    command = json.loads(args.command_json)
    if not command or any(not isinstance(arg, str) for arg in command):
        raise ValueError("command must be an argv list")
    old_mask = os.umask(0o077)
    server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    bound = False
    try:
        server.bind(str(path))
        bound = True
        os.chmod(path, 0o600)
        server.listen(4)
        server.settimeout(1)
        expires = time.monotonic() + args.lifetime
        requests = 0
        seed_fingerprint = None
        stable = None
        audit("ready", socket=str(path))
        while time.monotonic() < expires:
            try:
                connection, _ = server.accept()
            except socket.timeout:
                continue
            with connection:
                connection.settimeout(45)
                _, uid, _ = struct.unpack("3i", connection.getsockopt(
                    socket.SOL_SOCKET, socket.SO_PEERCRED, struct.calcsize("3i")))
                if uid != os.getuid():
                    raise PermissionError("unexpected peer")
                if requests == 0:
                    credentials = issue(command)
                    seed_fingerprint = fingerprint(credentials)
                else:
                    if stable is None:
                        stable = issue(command)
                        if fingerprint(stable) == seed_fingerprint:
                            raise ValueError("rotation did not change credentials")
                    credentials = validate(json.dumps(stable))
                requests += 1
                connection.sendall(json.dumps(credentials).encode() + b"\n")
                audit("served", request=requests, stage="A" if requests == 1 else "B",
                      fingerprint=fingerprint(credentials), expires_at=credentials["Expiration"])
                if args.max_requests and requests >= args.max_requests:
                    break
        audit("stopped", requests=requests)
    finally:
        server.close()
        if bound:
            path.unlink(missing_ok=True)
        os.umask(old_mask)


def fetch(args):
    path = Path(args.socket).absolute()
    private_parent(path)
    info = path.stat()
    if not stat.S_ISSOCK(info.st_mode) or info.st_uid != os.getuid():
        raise ValueError("unexpected socket owner or type")
    if stat.S_IMODE(info.st_mode) & 0o077:
        raise ValueError("socket is accessible to other users")
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as client:
        client.settimeout(45)
        client.connect(str(path))
        data = bytearray()
        while True:
            part = client.recv(min(4096, LIMIT + 1 - len(data)))
            if not part:
                break
            data.extend(part)
            if len(data) > LIMIT:
                raise ValueError("response too large")
    # Only this subcommand emits secrets. Its stdout goes straight to the runner.
    sys.stdout.write(json.dumps(validate(data)) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="mode", required=True)
    server = sub.add_parser("serve")
    server.add_argument("--socket", required=True)
    server.add_argument("--command-json", required=True)
    server.add_argument("--lifetime", type=int, default=600)
    server.add_argument("--max-requests", type=int, default=0)
    client = sub.add_parser("fetch")
    client.add_argument("--socket", required=True)
    args = parser.parse_args()
    try:
        if args.mode == "serve":
            if not 1 <= args.lifetime <= 1800 or args.max_requests < 0:
                raise ValueError("invalid bounds")
            serve(args)
        else:
            fetch(args)
    except Exception as error:
        # Neither subprocess output nor exception text is safe to print.
        print("credential bridge failed: " + type(error).__name__, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
