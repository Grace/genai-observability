#!/usr/bin/env bash
set -euo pipefail
REGION=${AWS_REGION:-us-east-1}
PROJECT=${PROJECT:-genai-observability}
SECRET_NAME="$PROJECT/honeycomb-api-key"
if [[ -z "${HONEYCOMB_API_KEY:-}" ]]; then
  echo "Set HONEYCOMB_API_KEY in your shell first." >&2
  exit 1
fi
if aws secretsmanager describe-secret --region "$REGION" --secret-id "$SECRET_NAME" >/dev/null 2>&1; then
  aws secretsmanager put-secret-value --region "$REGION" --secret-id "$SECRET_NAME" --secret-string "$HONEYCOMB_API_KEY" >/dev/null
else
  aws secretsmanager create-secret --region "$REGION" --name "$SECRET_NAME" --secret-string "$HONEYCOMB_API_KEY" >/dev/null
fi
aws secretsmanager describe-secret --region "$REGION" --secret-id "$SECRET_NAME" --query ARN --output text
