#!/usr/bin/env python3
"""Deterministically assess the three-check CD release Spec using configured JSON signals."""

import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path


def assess(signals):
    """Return every required check, including UNKNOWN when signals are missing."""
    def section(key):
        value = signals.get(key) if isinstance(signals, dict) else None
        return value if isinstance(value, dict) else {"error": key + " signals are unavailable"}

    freeze = section("freezeWindow")
    active, incident = freeze.get("active"), freeze.get("blockingIncident")
    freeze_verdict = ("BLOCKED" if active is True or incident is True else
                      "PASS" if active is False and incident is False else "UNKNOWN")

    change = section("changeAssessment")
    flags = [change.get(key) for key in ("backwardCompatible", "blastRadiusBounded", "canaryHealthy")]
    change_verdict = ("BLOCKED" if any(value is False for value in flags) else
                      "PASS" if all(value is True for value in flags) else "UNKNOWN")

    rollback = section("rollback")
    target = rollback.get("target")
    rollback_verdict = ("BLOCKED" if rollback.get("ready") is False else
                        "PASS" if rollback.get("ready") is True and
                        isinstance(target, str) and target.strip() else "UNKNOWN")

    observed_at = datetime.now(timezone.utc).isoformat()
    checks = []
    for check_id, requirement, verdict, observation in (
        ("freeze-window", "freeze-status", freeze_verdict, freeze),
        ("compatibility-and-blast-radius", "change-assessment", change_verdict, change),
        ("rollback-readiness", "rollback-target", rollback_verdict, rollback),
    ):
        checks.append({
            "checkId": check_id,
            "verdict": verdict,
            "summary": check_id + ": " + verdict,
            "evidence": [{
                "requirementId": requirement,
                "observedAt": observed_at,
                "querySummary": "Read configured release signals JSON for " + check_id,
                "resultSummary": json.dumps(observation, sort_keys=True),
            }],
        })
    verdicts = [check["verdict"] for check in checks]
    aggregate = "BLOCKED" if "BLOCKED" in verdicts else "UNKNOWN" if "UNKNOWN" in verdicts else "PASS"
    return aggregate, checks


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--signals", required=True, help="JSON release signals file")
    args = parser.parse_args()
    run = json.loads(Path(os.environ["OG_RUN_FILE"]).read_text(encoding="utf-8"))
    # An Agent evaluator can read these same frozen instructions and use its tools.
    if not Path(os.environ["OG_SPEC_FILE"]).read_text(encoding="utf-8").strip():
        raise ValueError("frozen Spec is empty")
    try:
        signals = json.loads(Path(args.signals).read_text(encoding="utf-8"))
    except (OSError, ValueError):
        signals = {}
    verdict, checks = assess(signals)
    snapshot = {
        "baseSubmissionNo": run["latestSubmissionNo"],
        "finishRun": True,
        "verdict": verdict,
        "summary": "CD release assessment: " + verdict,
        "checks": checks,
    }
    Path(os.environ["OG_SUBMISSION_FILE"]).write_text(json.dumps(snapshot), encoding="utf-8")
    # A completed BLOCKED/UNKNOWN assessment is submitted before deployment is denied.
    print("CD release assessment: " + verdict)


if __name__ == "__main__":
    main()
