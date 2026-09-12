#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
INFRA="$ROOT/infra"
URL=${ALB_URL:-$(terraform -chdir="$INFRA" output -raw alb_url)}
for fixture in otel openllmetry braintrust bedrock; do
  echo "== $fixture =="
  curl -fsS -X POST "$URL/normalize" \
    -H 'content-type: application/json' \
    --data-binary @"$ROOT/examples/normalization/$fixture.json" | python3 -m json.tool
  echo
done
