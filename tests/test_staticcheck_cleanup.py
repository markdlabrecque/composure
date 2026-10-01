"""Structural checks for #172's four bounded Go cleanup edits.

Run: python3 -m unittest discover -s tests -p test_staticcheck_cleanup.py -v
These check declarations, not scanner output or runtime publication behavior.
Existing Go tests retain the behavioral and sentinel-identity coverage.
"""

from pathlib import Path
import re
import unittest


REPO = Path(__file__).resolve().parents[1]
# Tokenize only what these existing single-name declarations need. Comments
# disappear; literals stay whole, so neither can supply fake declaration tokens.
GO_TOKEN = re.compile(
    rb'//[^\n]*|/\*.*?\*/|"(?:\\.|[^"\\])*"|`[^`]*`|'
    rb"'(?:\\.|[^'\\])*'|[A-Za-z_][A-Za-z_0-9]*|[^\s]",
    re.DOTALL,
)


def go_tokens(source: bytes) -> list[bytes]:
    return [match.group() for match in GO_TOKEN.finditer(source)
            if not match.group().startswith((b"//", b"/*"))]


def declaration_prefixes(source: bytes, kind: bytes, name: bytes,
                         width: int = 2) -> list[list[bytes]]:
    tokens = go_tokens(source)
    return [tokens[index:index + width] for index in range(len(tokens) - 1)
            if tokens[index:index + 2] == [kind, name]]


class StaticcheckCleanupTests(unittest.TestCase):
    def test_unused_integer_function_declaration_is_removed(self) -> None:
        source = (REPO / "internal/config/config.go").read_bytes()
        self.assertEqual(declaration_prefixes(source, b"func", b"integer"), [],
                         "remove the unused integer declaration, not integerNumber")

    def test_unused_is_json_space_function_declaration_is_removed(self) -> None:
        source = (REPO / "internal/config/validate.go").read_bytes()
        self.assertEqual(declaration_prefixes(source, b"func", b"isJSONSpace"), [],
                         "remove the unused isJSONSpace declaration")

    def test_unused_saved_page_template_variable_declaration_is_removed(self) -> None:
        source = (REPO / "internal/web/web.go").read_bytes()
        self.assertEqual(declaration_prefixes(source, b"var", b"savedPageTemplate"), [],
                         "remove the unused savedPageTemplate declaration")

    def test_already_published_sentinel_keeps_api_and_lowercases_only_first_word(self) -> None:
        source = (REPO / "internal/content/content.go").read_bytes()
        self.assertEqual(
            declaration_prefixes(source, b"var", b"ErrAlreadyPublished", width=9),
            [[b"var", b"ErrAlreadyPublished", b"=", b"errors", b".", b"New", b"(",
              b'"page is already published"', b")"]],
            "keep the errors.New sentinel declaration and lowercase only Page",
        )


class CleanupStructureFixtureTests(unittest.TestCase):
    def test_comments_literals_and_longer_identifiers_are_not_declarations(self) -> None:
        source = br'''
// func integer(raw []byte) {}
/* var savedPageTemplate = nil */
var note = "func isJSONSpace(value byte) bool"
var raw = `var savedPageTemplate = nil`
var char = 'f'
func integerNumber(number string) {}
var savedPageTemplateBackup = nil
'''
        for kind, name in ((b"func", b"integer"), (b"func", b"isJSONSpace"),
                           (b"var", b"savedPageTemplate")):
            with self.subTest(name=name):
                self.assertEqual(declaration_prefixes(source, kind, name), [])
        self.assertEqual(declaration_prefixes(b"func /* comment */ integer() {}",
                                             b"func", b"integer"),
                         [[b"func", b"integer"]])
        self.assertEqual(declaration_prefixes(b"var\nsavedPageTemplate = nil",
                                             b"var", b"savedPageTemplate"),
                         [[b"var", b"savedPageTemplate"]])
