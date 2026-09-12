#!/usr/bin/env python3
"""Verify the rendered architecture SVG is current with its Graphviz source."""
import html
import re
import sys


def labels(dot_text):
    for raw in re.findall(r'label="([^"]*)"', dot_text):
        for line in raw.split("\\n"):
            line = line.strip()
            if line:
                yield line


def main(dot_path, svg_path):
    try:
        dot_text = open(dot_path, encoding="utf-8").read()
        svg_text = open(svg_path, encoding="utf-8").read()
    except OSError as exc:
        print(f"{exc}; run: make diagram", file=sys.stderr)
        return 1

    # Graphviz XML-escapes label text and additionally writes hyphens as &#45;.
    missing = [
        label
        for label in labels(dot_text)
        if label not in svg_text
        and html.escape(label, quote=False).replace("-", "&#45;") not in svg_text
    ]
    if missing:
        for label in missing:
            print(f"stale diagram: label not in rendered SVG: {label}", file=sys.stderr)
        print("docs/full-architecture.svg is out of date; run: make diagram", file=sys.stderr)
        return 1
    print("diagram is current")
    return 0


if __name__ == "__main__":
    sys.exit(main(*sys.argv[1:3]))
