# Phase 1 Page configuration validation and export

This contract defines validation, serialization, and new-site use of the version 1 Page configuration document in [page-config-v1.md](page-config-v1.md). It supplements the storage and initialization rules in the [phase 1 content contract](content-contract.md). It does not define in-place configuration deployment, a site archive, or reference-integrity checks.

## End-to-end path

The initial round trip is: read `active_config.document` from an initialized site, validate the Page definition, serialize it to a versioned file, validate that file without opening a site, then pass it to `init` for a second site. Export and validation do not change either site's data. The configuration file is a definition only; it is not a copy of a site.

The file contains only `format_version` and `content_types`, with the Page type's ID, label, and field definitions. It never contains `active_config.revision`, `site.config_format_version`, SQLite schema version, a site ID, applied metadata, Page values, snapshots, accounts, audit records, credentials, deployment secrets, media, or an export manifest. Those three version markers remain distinct as described in the [content contract](content-contract.md#4-site-directory-and-storage).

## Validation

`config validate --file FILE` reads one UTF-8 JSON document and checks it without opening a site or writing files. JSON object property order and whitespace do not affect validity. The accepted shape is closed and follows [Page configuration format v1](page-config-v1.md): the root has exactly `format_version` and `content_types`; the format version is the JSON integer `1`; `content_types` has exactly one object with ID `page`; and each field has exactly `id`, `kind`, `label`, `help_text`, `required`, and `order`.

The type and field rules are:

- Field IDs match `[a-z][a-z0-9_]{0,63}` using ASCII only. `title` and `path` are reserved. Field IDs and positive integer `order` values are unique within the Page type.
- `kind` is exactly `short_text` or `long_text`. `id`, `kind`, `label`, and `help_text` are JSON strings; `required` is a JSON boolean; and `order` is a positive JSON integer. A number written with a fraction or exponent, such as `10.0` or `1e1`, is not an integer for this format. Empty labels and help text are allowed.
- Unknown properties at any object level, additional content types, unknown kinds, malformed IDs, duplicate IDs, duplicate orders, and missing or wrongly typed properties are errors. Property names are compared after JSON escape decoding, so `"id"` and `"\u0069d"` are the same name and cannot both occur in one object.
- A field list may be empty. The format does not add cross-field references or default values. Positive `order` values have no additional format-level upper bound and must be preserved as exact integers rather than converted through floating point. An empty field array serializes as `[]` with no interior whitespace.

The validator stops at the first error. It rejects malformed JSON and duplicate object property names before shape validation. For valid JSON, it checks the `format_version` property first: a missing or non-integer value is invalid input; any integer other than 1 is an unsupported format version. It then walks the schema from the root down, visiting arrays in input order and defined properties in the order shown above. For each object it checks required properties in schema order, then unknown properties in input order, then the present values in schema order. Duplicate IDs and orders are checked after each field passes its individual checks, in field-array order. This gives diagnostics a stable first-error rule.

JSON paths use `$` for the root, dot notation only for property names matching `[A-Za-z_][A-Za-z0-9_]*`, and zero-based brackets for array indexes. Other property names use bracket notation containing a JSON-escaped string, for example `$["bad.key"]` or `$["bad.key\nname"]`. A missing-property error points to the path where that property belongs. A syntax error uses path `$` and gives a zero-based byte offset from the start of the input in the message. Duplicate-property errors point to the duplicated member's path.

Error paths and messages must stay on one physical line. Render dynamic property names and string values as JSON string literals, escaping quotes, backslashes, and controls; never insert raw input text or parser excerpts. For example, a decoded property name containing a line feed is reported on one line:

```text
composure config validate: unknown_property at $["bad.key\nname"]: unknown property "bad.key\nname"
```

### Error and exit contract

