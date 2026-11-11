#!/usr/bin/env python3
"""Adam-only STS issuer. Stdout is SECRET credential_process protocol data.

Load the existing local SSO helper in memory, assume the already-approved role
with the explicit task session policy, and return only the scoped credentials.
No SSO credentials or cache files leave Adam. No IAM resources are changed.
"""

import argparse
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", required=True)
    parser.add_argument("--policy", required=True)
    parser.add_argument("--role", required=True)
    parser.add_argument("--region", default="us-east-2")
    args = parser.parse_args()
    try:
        spec = importlib.util.spec_from_file_location("local_sso_source", args.source)
        source = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(source)
        initial = source.credentials()
        policy = json.dumps(json.loads(Path(args.policy).read_text()), separators=(",", ":"))
        if len(policy) > 2048:
            raise ValueError("session policy too large")
        env = os.environ.copy()
        env.update(AWS_ACCESS_KEY_ID=initial["AccessKeyId"],
                   AWS_SECRET_ACCESS_KEY=initial["SecretAccessKey"],
                   AWS_SESSION_TOKEN=initial["SessionToken"], AWS_PAGER="")
        response = subprocess.run([
            "aws2", "--region", args.region, "sts", "assume-role", "--role-arn", args.role,
            "--role-session-name", "lance-go-rotation-" + uuid.uuid4().hex[:12],
            "--duration-seconds", "900", "--policy", policy, "--output", "json",
        ], env=env, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL, timeout=30, check=True)
        credentials = json.loads(response.stdout)["Credentials"]
        result = {"Version": 1, **{key: credentials[key] for key in (
            "AccessKeyId", "SecretAccessKey", "SessionToken", "Expiration")}}
        sys.stdout.write(json.dumps(result) + "\n")
    except Exception as error:
        print("scoped credential process failed: " + type(error).__name__, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
