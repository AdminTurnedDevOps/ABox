#!/usr/bin/env python3
import hashlib
import json
import pathlib
import sys


def fail(message: str) -> None:
    raise SystemExit(message)


if len(sys.argv) != 7:
    fail(
        "usage: validate-hardware-report.py <manifest> <report> <distro> "
        "<commit> <candidate-sha256> <runner>"
    )

manifest_path = pathlib.Path(sys.argv[1])
report_path = pathlib.Path(sys.argv[2])
distro, commit, candidate, runner = sys.argv[3:]
manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
report = json.loads(report_path.read_text(encoding="utf-8"))
manifest_digest = hashlib.sha256(manifest_path.read_bytes()).hexdigest()

expected_fields = {
    "schema": 1,
    "distro": distro,
    "commit": commit,
    "candidate_sha256": candidate,
    "acceptance_manifest_sha256": manifest_digest,
    "runner": runner,
}
if set(report) != set(expected_fields) | {"results"}:
    fail(f"hardware report has an unexpected top-level schema: {sorted(report)!r}")
for key, expected in expected_fields.items():
    if report.get(key) != expected:
        fail(f"hardware report {key!r} is {report.get(key)!r}; expected {expected!r}")

expected_tests = manifest.get("tests")
results = report.get("results")
if not isinstance(expected_tests, list) or not expected_tests:
    fail("acceptance manifest has no tests")
if not isinstance(results, list):
    fail("hardware report results must be a list")

seen = {}
for result in results:
    if not isinstance(result, dict) or set(result) - {"id", "status", "detail"}:
        fail(f"malformed hardware result: {result!r}")
    if "id" not in result or "status" not in result:
        fail(f"incomplete hardware result: {result!r}")
    test_id = result.get("id")
    if test_id in seen:
        fail(f"duplicate hardware result: {test_id!r}")
    seen[test_id] = result.get("status")

if set(seen) != set(expected_tests):
    fail(
        "hardware report test set differs from acceptance manifest: "
        f"missing={sorted(set(expected_tests) - set(seen))}, "
        f"extra={sorted(set(seen) - set(expected_tests))}"
    )
failed = sorted(test_id for test_id, status in seen.items() if status != "pass")
if failed:
    fail(f"hardware acceptance did not pass: {failed}")

print(manifest_digest)
