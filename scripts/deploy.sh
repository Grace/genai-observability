#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
INFRA="$ROOT/infra"
: "${AWS_REGION:=us-east-1}"

for cmd in aws terraform docker go zip; do command -v "$cmd" >/dev/null || { echo "$cmd is required" >&2; exit 1; }; done

"$ROOT/scripts/build-lambdas.sh"
terraform -chdir="$INFRA" init
# Bootstrap ECR so the application image exists before ECS is created.
terraform -chdir="$INFRA" apply -auto-approve -target=aws_ecr_repository.app
REPO=$(terraform -chdir="$INFRA" output -raw ecr_repository_url 2>/dev/null || aws ecr describe-repositories --region "$AWS_REGION" --repository-names genai-observability-app --query 'repositories[0].repositoryUri' --output text)
REGISTRY=${REPO%%/*}
aws ecr get-login-password --region "$AWS_REGION" | docker login --username AWS --password-stdin "$REGISTRY"
docker build --platform linux/amd64 -t "$REPO:latest" "$ROOT"
docker push "$REPO:latest"
terraform -chdir="$INFRA" apply -auto-approve

echo
echo "Deployment complete"
terraform -chdir="$INFRA" output
