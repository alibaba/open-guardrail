#!/usr/bin/env bash
set -euo pipefail
umask 077
work_dir=$(mktemp -d)
cred_dir=""
trap 'rm -rf "$work_dir"; if [ -n "$cred_dir" ]; then rm -rf "$cred_dir"; fi' EXIT

case "$OG_MODE" in
  evaluate|create|gate|verify) ;;
  *)
    echo "::error::Unsupported mode '$OG_MODE' (expected evaluate, create, gate, or verify)"
    exit 2
    ;;
esac

if [ "$OG_MODE" = "evaluate" ] && [ -z "${OG_ASSESSMENT_COMMAND:-}" ]; then
  echo "::error::assessment-command is required in evaluate mode"
  exit 2
fi
if [ -z "$OG_SERVER" ]; then
  echo "::error::server is required; set OPEN_GUARDRAIL_URL and pass it as the server input"
  exit 2
fi
for required_command in jq "$OG_CTL"; do
  if ! command -v "$required_command" > /dev/null 2>&1; then
    echo "::error::Required command '$required_command' is unavailable; install jq and guardrailctl or set guardrailctl-path"
    exit 2
  fi
done
if [ -n "${OG_SPEC_FILE_INPUT:-}" ]; then
  if [ "$OG_MODE" != "create" ] && [ "$OG_MODE" != "evaluate" ]; then
    echo "::error::spec-file is only used in create/evaluate mode; verification never publishes policy"
    exit 2
  fi
  if [ ! -r "$OG_SPEC_FILE_INPUT" ] || [ ! -f "$OG_SPEC_FILE_INPUT" ]; then
    echo "::error::spec-file must point to a readable Spec YAML file in the checked-out workspace"
    exit 2
  fi
fi

changeset_file="$work_dir/changeset.json"
if [ -n "$OG_CHANGE_SET_JSON" ]; then
  printf '%s' "$OG_CHANGE_SET_JSON" > "$changeset_file"
else
  jq -n \
    --arg repo "${GITHUB_REPOSITORY:?}" \
    --arg run_id "${GITHUB_RUN_ID:?}" \
    --arg run_attempt "${GITHUB_RUN_ATTEMPT:?}" \
    --arg sha "${GITHUB_SHA:?}" \
    --arg ref "${GITHUB_REF:?}" \
    --arg environment "$OG_DEPLOYMENT_ENVIRONMENT" \
    '{repository:$repo,workflowRunId:$run_id,workflowRunAttempt:$run_attempt,sha:$sha,ref:$ref,environment:$environment}' > "$changeset_file"
fi
if ! jq -e -s 'length == 1 and (.[0] | type == "object")' "$changeset_file" > /dev/null; then
  echo "::error::change-set-json must contain exactly one JSON object"
  exit 2
fi

if [ -n "${OG_SPEC_FILE_INPUT:-}" ]; then
  "$OG_CTL" scene apply --server "$OG_SERVER" --key "$OG_SCENE" \
    --name "$OG_SCENE" --spec "$OG_SPEC_FILE_INPUT" --publish > "$work_dir/scene.json"
fi

if [ "$OG_MODE" = "create" ] || [ "$OG_MODE" = "evaluate" ]; then
  cred_dir=$(mktemp -d)
  token_file="$cred_dir/submission-credential.json"
  create_args=(run create --server "$OG_SERVER" --scene "$OG_SCENE" --change-set "$changeset_file" --submission-credential-file "$token_file")
  if [ -n "$OG_BUSINESS_ID" ]; then
    create_args+=(--business-id "$OG_BUSINESS_ID")
  fi
  if [ -n "$OG_OPERATION_ID" ]; then
    create_args+=(--operation-id "$OG_OPERATION_ID")
  fi
  run_json=$("$OG_CTL" "${create_args[@]}")
  OG_RUN_ID=$(printf '%s' "$run_json" | jq -r '.id // empty')
  if [ -z "$OG_RUN_ID" ]; then
    echo "::error::Failed to parse Run ID from guardrailctl run create"
    exit 1
  fi
  echo "run-id=$OG_RUN_ID" >> "$GITHUB_OUTPUT"
  if [ "$OG_MODE" = "create" ]; then
    credential_dir=$(mktemp -d "${RUNNER_TEMP:-/tmp}/og-sub.XXXXXX")
    persisted_token="$credential_dir/credential.json"
    install -m 600 "$token_file" "$persisted_token"
    echo "submission-credential-file=$persisted_token" >> "$GITHUB_OUTPUT"
    exit 0
  fi

  # Give the evaluator immutable inputs and an output path, not the credential.
  export OG_RUN_FILE="$work_dir/run.json"
  export OG_SPEC_FILE="$work_dir/spec.yaml"
  export OG_SUBMISSION_FILE="$work_dir/submission.json"
  printf '%s' "$run_json" > "$OG_RUN_FILE"
  "$OG_CTL" run revision --server "$OG_SERVER" "$OG_RUN_ID" > "$work_dir/revision.json"
  if ! jq -e --slurpfile run "$OG_RUN_FILE" \
    '.id == $run[0].policyRevisionId and .digest == $run[0].specDigest' \
    "$work_dir/revision.json" > /dev/null; then
    echo "::error::Run policy Revision does not match its frozen identity (runId=$OG_RUN_ID)"
    exit 1
  fi
  jq -er '.yaml' "$work_dir/revision.json" > "$OG_SPEC_FILE"
  if bash -euo pipefail -c "$OG_ASSESSMENT_COMMAND"; then
    :
  else
    assessment_status=$?
    echo "::error::Assessment command failed (runId=$OG_RUN_ID, exitCode=$assessment_status); no automatic retry"
    exit "$assessment_status"
  fi
  if [ ! -s "$OG_SUBMISSION_FILE" ] || ! jq -e '.finishRun == true' "$OG_SUBMISSION_FILE" > /dev/null; then
    echo "::error::Assessment must write a complete final Snapshot to OG_SUBMISSION_FILE"
    exit 1
  fi
  submit_args=(run submit --server "$OG_SERVER" --submission-credential-file "$token_file" --submission "$OG_SUBMISSION_FILE")
  if [ -n "$OG_OPERATION_ID" ]; then
    submit_args+=(--operation-id "$OG_OPERATION_ID-submit")
  fi
  "$OG_CTL" "${submit_args[@]}" "$OG_RUN_ID" > "$work_dir/accepted.json"