Errors are one line on stderr with the command prefix from the [CLI conventions](content-contract.md#8-versions-startup-and-initialization): `composure <command>: <class> at <JSON-path>: <message>`. Invalid commands produce no success text on stdout. The standalone validator prints `Valid Page configuration format v1.` on stdout and exits 0 on success.

| Error class | Example path | Exit | Meaning |
| --- | --- | ---: | --- |
| `json_syntax` | `$` | 3 | Malformed JSON, invalid UTF-8, a lone escaped surrogate, or trailing non-whitespace. The diagnostic includes a zero-based byte offset from the input start. |
| `duplicate_property` | `$.content_types[0].fields[0].id` | 3 | The same decoded property name appears twice in one object. |
| `missing_property` | `$.content_types[0].fields[0].required` | 3 | A required property is absent. |
| `unknown_property` | `$.content_types[0].fields[0].extra` | 3 | A property is not part of the closed format. |
| `invalid_type` | `$` or a member path | 3 | The root or a member has the wrong JSON type, including a boolean or fractional/exponent number where an integer is required. |
| `invalid_id` | `$.content_types[0].fields[0].id` | 3 | A field ID does not match the ASCII ID grammar. |
| `reserved_id` | `$.content_types[0].fields[0].id` | 3 | A field ID is `title` or `path`. |
| `duplicate_id` | `$.content_types[0].fields[1].id` | 3 | A field ID is repeated. |
| `duplicate_order` | `$.content_types[0].fields[1].order` | 3 | An order value is repeated. |
| `invalid_value` | `$.content_types[0].fields[0].order` | 3 | A correctly typed value violates a format rule, such as an order of zero or unsupported field kind. |
| `unsupported_content_type` | `$.content_types[0].id` or `$.content_types[1]` | 3 | The type ID is not `page`, or an additional type is present. |
| `unsupported_version` | `$.format_version` | 4 | An integer format version other than 1, whether older or newer. |
| `io_error` | n/a | 1 | Input cannot be read or output cannot be installed. |
| `usage_error` | n/a | 2 | Unknown command or flag, or missing/invalid command arguments. |

Unsupported versions are classified before checking the rest of the document, so version 0 and version 2 both exit 4 even if a later property would also be invalid. A missing `format_version` is `missing_property`; a wrong type there is `invalid_type`; both exit 3. A valid JSON value with the wrong root shape is `invalid_type` at `$`. The phase 1 CLI uses exit codes 0 through 4 as documented in the [content contract](content-contract.md#cli-conventions).

Malformed JSON and escaped-surrogate examples are inline here and have no checked-in `.json` fixtures, because the repository's document checks parse checked-in JSON examples:

```json
{"format_version": 1, "content_types": [
```

This reports `json_syntax` at `$` with a zero-based byte offset and exits 3. A duplicate property is syntactically parseable JSON but still rejected as `duplicate_property`. A valid pair is a high surrogate U+D800–U+DBFF immediately followed by a low surrogate U+DC00–U+DFFF; it decodes to one Unicode scalar. For example, `{"label":"\uD83D\uDE00"}` represents `{"label":"😀"}`, and the serializer writes that scalar as UTF-8 bytes `F0 9F 98 80`. An isolated or reversed surrogate is rejected as `json_syntax` at `$`, with the offset of the first offending backslash in the error message. For `{"label":"\uD800"}`, that is zero-based byte offset 10.

## Page values and `init --example`

Configuration validation checks definitions, not Page content values. When `init --example` is requested, initialization validates the fixed seed in [seed-page.json](examples/seed-page.json) against the selected Page definition before any filesystem or database writes. Every supplied value must name a configured field and be a string. A required field must have a present, nonempty string; values are not trimmed for this check. Values must also satisfy their field-kind limits. No value is invented, truncated, or silently omitted. If validation fails, the whole initialization fails with a path-specific validation error and leaves the destination unchanged; when apply created a directory or temporary database, normal init cleanup removes those artifacts.

`short_text` values are limited to 255 Unicode code points, not 255 UTF-8 bytes. Count the string's decoded Unicode code points without Unicode normalization; reject a value over the limit and do not truncate it. For example, 255 `é` code points are within the limit even though their UTF-8 representation is 510 bytes; 256 are rejected. Existing `long_text` Page body rules, including the 100,000-character limit and newline normalization, remain as specified in the [content contract](content-contract.md#5-field-and-metadata-rules).

The current fixed seed supplies only the `body` value. With the custom #19 definition it validates because `body` is present and the optional `strapline` is not required. If a selected definition makes `strapline` required, `init --example` fails with class `required_value_missing` at `$.fields.strapline` (exit 3) before any writes. The ordinary second-site round trip initializes without `--example` and starts with no editorial rows. The seed is never copied from another site. A seed value with the wrong type, an unconfigured field ID, or an overlong value fails with class `field_value_invalid` at `$.fields.<id>` (exit 3). No plan/success line is printed for invalid configuration or seed data.

## Deterministic export

`composure config export --site DIR --out FILE` reads and validates the document stored in SQLite at `active_config.document`; it never exports a compiled-in default or reads an unrelated source file. Export does not mutate active configuration, content, snapshots, site files, or schema. An invalid active document is rejected with the same validation class, path, and exit mapping used by `config validate` (unsupported format version exits 4).

The exported bytes are deterministic for an unchanged definition:

- UTF-8 without a byte-order mark, with two-space indentation and exactly one final LF newline.
- Each object begins with `{` then LF and ends with LF then `}` at the object's indentation. Each member is on its own line at two spaces deeper; the quoted key is followed by the exact bytes `: ` (colon then one ASCII space) before its value. A nonempty array begins with `[` then LF; each element is on its own line two spaces deeper than the array property, commas are the exact bytes `,` then LF between elements, and the array ends with LF then `]` at the property's indentation. An empty array is exactly `[]`, with no inner newline or spaces. There are no trailing commas or spaces before commas.
- Object properties use the order established in [page-config-v1.md](page-config-v1.md): root `format_version`, then `content_types`; type `id`, `label`, then `fields`; field `id`, `kind`, `label`, `help_text`, `required`, then `order`.
- `content_types` are sorted by ID and each type's fields by ascending numeric `order`, then ID. Duplicate orders are invalid, but the ID tie-breaker remains part of the canonical rule.
- Numbers are written as plain base-10 integers without a decimal point or exponent. String values retain their Unicode scalar values and are not normalized. Input strings must contain only Unicode scalar values: a valid escaped high/low surrogate pair decodes to one scalar; an isolated high or low surrogate is rejected as `json_syntax` at `$`, exit 3, with the zero-based byte offset of the offending escape in the message. UTF-8 characters are emitted directly; `"` becomes `\"`, `\` becomes `\\`, backspace/form-feed/newline/carriage-return/tab use `\b`, `\f`, `\n`, `\r`, `\t`, and other U+0000–U+001F controls use lowercase `\u00xx`. `<`, `>`, `&`, and `/` are not HTML-escaped; U+2028 and U+2029 use `\u2028` and `\u2029` escapes.
- The file has no timestamps, site ID, active revision, applied metadata, content, credentials, site archive data, or other environment-specific values.

For an empty field array, the canonical member bytes are exactly:

```json
      "fields": []
```

For example, this valid input has fields in reverse order, reordered object properties, and escaped Unicode:

```json
{"content_types":[{"fields":[{"order":20,"required":false,"help_text":"A \"quote\" and \\ slash\n<&/","label":"Caf\u00e9","kind":"long_text","id":"body"},{"order":10,"required":false,"help_text":"","label":"Intro","kind":"short_text","id":"intro"}],"label":"Page","id":"page"}],"format_version":1}
```

It serializes as these UTF-8 bytes; the block ends with the single required LF:

```json
{
  "format_version": 1,
  "content_types": [
    {
      "id": "page",
      "label": "Page",
      "fields": [
        {
          "id": "intro",
          "kind": "short_text",
          "label": "Intro",
          "help_text": "",
          "required": false,
          "order": 10
        },
        {
          "id": "body",
          "kind": "long_text",
          "label": "Café",
          "help_text": "A \"quote\" and \\ slash\n<&/",
          "required": false,
          "order": 20
        }
      ]
    }
  ]
}
```

Export writes a uniquely named temporary file in the output file's parent directory, writes and flushes the complete serialized document, closes it, then renames it over `FILE`. It prints success only after the rename succeeds. The parent directory must already exist. If `FILE` exists, a successful export replaces it. The target must not resolve to the site's `composure.db`, `composure.db-wal`, or `composure.db-shm` path. Any failure before a successful rename returns exit 1, removes the temporary file where possible, and preserves the previous `FILE`; if there was no previous file, no partial output is left at that name. Validation never creates or replaces an output file.

## Initialization and production boundary

`composure init --site DIR --config FILE [--example] [--apply]` runs the same validator before creating anything. Without `--apply`, it validates the configuration and any requested example seed, prints the plan, and changes nothing. With `--apply`, it stores the validated definition in the new site's `active_config.document` at revision 1 and keeps all type/field IDs and settings. It never copies the source site's content. Nonempty destinations are refused; there is no merge, overwrite, or existing-site update mode. The ordinary second-site journey omits `--example`.

This new-site setup path does not change production configuration. Production configuration is deploy-only under the [runtime and deployment ADR](../adr/0001-runtime-content-and-deployment.md). Drift checks, expected-active-version checks, model-change preflight, and migrations belong to deployment in phase 6, not to this validator or exporter. Phase 1 has no relationship fields; cross-record and file-reference integrity is outside this contract and belongs to phase 6 full recovery.

## Fixture matrix

Every file in [`examples/config-invalid/`](examples/config-invalid/) is syntactically parseable JSON and has one deliberate failure. The old and new version fixtures are separate because each must independently prove the exit-4 boundary. The matrix is also recorded with the validation evidence in [the ticket report](../reports/2026-09-29-ticket-20.md).

| Fixture | Deliberate failure | Expected class | JSON path | Exit |
| --- | --- | --- | --- | ---: |
| `duplicate-field-id.json` | Second field reuses `body` | `duplicate_id` | `$.content_types[0].fields[1].id` | 3 |
| `duplicate-field-order.json` | Second field reuses order 10 | `duplicate_order` | `$.content_types[0].fields[1].order` | 3 |
| `reserved-field-id.json` | Field ID is `title` | `reserved_id` | `$.content_types[0].fields[0].id` | 3 |
| `unsupported-version-old.json` | Format version is 0 | `unsupported_version` | `$.format_version` | 4 |
| `unsupported-version-new.json` | Format version is 2 | `unsupported_version` | `$.format_version` | 4 |
| `unknown-property.json` | Field has unrecognized `extra` property | `unknown_property` | `$.content_types[0].fields[0].extra` | 3 |
| `unsupported-field-kind.json` | Kind is `rich_text` | `invalid_value` | `$.content_types[0].fields[0].kind` | 3 |
| `missing-required-property.json` | Field omits `required` | `missing_property` | `$.content_types[0].fields[0].required` | 3 |
| `invalid-order-zero.json` | Order is zero | `invalid_value` | `$.content_types[0].fields[0].order` | 3 |
| `invalid-required-type.json` | `required` is the string `false` | `invalid_type` | `$.content_types[0].fields[0].required` | 3 |
| `invalid-order-boolean.json` | Order is JSON `true` | `invalid_type` | `$.content_types[0].fields[0].order` | 3 |
| `invalid-order-fraction.json` | Order is 1.5 | `invalid_type` | `$.content_types[0].fields[0].order` | 3 |
| `invalid-field-id.json` | Field ID contains uppercase ASCII | `invalid_id` | `$.content_types[0].fields[0].id` | 3 |
| `duplicate-json-property.json` | Field object repeats decoded `id` property | `duplicate_property` | `$.content_types[0].fields[0].id` | 3 |

The malformed JSON case is inline above rather than a fixture. Cross-record, relationship, and file-reference cases are deliberately absent because the v1 definition has no such properties.
