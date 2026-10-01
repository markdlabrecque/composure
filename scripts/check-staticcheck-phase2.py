#!/usr/bin/env python3
"""Run the pinned Phase 2 Staticcheck scan without diagnostic exceptions."""

from pathlib import Path
import subprocess
import sys


def fail(message):
    print("Staticcheck failed closed: " + message, file=sys.stderr)
    raise SystemExit(1)


def main():
    if len(sys.argv) != 2:
        fail("usage: check-staticcheck-phase2.py SCANNER")
    scanner = Path(sys.argv[1])
    if not scanner.is_absolute() or scanner.name != "staticcheck":
        fail("expected the freshly installed absolute staticcheck path")
    try:
        result = subprocess.run([str(scanner), "-f", "json", "./..."],
                                capture_output=True, check=False)
    except OSError as error:
        fail("could not execute scanner: " + str(error))
    if result.returncode != 0:
        fail("scanner exited with status " + str(result.returncode))
    if result.stdout:
        fail("scanner wrote diagnostics or other output to stdout")
    if result.stderr:
        fail("scanner wrote diagnostics or other output to stderr")
    raise SystemExit(0)


if __name__ == "__main__":
    main()
