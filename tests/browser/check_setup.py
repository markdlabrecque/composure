"""Fail closed when the focused browser-test tools drift from their pins."""

import hashlib
import importlib.metadata
import os
from pathlib import Path
import sys


PLAYWRIGHT_VERSION = "1.58.0"
CHROMIUM_VERSION = "145.0.7632.6"
AXE_VERSION = "4.11.0"
AXE_SHA256 = "e9e5863c33a874f09bc01acd9234b7e3c871479f5eef8802fa582544465e6d01"
REPO = Path(__file__).resolve().parents[2]


def fail(message):
    print(message, file=sys.stderr)
    raise SystemExit(1)


def main():
    try:
        playwright_version = importlib.metadata.version("playwright")
    except importlib.metadata.PackageNotFoundError:
        fail("Python Playwright is missing; run scripts/install-browser-tests.")
    if playwright_version != PLAYWRIGHT_VERSION:
        fail(
            f"Expected Playwright {PLAYWRIGHT_VERSION}, found {playwright_version}; "
            "run scripts/install-browser-tests."
        )

    axe_path = Path(os.environ.get("COMPOSURE_AXE_PATH", REPO / "tests/browser/vendor/axe.min.js"))
    if not axe_path.is_file():
        fail("Pinned axe-core asset is missing; run scripts/install-browser-tests.")
    axe_bytes = axe_path.read_bytes()
    if hashlib.sha256(axe_bytes).hexdigest() != AXE_SHA256 or not axe_bytes.startswith(
        f"/*! axe v{AXE_VERSION}".encode()
    ):
        fail(
            f"axe-core does not match pinned version {AXE_VERSION}; "
            "run scripts/install-browser-tests."
        )

    try:
        from playwright.sync_api import sync_playwright

        with sync_playwright() as playwright:
            browser = playwright.chromium.launch(headless=True)
            try:
                chromium_version = browser.version
            finally:
                browser.close()
    except Exception as error:
        fail(
            "Pinned Chromium is missing or cannot start; run "
            f"scripts/install-browser-tests. Details: {error}"
        )
    if chromium_version != CHROMIUM_VERSION:
        fail(
            f"Expected bundled Chromium {CHROMIUM_VERSION}, found {chromium_version}; "
            "run scripts/install-browser-tests."
        )


if __name__ == "__main__":
    main()
