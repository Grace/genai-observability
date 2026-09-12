#!/usr/bin/env bash
# Fail if docs/full-architecture.dot has changed without the rendered SVG being
# regenerated.
#
# A byte comparison is not usable here: Graphviz embeds its own version in the
# output, so the same source renders differently across machines and CI. Instead
# this checks that every label in the source appears in the rendered SVG, which
# is what actually matters - a stale diagram shows components that no longer
# match the source, and that is exactly how a diagram starts lying.
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
DOT="$ROOT/docs/full-architecture.dot"
SVG="$ROOT/docs/full-architecture.svg"

[ -f "$SVG" ] || { echo "missing $SVG; run: make diagram" >&2; exit 1; }

missing=0
# Pull label="..." values, split on \n, and check each line is present in the SVG.
while IFS= read -r line; do
  [ -n "$line" ] || continue
  # Graphviz XML-escapes label text: & < > and hyphens all change form.
  escaped=${line//&/&amp;}
  escaped=${escaped//</&lt;}
  escaped=${escaped//>/&gt;}
  escaped=${escaped//-/&#45;}
  if ! grep -qF -- "$line" "$SVG" && ! grep -qF -- "$escaped" "$SVG"; then
    echo "stale diagram: label not found in rendered SVG: $line" >&2
    missing=1
  fi
done < <(sed -n 's/.*label="\([^"]*\)".*/\1/p' "$DOT" | sed 's/\\n/\n/g')

if [ "$missing" -ne 0 ]; then
  echo "docs/full-architecture.svg is out of date; run: make diagram" >&2
  exit 1
fi
echo "diagram is current"
