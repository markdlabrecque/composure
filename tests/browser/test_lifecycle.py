"""Real-process contracts for the browser test site's lifetime."""

from pathlib import Path
import os
import shutil
import sqlite3
import subprocess
import tempfile
import unittest
from urllib.request import urlopen

from support import managed_site

REPO = Path(__file__).resolve().parents[2]


class BuiltAppTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.build = tempfile.TemporaryDirectory(prefix="composure-browser-build-")
        cls.binary = Path(cls.build.name) / "composure"
        subprocess.run(["go", "build", "-o", str(cls.binary), "./cmd/composure"],
                       cwd=REPO, env={**os.environ, "CGO_ENABLED": "0"}, check=True)

    @classmethod
    def tearDownClass(cls):
        cls.build.cleanup()

    def retain_cleanup(self, site):
        # Even the intentionally failing stub must leave no process/site behind.
        def cleanup():
            if site.process.poll() is None:
                site.process.terminate()
                try:
                    site.process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    site.process.kill()
                    site.process.wait(timeout=5)
            if site.process.stdout:
                site.process.stdout.close()
            shutil.rmtree(site.root, ignore_errors=True)
        self.addCleanup(cleanup)


class SiteLifetimeTests(BuiltAppTests):
    def assert_fresh(self, site):
        self.assertRegex(site.url, r"^http://127\.0\.0\.1:[1-9][0-9]*$")
        self.assertIsNone(site.process.poll())
        with sqlite3.connect(site.root / "composure.db") as db:
            self.assertEqual(db.execute("SELECT count(*) FROM items").fetchone()[0], 0)
            self.assertEqual(db.execute("PRAGMA application_id").fetchone()[0], 0x434D5053)
        with urlopen(site.url + "/healthz", timeout=1) as response:
            self.assertEqual(response.read().strip(), b"ok")

    def assert_cleaned(self, site):
        self.assertIsNotNone(site.process.poll(), "managed_site leaked the real server process")
        self.assertFalse(site.root.exists(), "managed_site left its temporary SQLite site")

    def test_success_reaps_server_and_removes_site(self):
        with managed_site(self.binary) as site:
            self.retain_cleanup(site)
            self.assert_fresh(site)
        self.assert_cleaned(site)

    def test_failed_assertion_reaps_server_and_removes_site(self):
        with self.assertRaisesRegex(AssertionError, "injected journey failure"):
            with managed_site(self.binary) as site:
                self.retain_cleanup(site)
                self.assert_fresh(site)
                raise AssertionError("injected journey failure")
        self.assert_cleaned(site)

    def test_each_viewport_gets_independent_empty_sqlite_site(self):
        with managed_site(self.binary) as first, managed_site(self.binary) as second:
            self.retain_cleanup(first)
            self.retain_cleanup(second)
            self.assert_fresh(first)
            self.assert_fresh(second)
            self.assertNotEqual(first.root, second.root)
            self.assertNotEqual(first.url, second.url)
            with sqlite3.connect(first.root / "composure.db") as a, sqlite3.connect(second.root / "composure.db") as b:
                self.assertNotEqual(a.execute("SELECT site_id FROM site").fetchone(),
                                    b.execute("SELECT site_id FROM site").fetchone())


if __name__ == "__main__":
    unittest.main()
