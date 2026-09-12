#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
INFRA="$ROOT/infra"
ARN=${REPLAY_STATE_MACHINE_ARN:-$(terraform -chdir="$INFRA" output -raw replay_state_machine_arn)}
MODEL=${1:-$(awk -F'"' '/replay_model_id/ {print $2; exit}' "$INFRA/terraform.tfvars" 2>/dev/null || true)}
PROMPT=${2:-replay-v2}
[[ -n "$MODEL" ]] || { echo "Pass a replay model ID as arg 1" >&2; exit 1; }
aws stepfunctions start-execution --state-machine-arn "$ARN" --input "{\"model\":\"$MODEL\",\"prompt_version\":\"$PROMPT\"}"
