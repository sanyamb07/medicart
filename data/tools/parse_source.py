"""Parse the supplier price list (SR NO / PRODUCT NAME / GENERIC / PRICE) into rows.

Columns are separated by runs of whitespace (the paste uses 4 spaces, some
cells carry trailing spaces, and GENERIC may be blank). SR NO is the first
token and PRICE the last; the middle is split on the widest gap.
"""
import json
import re
import sys

ROW_RE = re.compile(r"^\s*(\d+)\s+(.*?)\s+(\d+(?:\.\d+)?)\s*$")


def parse(path):
    rows = []
    with open(path, encoding="utf-8") as fh:
        text = fh.read()
    # The paste is missing a newline before the trailing header copy.
    text = text.replace("SR NO    PRODUCT NAME", "\nSR NO    PRODUCT NAME")
    for line in text.splitlines():
        if not line.strip() or line.strip().startswith("SR NO"):
            continue
        m = ROW_RE.match(line)
        if not m:
            raise ValueError(f"unparseable line: {line!r}")
        sr, middle, price = int(m.group(1)), m.group(2), m.group(3)
        parts = re.split(r"\s{3,}|\t+", middle.strip(), maxsplit=1)
        name = parts[0].strip()
        generic = parts[1].strip() if len(parts) > 1 else ""
        rows.append({"sr": sr, "name": name, "generic": generic, "price": price})
    return rows


if __name__ == "__main__":
    rows = parse(sys.argv[1])
    json.dump(rows, sys.stdout, indent=0)
