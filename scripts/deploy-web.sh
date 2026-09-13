#!/usr/bin/env bash
# Build the Normalization Inspector and publish it to S3 behind CloudFront.
#
# Run after the infrastructure exists: it reads the bucket and distribution from
# Terraform outputs rather than taking them as arguments, so it cannot publish to
# the wrong place.
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
INFRA="$ROOT/infra"

for cmd in aws terraform npm; do
  command -v "$cmd" >/dev/null || { echo "$cmd is required" >&2; exit 1; }
done

BUCKET=$(terraform -chdir="$INFRA" output -raw web_bucket)
[ -n "$BUCKET" ] || { echo "web_bucket is empty; set web_domain and apply first" >&2; exit 1; }
API_URL=$(terraform -chdir="$INFRA" output -raw web_url)

# Same-origin: the app posts to /normalize on the hostname it was served from,
# so there is no CORS and no mixed content.
echo "Building inspector against $API_URL"
(cd "$ROOT/web" && npm ci --silent && VITE_NORMALIZE_API="$API_URL" npm run build)

echo "Publishing to s3://$BUCKET"
# Hashed asset filenames can be cached hard; index.html must not be, or a deploy
# is invisible until the cache expires.
aws s3 sync "$ROOT/web/dist" "s3://$BUCKET" --delete \
  --exclude index.html --cache-control "public,max-age=31536000,immutable"
aws s3 cp "$ROOT/web/dist/index.html" "s3://$BUCKET/index.html" \
  --cache-control "no-cache,must-revalidate"

DIST=$(aws cloudfront list-distributions \
  --query "DistributionList.Items[?Aliases.Items[0]=='${API_URL#https://}'].Id | [0]" --output text)
if [ -n "$DIST" ] && [ "$DIST" != "None" ]; then
  echo "Invalidating $DIST"
  aws cloudfront create-invalidation --distribution-id "$DIST" --paths "/*" \
    --query 'Invalidation.Status' --output text
fi

echo "Published: $API_URL"
