#!/usr/bin/env python3
"""Run the bounded Staticcheck warning lease for Phase 2; remove for #172."""

import json
from pathlib import Path
import subprocess
import sys


KNOWN = [
    {"code": "U1000", "severity": "error", "location": {"file": "internal/config/config.go", "line": 47, "column": 6}, "end": {"file": "", "line": 0, "column": 0}, "message": "func integer is unused"},
    {"code": "U1000", "severity": "error", "location": {"file": "internal/config/validate.go", "line": 407, "column": 6}, "end": {"file": "", "line": 0, "column": 0}, "message": "func isJSONSpace is unused"},
    {"code": "ST1005", "severity": "error", "location": {"file": "internal/content/content.go", "line": 25, "column": 27}, "end": {"file": "internal/content/content.go", "line": 25, "column": 66}, "message": "error strings should not be capitalized"},
    {"code": "U1000", "severity": "error", "location": {"file": "internal/web/web.go", "line": 33, "column": 5}, "end": {"file": "", "line": 0, "column": 0}, "message": "var savedPageTemplate is unused"},
]


def fail(message):
    print("Staticcheck failed closed: " + message, file=sys.stderr)
    raise SystemExit(1)


def reject_duplicate_keys(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate JSON key")
        result[key] = value
    return result


def main():
    if len(sys.argv) != 2:
        fail("usage: check-staticcheck-phase2.py SCANNER")
    scanner = Path(sys.argv[1])
    if not scanner.is_absolute() or scanner.name != "staticcheck":
        fail("expected the freshly installed absolute staticcheck path")
    try:
        result = subprocess.run([str(scanner), "-f", "json", "./..."],
                                capture_output=True, text=True, check=False)
    except OSError as error:
        fail("could not execute scanner: " + str(error))
    if result.stderr:
        fail("scanner wrote to stderr: " + result.stderr.rstrip())
    try:
        diagnostics = [json.loads(line, object_pairs_hook=reject_duplicate_keys)
                       for line in result.stdout.splitlines()]
    except (json.JSONDecodeError, ValueError) as error:
        fail("invalid diagnostic JSON: " + str(error))

    root = str(Path.cwd().resolve()) + "/"
    for diagnostic in diagnostics:
        if not isinstance(diagnostic, dict):
            fail("diagnostic is not an object")
        for field in ("location", "end"):
            position = diagnostic.get(field)
            if not isinstance(position, dict):
                fail("diagnostic has malformed " + field)
            filename = position.get("file")
            if isinstance(filename, str) and filename.startswith(root):
                position["file"] = filename[len(root):]
    canonical = lambda items: sorted(json.dumps(item, sort_keys=True) for item in items)
    if canonical(diagnostics) == canonical(KNOWN) and result.returncode == 1:
        print("WARNING: allowing exactly four recorded Staticcheck findings for Phase 2; "
              "remove this lease in #172.")
        raise SystemExit(0)
    if result.returncode != 0:
        fail("scanner exited with status " + str(result.returncode) +
             " and did not produce exactly the leased findings")
    fail("warning lease expired: findings changed or scan is clean; remove it for #172")


if __name__ == "__main__":
    main()