fi

if [ -z "$OG_RUN_ID" ]; then
  echo "::error::run-id is required in gate and verify modes; create a Run and hand off its credential before waiting"
  exit 1
fi
if [ "$OG_MODE" != "evaluate" ]; then
  echo "run-id=$OG_RUN_ID" >> "$GITHUB_OUTPUT"
fi

if [ "$OG_MODE" = "gate" ]; then
  wait_args=(run wait --server "$OG_SERVER")
  if [ -n "$OG_WAIT_TIMEOUT" ]; then
    wait_args+=(--timeout "$OG_WAIT_TIMEOUT")
  fi
  wait_args+=("$OG_RUN_ID")
  if run_json=$("$OG_CTL" "${wait_args[@]}"); then
    :
  else
    wait_status=$?
    echo "decision=DENY" >> "$GITHUB_OUTPUT"
    echo "result=UNKNOWN" >> "$GITHUB_OUTPUT"
    echo "::error::OpenGuardrail blocked deployment: run wait failed (runId=$OG_RUN_ID, timeout=${OG_WAIT_TIMEOUT:-default}, exitCode=$wait_status). See guardrailctl diagnostics above."
    exit "$wait_status"
  fi
else
  run_json=$("$OG_CTL" run get --server "$OG_SERVER" "$OG_RUN_ID")
fi

if ! printf '%s' "$run_json" | jq -e \
  --arg id "$OG_RUN_ID" --arg scene "$OG_SCENE" --arg business "$OG_BUSINESS_ID" \
  --slurpfile expected "$changeset_file" \
  '.id == $id and .sceneKey == $scene and .changeSet == $expected[0] and ($business == "" or .businessId == $business)' > /dev/null; then
  echo "decision=DENY" >> "$GITHUB_OUTPUT"
  echo "result=UNKNOWN" >> "$GITHUB_OUTPUT"
  echo "::error::OpenGuardrail Run does not match the expected scene, deployment context, or business ID (runId=$OG_RUN_ID)"
  exit 1
fi

status=$(printf '%s' "$run_json" | jq -r '.status // empty')
decision=$(printf '%s' "$run_json" | jq -r '.decision // "DENY"')
result=$(printf '%s' "$run_json" | jq -r '.result // "UNKNOWN"')
expires_at=$(printf '%s' "$run_json" | jq -r '.decisionExpiresAt // empty')
now_epoch=$(date +%s)

echo "decision=$decision" >> "$GITHUB_OUTPUT"
echo "result=$result" >> "$GITHUB_OUTPUT"

if [ "$status" != "FINISHED" ] || [ "$result" != "PASS" ] || [ "$decision" != "ALLOW" ]; then
  echo "::error::OpenGuardrail blocked deployment (runId=$OG_RUN_ID, status=$status, result=$result, decision=$decision)"
  exit 1
fi
if ! [[ "$expires_at" =~ ^[0-9]+$ ]] || ! [ "$expires_at" -gt "$now_epoch" ]; then
  echo "::error::OpenGuardrail ALLOW decision is missing or expired (runId=$OG_RUN_ID, decisionExpiresAt=${expires_at:-missing})"
  exit 1
fi
echo "OpenGuardrail verified ALLOW (runId=$OG_RUN_ID, result=$result)"
