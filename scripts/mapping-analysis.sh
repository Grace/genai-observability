#!/usr/bin/env bash
set -euo pipefail
if [[ $# -lt 2 ]]; then
  echo "usage: $0 <mapping-state-machine-arn> <mapping-request.json>" >&2
  exit 2
fi
ARN=$1
FILE=$2
aws stepfunctions start-execution \
  --state-machine-arn "$ARN" \
  --input "file://$FILE"
