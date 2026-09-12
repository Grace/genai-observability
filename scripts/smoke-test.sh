#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
INFRA="$ROOT/infra"
URL=${ALB_URL:-$(terraform -chdir="$INFRA" output -raw alb_url)}
BUCKET=${CORPUS_BUCKET:-$(terraform -chdir="$INFRA" output -raw corpus_bucket)}

echo "POST $URL/ask"
RESP=$(curl -fsS -X POST "$URL/ask" -H 'content-type: application/json' -d '{"question":"What color is the daytime sky on a clear day?","evidence":["On a clear day, Rayleigh scattering causes the daytime sky to appear blue to human observers."]}')
echo "$RESP" | python3 -m json.tool
TRACE_ID=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["trace_id"])' <<<"$RESP")

echo "Waiting for asynchronous evaluation object s3://$BUCKET/evaluations/$TRACE_ID.json"
for _ in $(seq 1 40); do
  if aws s3api head-object --bucket "$BUCKET" --key "evaluations/$TRACE_ID.json" >/dev/null 2>&1; then
    aws s3 cp "s3://$BUCKET/evaluations/$TRACE_ID.json" -
    echo
    exit 0
  fi
  sleep 3
done

echo "Evaluation did not appear within the smoke-test window. Inspect Step Functions and CloudWatch logs." >&2
exit 1
