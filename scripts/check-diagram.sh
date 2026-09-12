#!/usr/bin/env bash
# Fail if docs/full-architecture.dot has changed without the rendered SVG being
# regenerated.
#
# A byte comparison is not usable: Graphviz embeds its own version in the output,
# so the same source renders differently across machines. This compares labels
# instead, which is what actually matters - a stale diagram shows components that
# no longer match the source, and that is how a diagram starts lying.
#
# The comparison is in Python rather than shell because bash 5.2 expands & in a
# ${var//x/y} replacement to the matched text while older bash does not, so the
# XML escaping silently differed between a laptop and CI.
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
exec python3 "$ROOT/scripts/check_diagram.py" "$ROOT/docs/full-architecture.dot" "$ROOT/docs/full-architecture.svg"
