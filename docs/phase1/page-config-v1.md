# Page configuration format v1

This document defines the minimal, closed configuration document for a new phase-1 site. It defines a Page type only; it is not a general content-model format.

## Document shape

The document is a JSON object with exactly these properties:

```json
{
  "format_version": 1,
  "content_types": [
    {
      "id": "page",
      "label": "Page",
      "fields": [
        {
          "id": "body",
          "kind": "long_text",
          "label": "Body",
          "help_text": "",
          "required": false,
          "order": 10
        }
      ]
    }
  ]
}
```

`format_version` identifies this document format. It is separate from SQLite `PRAGMA user_version` (schema version 1) and from `active_config.revision` (the revision of the active document). The active configuration's format version is also recorded as `site.config_format_version`, as described in the [phase 1 content contract](content-contract.md).

In v1, `content_types` contains exactly one object, the Page definition. Its `id` is exactly `page`; `label` is a string. `fields` is an array of field definitions. Each field object has exactly the six properties `id`, `kind`, `label`, `help_text`, `required`, and `order`: IDs and labels are strings, `kind` is a string, `help_text` is a string, `required` is a boolean, and `order` is a positive integer. Field orders are unique within the type.

Unknown properties at any level, unknown field kinds, additional content types, and invalid or duplicate IDs/references are errors; they are not ignored or discarded. There are no cross-field references in this minimal format. The only supported type ID is `page`. Supported kinds are `short_text` and `long_text`; both store string values keyed by field ID in `items.fields` and `snapshots.fields` (see the content contract). No other field properties, kinds, or content types are defined for phase 1.

## IDs and system metadata

Type and field IDs are stable lowercase ASCII identifiers: they start with a letter, contain only lowercase ASCII letters, digits, and underscores, and are at most 64 characters. IDs are immutable across sites; a site's labels, help text, required state, and order may differ in its definition without changing those IDs. `title` and `path` are reserved field IDs: they are system metadata, not configurable fields.

## Page fields and generated form

The default Page definition has one optional `long_text` field, `body`, labelled `Body`, with empty help text and order 10. The custom example retains the `body` ID, makes it required, labels it `Story`, and puts it at order 20; it adds optional `short_text` field `strapline` at order 10 with nonempty help text.

A generated Page form displays configured fields in ascending numeric `order` and enforces each field's `required` setting. Thus the custom form presents `strapline` before `body`, and requires `body` while leaving `strapline` optional. Field identity and stored-value keys remain the IDs, not labels or order.

## Change and deployment boundary

This is a new-site document format, not authorization for in-place production configuration edits. Production configuration is deploy-only; ordinary startup does not import a file. A deployment that tightens validation requires a preflight report of affected drafts and published items and must be rejected until values comply or an explicit reviewed migration supplies valid values. Removing a populated field or type, changing a field's type, or changing cardinality is rejected in v1. These preservation and deployment limits follow the [runtime content and deployment ADR](../adr/0001-runtime-content-and-deployment.md); this document does not define a deploy command or migration procedure.
