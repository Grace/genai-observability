#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
BUILD="$ROOT/build"
rm -rf "$BUILD" && mkdir -p "$BUILD"

build_one() {
  local src=$1 out=$2
  local tmp
  tmp=$(mktemp -d)
  echo "Building $out from $src"
  (cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o "$tmp/bootstrap" "$src")
  (cd "$tmp" && zip -q "$BUILD/$out.zip" bootstrap)
  rm -rf "$tmp"
}

build_one ./lambda/schema-eval schema-eval
build_one ./lambda/policy-eval policy-eval
build_one ./lambda/oracle-eval oracle-eval-a
build_one ./lambda/oracle-eval oracle-eval-b
build_one ./lambda/consensus consensus
build_one ./lambda/normalizer normalizer
build_one ./lambda/replay-worker replay-worker

echo "Lambda packages are in $BUILD"
