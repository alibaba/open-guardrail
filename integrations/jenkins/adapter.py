#!/usr/bin/env python3
"""Translate a Jenkins build/checkpoint into one Recovery Agent gate invocation."""

import argparse
import json
import os
from pathlib import Path
import sys

from integrations.recovery_agent import gate


def context_from_jenkins(jenkins_env, *, instance_id, checkpoint, service, candidate,
                         environment, observation, key_file, cli):
    job_name = jenkins_env.get("JOB_NAME", "")
    build_number = jenkins_env.get("BUILD_NUMBER", "")
    build_tag = jenkins_env.get("BUILD_TAG", "")
    if not instance_id or not checkpoint or not service or not environment or not candidate:
        raise ValueError("instance, checkpoint, service, environment and candidate are required")
    if not job_name or not (build_number.isascii() and build_number.isdigit()) or not build_tag:
        raise ValueError("Jenkins JOB_NAME, BUILD_NUMBER and BUILD_TAG are required")
    if not key_file:
        raise ValueError("Jenkins GUARDRAIL_KEY_FILE credential is required")
    delivery = {"type": "jenkins", "instanceId": instance_id,
                "jobName": job_name, "buildNumber": build_number,
                "buildTag": build_tag, "checkpoint": checkpoint}
    identity = json.dumps(delivery, sort_keys=True, separators=(",", ":"))
    return gate.GateInput(
        change_set={"service": service, "environment": environment,
                    "candidate": candidate, "delivery": delivery},
        operation_identity=identity,
        observation=observation,
        key_file=key_file,
        cli=cli,
    )


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--phase", choices=("prepare", "poll"), required=True)
    parser.add_argument("--state-dir", required=True, help="private state directory shared by gate stages")
    source = parser.add_mutually_exclusive_group()
    source.add_argument("--observation", help="synthetic observation fixture")
    source.add_argument("--health-url", help="real canary health JSON URL")
    parser.add_argument("--instance-id", help="stable, unique Jenkins controller ID")
    parser.add_argument("--checkpoint", help="stable checkpoint ID within this build")
    parser.add_argument("--service")
    parser.add_argument("--environment")
    parser.add_argument("--candidate", help="immutable deployed artifact or demo build tag")
    parser.add_argument("--cli", default="guardrailctl")
    parser.add_argument("--recipient", help="age public key supplied by the initiating Agent")
    parser.add_argument("--envelope", help="encrypted artifact path")
    args = parser.parse_args()
    try:
        if not os.environ.get("OPEN_GUARDRAIL_URL"):
            raise ValueError("Jenkins job must configure OPEN_GUARDRAIL_URL")
        if args.phase == "prepare":
            key_file = os.environ.get("GUARDRAIL_KEY_FILE", "")
            if not key_file or not Path(key_file).is_file():
                raise ValueError("Jenkins file credential GUARDRAIL_KEY_FILE is unavailable")
            if not args.recipient or not args.recipient.startswith("age1") or not args.envelope:
                raise ValueError("an age recipient and envelope path are required")
            if not (args.observation or args.health_url):
                raise ValueError("prepare needs --observation or --health-url")
            observation = gate.load_observation(args.observation, args.health_url)
            gate.prepare_gate(context_from_jenkins(
                os.environ, instance_id=args.instance_id, checkpoint=args.checkpoint,
                service=args.service, environment=args.environment, candidate=args.candidate,
                observation=observation, key_file=key_file, cli=args.cli,
            ), args.state_dir, args.recipient, args.envelope)
        else:
            key_file = os.environ.get("GUARDRAIL_KEY_FILE", "")
            if not key_file or not Path(key_file).is_file():
                raise ValueError("Jenkins file credential GUARDRAIL_KEY_FILE is unavailable")
            return 0 if gate.poll_gate(args.cli, key_file, args.state_dir) else 2
    except (KeyError, ValueError, RuntimeError, OSError) as error:
        print(f"Jenkins Guardrail adapter stopped: {error}. Inspect the Run and private "
              "state before retrying; see docs/runs.md.", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
