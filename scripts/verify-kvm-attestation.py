#!/usr/bin/env python3
import hashlib
import json
import pathlib
import sys


def fail(message: str) -> None:
    raise SystemExit(message)


if len(sys.argv) != 10:
    fail(
        "usage: verify-kvm-attestation.py <gh-verification-json> <predicate> <report> "
        "<candidate> <distro> <commit> <acceptance-manifest> <runner> <harness-sha256>"
    )

verification_path = pathlib.Path(sys.argv[1])
predicate_path = pathlib.Path(sys.argv[2])
report_path = pathlib.Path(sys.argv[3])
candidate_path = pathlib.Path(sys.argv[4])
distro = sys.argv[5]
commit = sys.argv[6]
manifest_path = pathlib.Path(sys.argv[7])
expected_runner = sys.argv[8]
expected_harness = sys.argv[9]

verification = json.loads(verification_path.read_text(encoding="utf-8"))
if not isinstance(verification, list) or len(verification) != 1:
    fail("expected exactly one cryptographically verified KVM attestation")
statement = verification[0].get("verificationResult", {}).get("statement")
if not isinstance(statement, dict):
    fail("gh verification output has no parsed in-toto statement")

predicate = json.loads(predicate_path.read_text(encoding="utf-8"))
report = json.loads(report_path.read_text(encoding="utf-8"))
if statement.get("predicateType") != "https://github.com/AdminTurnedDevOps/ABox/attestations/kvm-test/v1":
    fail("unexpected KVM attestation predicate type")
if statement.get("predicate") != predicate:
    fail("downloaded predicate does not match the signed DSSE statement")

candidate_digest = hashlib.sha256(candidate_path.read_bytes()).hexdigest()
subjects = statement.get("subject")
if not isinstance(subjects, list) or len(subjects) != 1:
    fail("KVM attestation must have exactly one subject")
if subjects[0].get("digest", {}).get("sha256") != candidate_digest:
    fail("KVM attestation subject is not the release candidate")

manifest_digest = hashlib.sha256(manifest_path.read_bytes()).hexdigest()
manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
baselines = {
    "arch": "arch-2026-09-17-x86_64-libkrun-1.19.4-1-libkrunfw-5.5.0-1",
    "fedora": "fedora-44-x86_64-libkrun-1.19.0-1.fc44-libkrunfw-5.5.0-1.fc44-selinux-enforcing",
}
if distro not in baselines:
    fail(f"unsupported attested distro: {distro!r}")
expected = {
    "schema": 1,
    "distro": distro,
    "commit": commit,
    "candidate_sha256": candidate_digest,
    "acceptance_manifest_sha256": manifest_digest,
    "runner": expected_runner,
    "baseline": baselines[distro],
    "harness_sha256": expected_harness,
}
if set(predicate) != set(expected) | {"results"}:
    fail(f"KVM predicate has an unexpected top-level schema: {sorted(predicate)!r}")
for key, value in expected.items():
    if predicate.get(key) != value:
        fail(f"KVM predicate {key!r} is {predicate.get(key)!r}; expected {value!r}")
results = predicate.get("results")
expected_tests = manifest.get("tests")
if not isinstance(expected_tests, list) or not expected_tests:
    fail("acceptance manifest has no tests")
if not isinstance(results, list):
    fail("KVM predicate results must be a list")
seen = {}
for result in results:
    if not isinstance(result, dict) or set(result) - {"id", "status", "detail"}:
        fail(f"malformed KVM result: {result!r}")
    if "id" not in result or "status" not in result:
        fail(f"incomplete KVM result: {result!r}")
    test_id = result.get("id")
    if test_id in seen:
        fail(f"duplicate KVM result: {test_id!r}")
    seen[test_id] = result.get("status")
if set(seen) != set(expected_tests):
    fail("KVM predicate test set differs from the acceptance manifest")
if any(status != "pass" for status in seen.values()):
    fail("KVM predicate does not contain an all-pass result set")

signed_report = dict(predicate)
signed_report.pop("baseline", None)
signed_report.pop("harness_sha256", None)
if report != signed_report:
    fail("published hardware report does not match the signed predicate")
