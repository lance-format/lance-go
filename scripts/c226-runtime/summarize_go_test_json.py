#!/usr/bin/env python3

import argparse
import collections
import json
import os
import sys


TERMINAL_ACTIONS = {"pass", "fail", "skip"}


def final_action(actions):
    for action in reversed(actions):
        if action in TERMINAL_ACTIONS:
            return action
    return "incomplete"


def parse_args():
    parser = argparse.ArgumentParser()
    parser.add_argument("--label", required=True)
    parser.add_argument("--json", required=True)
    parser.add_argument("--out", required=True)
    parser.add_argument("--expected", nargs="+", required=True)
    parser.add_argument("--require-log", action="append", default=[])
    return parser.parse_args()


def write_atomic(path, payload):
    temporary = f"{path}.tmp.{os.getpid()}"
    with open(temporary, "w", encoding="utf-8") as handle:
        handle.write(payload)
        handle.flush()
        os.fsync(handle.fileno())
    os.replace(temporary, path)


def main():
    args = parse_args()
    expected = list(dict.fromkeys(args.expected))
    events_by_test = collections.defaultdict(list)
    outputs_by_test = collections.defaultdict(list)
    events_by_package = collections.defaultdict(list)
    packages = set()
    raw_events = 0
    invalid_lines = []

    with open(args.json, encoding="utf-8") as handle:
        for line_number, line in enumerate(handle, start=1):
            stripped = line.strip()
            if not stripped:
                continue
            try:
                event = json.loads(stripped)
            except json.JSONDecodeError as error:
                invalid_lines.append(
                    f"line {line_number}: {error.msg}: {stripped[:200]}"
                )
                continue
            if not isinstance(event, dict):
                invalid_lines.append(
                    f"line {line_number}: event is {type(event).__name__}, want object"
                )
                continue

            raw_events += 1
            package = event.get("Package")
            action = event.get("Action")
            test = event.get("Test")
            if package:
                packages.add(package)
            if test is not None:
                events_by_test[test].append(action)
                if action == "output" and isinstance(event.get("Output"), str):
                    outputs_by_test[test].append(event["Output"])
            elif package and action:
                events_by_package[package].append(action)

    test_actions = {
        test: final_action(actions) for test, actions in events_by_test.items()
    }
    test_has_run = {
        test: "run" in actions for test, actions in events_by_test.items()
    }
    top_level = sorted(test for test in events_by_test if "/" not in test)
    subtests = sorted(test for test in events_by_test if "/" in test)

    def counted(names):
        counts = collections.Counter(test_actions[name] for name in names)
        return {
            "selected": len(names),
            "run": sum(1 for name in names if test_has_run[name]),
            "pass": counts.get("pass", 0),
            "skip": counts.get("skip", 0),
            "fail": counts.get("fail", 0),
            "incomplete": counts.get("incomplete", 0),
        }

    top_counts = counted(top_level)
    sub_counts = counted(subtests)
    expected_set = set(expected)
    selected_set = set(top_level)
    missing_or_nonpass = [
        test
        for test in expected
        if not test_has_run.get(test, False) or test_actions.get(test) != "pass"
    ]
    unexpected = sorted(selected_set - expected_set)
    failed_tests = sorted(
        test for test, action in test_actions.items() if action == "fail"
    )
    skipped_tests = sorted(
        test for test, action in test_actions.items() if action == "skip"
    )
    incomplete_tests = sorted(
        test for test, action in test_actions.items() if action == "incomplete"
    )
    package_actions = {
        package: final_action(actions)
        for package, actions in events_by_package.items()
    }
    package_failures = sorted(
        package for package, action in package_actions.items() if action != "pass"
    )
    required_logs = []
    for specification in args.require_log:
        test, separator, text = specification.partition("=")
        if not separator or not test or not text:
            required_logs.append(
                {
                    "test": test,
                    "text": text,
                    "matched": False,
                    "error": "invalid --require-log specification",
                }
            )
            continue
        required_logs.append(
            {
                "test": test,
                "text": text,
                "matched": any(text in output for output in outputs_by_test.get(test, [])),
            }
        )
    missing_required_logs = [
        required for required in required_logs if not required["matched"]
    ]
    no_skip_or_fail = not failed_tests and not skipped_tests
    all_expected_run_pass = not missing_or_nonpass and not unexpected
    selected_zero_tests = top_counts["selected"] == 0
    passed = (
        not invalid_lines
        and not selected_zero_tests
        and all_expected_run_pass
        and no_skip_or_fail
        and not incomplete_tests
        and not missing_required_logs
        and len(packages) == 1
        and not package_failures
    )

    summary = {
        "schema_version": 1,
        "label": args.label,
        "raw_json": args.json,
        "raw_events": raw_events,
        "invalid_json_lines": invalid_lines,
        "package_count": len(packages),
        "packages": [
            {
                "name": package,
                "terminal_action": package_actions.get(package, "incomplete"),
            }
            for package in sorted(packages)
        ],
        "package_failures": package_failures,
        "expected_count": len(expected),
        "expected": expected,
        "top_level": {
            "count": top_counts,
            "selected": top_level,
            "missing_or_nonpass": missing_or_nonpass,
            "unexpected": unexpected,
        },
        "subtests": {
            "count": sub_counts,
            "selected": subtests,
        },
        "failed_tests": failed_tests,
        "skipped_tests": skipped_tests,
        "incomplete_tests": incomplete_tests,
        "required_logs": required_logs,
        "missing_required_logs": missing_required_logs,
        "selected_zero_tests": selected_zero_tests,
        "no_skip_or_fail": no_skip_or_fail,
        "all_expected_run_pass": all_expected_run_pass,
        "passed": passed,
    }
    write_atomic(args.out, json.dumps(summary, indent=2, sort_keys=True) + "\n")

    line = (
        f"label={args.label} passed={str(passed).lower()} "
        f"packages={len(packages)}/{len(package_failures)}failed "
        f"top_level={top_counts['selected']} "
        f"top_run={top_counts['run']} top_pass={top_counts['pass']} "
        f"top_skip={top_counts['skip']} top_fail={top_counts['fail']} "
        f"top_incomplete={top_counts['incomplete']} "
        f"subtests={sub_counts['selected']} sub_pass={sub_counts['pass']} "
        f"sub_skip={sub_counts['skip']} sub_fail={sub_counts['fail']} "
        f"required_logs={len(required_logs)}/{len(missing_required_logs)}missing "
        f"invalid_json={len(invalid_lines)}"
    )
    print(line)
    if not passed:
        if invalid_lines:
            print("invalid JSON lines: " + "; ".join(invalid_lines), file=sys.stderr)
        if missing_or_nonpass:
            print(
                "expected tests not run/pass: " + ", ".join(missing_or_nonpass),
                file=sys.stderr,
            )
        if unexpected:
            print("unexpected top-level tests: " + ", ".join(unexpected), file=sys.stderr)
        if failed_tests:
            print("failed tests: " + ", ".join(failed_tests), file=sys.stderr)
        if skipped_tests:
            print("skipped tests: " + ", ".join(skipped_tests), file=sys.stderr)
        if incomplete_tests:
            print("incomplete tests: " + ", ".join(incomplete_tests), file=sys.stderr)
        if missing_required_logs:
            print(
                "missing required test logs: "
                + "; ".join(
                    f"{required['test']}={required['text']}"
                    for required in missing_required_logs
                ),
                file=sys.stderr,
            )
        if package_failures:
            print("non-passing packages: " + ", ".join(package_failures), file=sys.stderr)
        if len(packages) != 1:
            print(f"package count is {len(packages)}, want 1", file=sys.stderr)
    return 0 if passed else 1


if __name__ == "__main__":
    sys.exit(main())
