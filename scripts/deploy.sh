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
# Tag by commit, not :latest. With a constant tag the task definition's image
# string never changes, so Terraform sees no diff, no new revision is created,
# and ECS keeps serving the previous image - a code-only change deploys nothing
# while appearing to succeed. That is not hypothetical: the live service ran
# pre-fix code for a full day because of it.
TAG=$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo "manual-$(date +%s)")
if ! git -C "$ROOT" diff --quiet 2>/dev/null || ! git -C "$ROOT" diff --cached --quiet 2>/dev/null; then
  TAG="$TAG-dirty"
  echo "WARNING: working tree has uncommitted changes; tagging image $TAG" >&2
fi

echo "Building image $REPO:$TAG"
docker build --platform linux/amd64 -t "$REPO:$TAG" -t "$REPO:latest" "$ROOT"
docker push "$REPO:$TAG"
docker push "$REPO:latest"

terraform -chdir="$INFRA" apply -auto-approve -var "app_image_tag=$TAG"

echo
echo "Deployment complete"
terraform -chdir="$INFRA" output
