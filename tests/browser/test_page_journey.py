"""Critical Page journey through real Chromium controls at two widths."""

from html.parser import HTMLParser
import json
import os
from pathlib import Path
import unittest
from urllib.error import HTTPError
from urllib.request import urlopen

from test_lifecycle import BuiltAppTests, managed_site


class PublicHTML(HTMLParser):
    def __init__(self):
        super().__init__()
        self.stack = []
        self.title = ""
        self.heading = ""
        self.body = ""

    def handle_starttag(self, tag, attrs):
        self.stack.append((tag, dict(attrs)))

    def handle_endtag(self, tag):
        for index in range(len(self.stack) - 1, -1, -1):
            if self.stack[index][0] == tag:
                del self.stack[index:]
                break

    def handle_data(self, text):
        if any(tag == "title" for tag, _ in self.stack):
            self.title += text
        if any(tag == "h1" for tag, _ in self.stack):
            self.heading += text
        if any(attrs.get("data-field") == "body" for _, attrs in self.stack):
            self.body += text


class PageJourneyTests(BuiltAppTests):
    @classmethod
    def setUpClass(cls):
        try:
            from playwright.sync_api import sync_playwright, expect
        except ImportError as error:
            raise RuntimeError("Python Playwright is missing; install the pinned browser test requirements") from error
        cls.expect = staticmethod(expect)
        cls.axe = Path(os.environ.get("COMPOSURE_AXE_PATH", "tests/browser/vendor/axe.min.js"))
        if not cls.axe.is_file():
            raise RuntimeError("axe-core is missing; install the pinned axe asset for browser tests")
        super().setUpClass()
        cls.playwright = sync_playwright().start()
        try:
            cls.browser = cls.playwright.chromium.launch(headless=True)
        except Exception as error:
            cls.playwright.stop()
            cls.build.cleanup()
            raise RuntimeError("Pinned Chromium is missing or cannot start; run python -m playwright install chromium") from error

    @classmethod
    def tearDownClass(cls):
        try:
            cls.browser.close()
        finally:
            cls.playwright.stop()
            super().tearDownClass()

    def audit(self, page, width, state):
        page.add_script_tag(path=str(self.axe.resolve()))
        result = page.evaluate("async () => { const r = await axe.run(document); return {violations: r.violations, passes: r.passes.map(x => x.id), incomplete: r.incomplete.map(x => x.id)}; }")
        print(json.dumps({"width": width, "state": state, "axe": result}, sort_keys=True))
        self.assertEqual(result["violations"], [], f"axe violations at {width}px {state}")

    def keyboard_activate(self, page, control):
        self.expect(control).to_be_visible()
        self.expect(control).to_be_enabled()
        # Reach the control using only sequential keyboard navigation.
        for _ in range(30):
            page.keyboard.press("Tab")
            if control.evaluate("e => e === document.activeElement"):
                break
        self.expect(control).to_be_focused()
        self.assertTrue(control.evaluate("e => { const s = getComputedStyle(e); return e.matches(':focus-visible') && s.outlineStyle !== 'none' && parseFloat(s.outlineWidth) >= 1 && s.outlineColor !== 'rgba(0, 0, 0, 0)'; }"), "critical control needs a visible keyboard focus indicator")
        box = control.bounding_box()
        self.assertIsNotNone(box)
        self.assertGreater(box["width"], 0)
        self.assertGreater(box["height"], 0)
        self.assertGreaterEqual(box["x"], 0)
        self.assertLessEqual(box["x"] + box["width"], page.viewport_size["width"])
        with page.expect_navigation(wait_until="load"):
            page.keyboard.press("Enter")

    def assert_fields(self, page, title, path, body):
        for label, value in (("Title", title), ("Path", path), ("Body", body)):
            field = page.get_by_label(label, exact=True)
            self.expect(field).to_be_visible()
            self.expect(field).to_be_enabled()
            self.expect(field).to_have_value(value)
            box = field.bounding_box()
            self.assertGreater(box["width"], 0)
            self.assertGreaterEqual(box["x"], 0)
            self.assertLessEqual(box["x"] + box["width"], page.viewport_size["width"])

    def public(self, url, status, title=None, body=None, absent=()):
        # urllib is independent of browser cookies, page DOM and request context.
        try:
            response = urlopen(url, timeout=5)
        except HTTPError as error:
            response = error
        with response:
            self.assertEqual(response.status, status)
            text = response.read().decode("utf-8")
        if title is not None:
            document = PublicHTML()
            document.feed(text)
            self.assertEqual(document.title, title)
            self.assertEqual(document.heading, title)
            self.assertEqual(document.body, body)
        for value in absent:
            self.assertNotIn(value, text)

    def journey(self, width):
        title_a, body_a = "Publication Alpha title", "Alpha saved body: violet river"
        title_b, body_b = "Publication Beta title", "Beta saved body: copper mountain"
        path = "/browser-journey"
        with managed_site(self.binary) as site:
            self.retain_cleanup(site)
            context = self.browser.new_context(viewport={"width": width, "height": 900})
            try:
                page = context.new_page()
                page.set_default_timeout(5000)
                page.goto(site.url + "/admin/pages")
                empty_state = page.get_by_text("No Pages yet.", exact=True)
                if os.environ.get("COMPOSURE_BROWSER_FAILURE_PROBE") == "1":
                    self.expect(empty_state).not_to_be_visible()
                else:
                    self.expect(empty_state).to_be_visible()
                self.keyboard_activate(page, page.get_by_role("link", name="New Page", exact=True))
                self.assert_fields(page, "", "", "")
                self.audit(page, width, "create")
                page.get_by_label("Path", exact=True).fill(path)
                page.get_by_label("Body", exact=True).fill("Retained nonempty validation body")
                # Native required validation would prevent the requested SERVER
                # error. Disable only that browser check; submit the real button.
                page.get_by_role("button", name="Create Page", exact=True).evaluate("button => button.form.noValidate = true")
                with page.expect_response(lambda r: r.request.method == "POST" and r.url.endswith("/admin/pages")) as submitted:
                    self.keyboard_activate(page, page.get_by_role("button", name="Create Page", exact=True))
                self.assertEqual(submitted.value.status, 422)
                self.expect(page.locator('[data-error-code="required"]')).to_be_visible()
                self.assert_fields(page, "", path, "Retained nonempty validation body")
                self.audit(page, width, "error")
                page.get_by_label("Title", exact=True).fill("Initial draft title")
                self.keyboard_activate(page, page.get_by_role("button", name="Create Page", exact=True))
                page.wait_for_url("**/edit")
                edit_url = page.url
                self.assert_fields(page, "Initial draft title", path, "Retained nonempty validation body")
                self.audit(page, width, "edit-created")
                page.get_by_label("Title", exact=True).fill(title_a)
                page.get_by_label("Body", exact=True).fill(body_a)
                self.keyboard_activate(page, page.get_by_role("button", name="Save changes", exact=True))
                self.assert_fields(page, title_a, path, body_a)
                self.audit(page, width, "edit-A")
                self.keyboard_activate(page, page.get_by_role("link", name="Preview saved draft", exact=True))
                page.wait_for_url("**/preview")
                self.expect(page.get_by_role("heading", level=1)).to_have_text(title_a)
                self.expect(page.get_by_text(body_a, exact=True)).to_be_visible()
                self.expect(page.get_by_role("status")).to_contain_text("Preview")
                self.audit(page, width, "preview-A")
                self.public(site.url + path, 404, absent=(title_a, body_a))
                page.go_back()
                self.expect(page).to_have_url(edit_url)
                self.keyboard_activate(page, page.get_by_role("button", name="Publish saved draft", exact=True))
                self.public(site.url + path, 200, title_a, body_a, (title_b, body_b))
                page.goto(site.url + path)
                self.audit(page, width, "public-A")
                page.go_back()
                self.expect(page).to_have_url(edit_url + "?notice=published")
                page.get_by_label("Title", exact=True).fill(title_b)
                page.get_by_label("Body", exact=True).fill(body_b)
                self.keyboard_activate(page, page.get_by_role("button", name="Save changes", exact=True))
                self.assert_fields(page, title_b, path, body_b)
                self.audit(page, width, "edit-B")
                self.keyboard_activate(page, page.get_by_role("link", name="Preview saved draft", exact=True))
                page.wait_for_url("**/preview")
                self.expect(page.get_by_role("heading", level=1)).to_have_text(title_b)
                self.expect(page.get_by_text(body_b, exact=True)).to_be_visible()
                self.expect(page.get_by_text(body_a, exact=True)).to_have_count(0)
                self.audit(page, width, "preview-B")
                self.public(site.url + path, 200, title_a, body_a, (title_b, body_b))
                page.go_back()
                self.keyboard_activate(page, page.get_by_role("button", name="Publish saved draft", exact=True))
                self.public(site.url + path, 200, title_b, body_b, (title_a, body_a))
                page.goto(site.url + path)
                self.expect(page.get_by_role("heading", level=1)).to_have_text(title_b)
                self.expect(page.get_by_text(body_b, exact=True)).to_be_visible()
                self.audit(page, width, "public-B")
            finally:
                context.close()

    def test_desktop_page_journey(self):
        self.journey(1280)

    def test_narrow_page_journey(self):
        self.journey(390)


if __name__ == "__main__":
    unittest.main()
