#!/usr/bin/env python3
"""Minimal S3 path-style mock for publisher tests."""

from __future__ import annotations

import argparse
import hashlib
import http.server
import json
import os
import pathlib
import sys
import urllib.parse


class Store:
    def __init__(self, root: pathlib.Path, log: pathlib.Path):
        self.root = root
        self.log = log
        self.root.mkdir(parents=True, exist_ok=True)

    def path(self, key: str) -> pathlib.Path:
        if ".." in key.split("/") or key.startswith("/"):
            raise ValueError(key)
        return self.root / key

    def etag(self, path: pathlib.Path) -> str:
        return '"' + hashlib.md5(path.read_bytes()).hexdigest() + '"'

    def write_log(self, line: str) -> None:
        with self.log.open("a", encoding="utf-8") as handle:
            handle.write(line + "\n")


STORE: Store


class Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def split(self):
        parsed = urllib.parse.urlsplit(self.path)
        parts = parsed.path.lstrip("/").split("/", 1)
        if len(parts) != 2:
            raise ValueError(self.path)
        return urllib.parse.unquote(parts[1])

    def send_empty(self, status, headers=None):
        self.send_response(status)
        for key, value in (headers or {}).items():
            self.send_header(key, value)
        self.send_header("Content-Length", "0")
        self.end_headers()

    def do_HEAD(self):
        try:
            key = self.split()
            path = STORE.path(key)
            if not path.is_file():
                STORE.write_log(f"HEAD {key} 404")
                self.send_empty(404)
                return
            STORE.write_log(f"HEAD {key} 200")
            self.send_response(200)
            self.send_header("Content-Length", str(path.stat().st_size))
            self.send_header("ETag", STORE.etag(path))
            self.send_header("Content-Type", "application/octet-stream")
            self.end_headers()
        except Exception as exc:  # noqa: BLE001
            self.send_empty(400, {"X-Error": str(exc)})

    def do_GET(self):
        try:
            key = self.split()
            path = STORE.path(key)
            if not path.is_file():
                STORE.write_log(f"GET {key} 404")
                self.send_empty(404)
                return
            body = path.read_bytes()
            STORE.write_log(f"GET {key} 200")
            self.send_response(200)
            self.send_header("Content-Length", str(len(body)))
            self.send_header("ETag", STORE.etag(path))
            self.send_header("Content-Type", "application/octet-stream")
            self.end_headers()
            self.wfile.write(body)
        except Exception as exc:  # noqa: BLE001
            self.send_empty(400, {"X-Error": str(exc)})

    def do_PUT(self):
        try:
            key = self.split()
            path = STORE.path(key)
            length = int(self.headers.get("Content-Length", "0"))
            body = self.rfile.read(length)
            existing = path.is_file()
            if self.headers.get("If-None-Match") == "*" and existing:
                STORE.write_log(f"PUT {key} 412 if-none-match")
                self.send_empty(412)
                return
            if_match = self.headers.get("If-Match")
            # R2 requires the condition value to be the quoted ETag exactly.
            if if_match and (not existing or STORE.etag(path) != if_match):
                STORE.write_log(f"PUT {key} 412 if-match")
                self.send_empty(412)
                return
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(body)
            STORE.write_log(f"PUT {key} 200 {len(body)} if-match={if_match or '-'}")
            digest = hashlib.md5(body).hexdigest()
            self.send_response(200)
            self.send_header("ETag", '"' + digest + '"')
            self.send_header("Content-Length", "0")
            self.end_headers()
        except Exception as exc:  # noqa: BLE001
            STORE.write_log(f"PUT error {exc}")
            self.send_empty(400, {"X-Error": str(exc)})

    def log_message(self, fmt, *args):  # noqa: ANN002
        return


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", required=True)
    parser.add_argument("--port", type=int, default=18789)
    parser.add_argument("--log", required=True)
    args = parser.parse_args()
    global STORE
    STORE = Store(pathlib.Path(args.root), pathlib.Path(args.log))
    server = http.server.ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
    print(server.server_address[1], flush=True)
    server.serve_forever()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
