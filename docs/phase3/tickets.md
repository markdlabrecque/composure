# Phase 3 ticket draft: configurable content and the publishing journey

Status: Draft written 2026-09-30 while Phase 1 and Phase 2 are still in progress. Planning does not wait for them: this doc is the ticket source for the `analyze-ticket` skill, which splits or refines `P3-nn` entries in place. GitHub issues are opened from the settled layout and target the `phase-3` integration branch; issue numbers are recorded here when opened.

Source: [work plan Phase 3](../work_plan.md#phase-3-build-configurable-content-and-the-publishing-journey), [PRD](../prd.md) FR-03, FR-04 (basic editing), FR-05, FR-06, FR-08, FR-13, FR-14 and the editorial consistency requirements. The [phase 1 content contract](../phase1/content-contract.md) section 10 and the [storage and deployment ADR](../adr/0001-runtime-content-and-deployment.md) fix the rules these tickets must follow.

Outcome for the phase: an administrator defines content in the interface, and an editor creates, finds, previews and publishes it without code changes. All five launch types render through the barebones public theme.

Each ticket has one entry point (admin HTTP route, public route, package API or document), one storage concern and one observable result. Ticket order follows the plan's four tracer bullets. Tickets are sized so a small model agent can take each one in a single test-first pass.

Not in this phase: images and documents (phase 4), menus, redirects, path changes, Trash and permanent deletion (phase 5), configuration deploy and diff commands (phase 6). Where a phase 3 screen would need one of these, it shows the phase 1 `path_change_unsupported` style error or omits the control.

## Contracts (start of phase)

Contract tickets write one section file each under `docs/phase3/` and add one link to the matching index, so they can run in parallel. No code.

### P3-01a Content model contract: document shape and types

Create `docs/phase3/content-model.md` as an index linking section files under `docs/phase3/content-model/`. Write the `document` section: configuration format version 2, an ordered list of content types, each with `id`, `label`, plural label, `fields` and `groups`. Keep the phase 1 ID rules and reserved `title` and `path`. State the v1-to-v2 upgrade rule for an existing Page site: a v1 document is a valid v2 document with one type and no groups.

Depends on: Phase 2 closed.

### P3-01b Content model contract: field definition and groups

Write the `fields` section: every field property (`id`, `kind`, `label`, `help_text`, `required`, `order`, `group`, `multiple`, `validation`), which kinds allow `multiple`, and the group object (`id`, `label`, `order`). Groups only affect form layout; values stay keyed by field ID.

Depends on: P3-01a.

### P3-01c Content model contract: validation rules

Write the `validation` section: the closed set of validation properties per kind (text length bounds, numeric bounds and decimal places, choice options, relationship target types, link schemes) and the rule that unknown properties are errors.

Depends on: P3-01b.

### P3-01d Content model contract: allowed model changes

Write the `changes` section: which edits the field builder may apply in place (add field, add group, relabel, reorder, change help text, make optional, loosen validation) and which it rejects (remove populated field, change kind, change cardinality, tighten validation with non-compliant values). Reference the ADR; add the preflight report shape listing affected drafts and snapshots.

Depends on: P3-01c.

### P3-02a Field kind contract: text and email

Create `docs/phase3/field-kinds.md` as an index linking section files under `docs/phase3/field-kinds/`. Write the section for `short_text`, `long_text` and `email`: stored JSON value shape, empty-value rule, validation, form control and public rendering.

Depends on: P3-01c.

### P3-02b Field kind contract: rich text

Write the `rich_text` section: the stored value is sanitized HTML; the allowlist of elements and attributes; internal links store `composure:item/<id>` and resolve to the published path at render time or are dropped when the target is unpublished; no inline images.

Depends on: P3-02a, P3-04.

### P3-02c Field kind contract: link and phone

Write the `link` and `phone` sections: link stores `url` and optional `text`, allowed schemes `http`, `https`, `mailto`; phone stores the entered string with a bounded character allowlist and no country validation.

Depends on: P3-02a.

### P3-02d Field kind contract: numbers and yes or no

Write the `integer`, `decimal` and `boolean` sections: integers as JSON integers within int64; decimals as strings with a configured number of places; booleans with an explicit unset state when optional.

Depends on: P3-02a.

### P3-02e Field kind contract: choice fields

Write the `single_choice` and `multiple_choice` sections: options are `{value, label}` pairs in configuration; stored values are option values; removing an option that is in use is a rejected model change.

Depends on: P3-02a, P3-01d.

### P3-02f Field kind contract: date and time

Write the `datetime` section: stored as a UTC instant plus an IANA zone, the site default zone setting, rejection of nonexistent DST local times, explicit offset choice for ambiguous times, and rendering in the saved zone with an offset shown during transitions.

Depends on: P3-02a.

### P3-02g Field kind contract: address and coordinates

Write the `address` section: structured lines (`line1`, `line2`, `locality`, `region`, `postal_code`, `country`) with optional `latitude` and `longitude`, range checks and the rule that coordinates cannot be set without at least one address line.

Depends on: P3-02a.

### P3-02h Field kind contract: relationship

Write the `relationship` section: values are item IDs, allowed target types come from validation, the `item_references` index table from the phase 1 contract, the same-transaction maintenance rule and the public hidden-when-unpublished rule.

Depends on: P3-02a.

### P3-03 Snapshot history and restore contract

Write `docs/phase3/snapshots.md`: the history list shape (snapshot id, published time, actor, revision), the restore rule from the phase 1 contract (new draft revision, no snapshot, no public change), the expected-revision conflict rule for restore, and the failure report when a snapshot's values do not satisfy current validation.

Depends on: P3-01d.

### P3-04 Rich-text editor and sanitizer selection

Decision (2026-09-30): Tiptap as the editor, `bluemonday` as the Go sanitizer. Write `docs/phase3/rich-text.md`: the pinned Tiptap version and extension set, the one-time Node build that produces the vendored bundle under `internal/web/static/` (Go needs no Node at run time), the bundle hash, the pinned `bluemonday` version, the toolbar drawn in the admin's own style, and the security review checklist Mark signs off before P3-62 starts. Record the decision in `docs/adr/0002-rich-text.md`.

Depends on: Phase 2 closed. Blocks the rich text tickets only.

### P3-05 Public cache contract

Write `docs/phase3/cache.md`: the `site.generation` column, the transactional bump on every public-affecting change, the bounded in-process cache keyed by generation and path, size bound and eviction, the rule that an in-flight render started under an older generation cannot populate the new one, and that admin and preview responses are never cached.

Depends on: Phase 2 closed.

### P3-06 Content listing contract

Write `docs/phase3/listing.md`: the cross-type list columns (title, type, status, updated), the title search rule (case-insensitive substring), the type and status filters, how they combine, paging and the default sort.

Depends on: P3-01a.

### P3-07 Configuration mutability contract

Decision (2026-09-30): the environment variable `COMPOSURE_CONFIG_EDITABLE=1`, set by the deployment templates for development and staging only, turns the builder on. It is off unless set and cannot be changed from the admin. Write `docs/phase3/config-mutability.md`: the variable, the response when a production site receives a builder request, and the deployment template change. Production stays deploy-only per the ADR.

Depends on: P3-01d.

## Tracer bullet 1: define a type and publish its first item

An administrator adds a simple field to a type in the UI; an editor fills the generated form, saves, previews and publishes it. Required-field errors preserve input and model changes preserve existing content.

### P3-08 Configuration format v2 parser

Add `internal/config/v2.go` that parses and validates a P3-01 document with `short_text` and `long_text` only. Reject unknown properties, duplicate IDs and bad orders. A v1 document parses as v2 with one type. Fixtures under `docs/phase3/examples/`.

Depends on: P3-01b.

### P3-09 Store the format version on init and upgrade

Make `init` write format version 2 and accept v1 documents by upgrading them in memory. An existing v1 site opens and reports version 2 on the next config write, without changing stored items. Test with the phase 1 fixture database.

Depends on: P3-08.

### P3-10 Multiple content types in active configuration

Extend the active configuration reader so the application holds every type, not only `page`. Existing `page` routes keep working. Test that a two-type document loads and each type's fields are addressable by type ID.

Depends on: P3-08.

### P3-11 Type ID on items and snapshots

Add `internal/store/schema/0nn_types.sql` giving `items` and `snapshots` a `type_id` column defaulting to `page` for existing rows, plus `config_revision` on snapshots if phase 1 did not add it. Store methods accept a type ID. Test that the phase 1 fixture opens and every row reads `page`.

Depends on: P3-10.

### P3-12 Model change validator

Add `internal/config/changes.go` that compares an active document with a proposed one and returns the allowed change list or the rejection report from P3-01d. Test each allowed and rejected transformation with stored drafts and snapshots present.

Depends on: P3-01d, P3-11.

### P3-13 Configuration mutability guard

Add the P3-07 setting and a guard used by every builder route. Production sites return the P3-07 response. Test both states through a real request.

Depends on: P3-07, P3-10.

### P3-14 Builder: content types list screen

Add `GET /admin/model` in `internal/web/model.go` listing types with label and field count. Administrator only. Test the permission boundary and that a two-type site lists both.

Depends on: P3-13.

### P3-15 Builder: create a content type

Add `GET` and `POST /admin/model/new` creating a type with an ID, label and plural label, written to active configuration through P3-12. Test the duplicate-ID error and that the new type appears in P3-14.

Depends on: P3-14.

### P3-16 Builder: add a text field to a type

Add `GET` and `POST /admin/model/{type}/fields/new` for `short_text` and `long_text` with label, help text, required and order. Test that adding a field to a type with existing items leaves their stored values unchanged.

Depends on: P3-15.

### P3-17 Builder: edit field label, help text, required and order

Add `GET` and `POST /admin/model/{type}/fields/{field}` for in-place edits allowed by P3-01d. Making a field required with empty existing values is rejected with the preflight report. Test both.

Depends on: P3-16.

### P3-18 Builder: reorder and group fields

Add group creation and a field's group assignment on the P3-17 form. Test that the generated form (P3-20) renders groups in order with their fields.

Depends on: P3-17.

### P3-19 Generic item list per type

Replace the Page-only list with `GET /admin/content/{type}` in `internal/web/content.go`, rendered from configuration. The old Page route redirects. Test with two types.

Depends on: P3-11.

### P3-20 Generic form generation from configuration

Add `internal/web/form.go` that builds the create and edit form for any type from its fields and groups. Phase 1's Page form uses it. Test that a new type with two fields renders a usable form with no template change.

Depends on: P3-19, P3-18.

### P3-21 Generic create and save draft

Add `GET` and `POST /admin/content/{type}/new` and `POST /admin/content/{type}/{id}` using P3-20 and the phase 1 draft store with `type_id`. Keep the expected-revision conflict. Test create and save on a non-Page type.

Depends on: P3-20.

### P3-22 Preserve input on validation failure

On a failed save, re-render the form with every submitted value and per-field errors from the P2-44a partial. Test a required-field failure on a two-field form.

Depends on: P3-21.

### P3-23 Generic preview

Extend the phase 1 preview route to any type, rendering the draft through the P3-40 renderer. Session required. Test that a non-Page draft previews and an unauthenticated request is rejected.

Depends on: P3-21, P3-40.

### P3-24 Generic publish

Extend the phase 1 publish route to any type, writing a snapshot with `type_id` and `config_revision`. Test that the public route serves the new type's item after publish and the draft-only edit stays private.

Depends on: P3-23.

### P3-25 Bullet 1 journey test

Add one integration test: administrator creates a type and a field, editor creates, saves with an error, corrects, previews and publishes, then the administrator adds a second field and the item still renders. Add this to the phase gate.

Depends on: P3-24, P3-17.

## Tracer bullet 2: find, revise and restore an item

Search and combine filters, open a published item, publish a revision, restore the earlier snapshot as a draft and unpublish. Assert the public response, private preview and cache invalidation at every step.

### P3-26 Cross-type listing query

Add `internal/store/listing.go` with a query over `items` joined to the published state, filtered by title substring, type set and status set, with paging. Test each filter alone and combined.

Depends on: P3-06, P3-11.

### P3-27 Content listing screen

Add `GET /admin/content` using P3-26 with a search box and filter controls that keep their state in the query string. Editor or administrator. Test that a known item is found by partial title and narrowed by type and status.

Depends on: P3-26.

### P3-28 Snapshot history store query

Add a store method listing an item's snapshots newest first with id, published time, actor and revision. Test ordering and that draft saves add nothing.

Depends on: P3-03, P3-11.

### P3-29 Snapshot history screen

Add `GET /admin/content/{type}/{id}/history` listing snapshots and marking the currently published one. Test with three publishes.

Depends on: P3-28.

### P3-30 Inspect a past snapshot

Add `GET /admin/content/{type}/{id}/history/{snapshot}` rendering the snapshot read-only through the public renderer with a private banner. Session required, never cached. Test the permission boundary.

Depends on: P3-29, P3-40.

### P3-31 Restore a snapshot as a draft

Add `POST /admin/content/{type}/{id}/history/{snapshot}/restore` with the expected draft revision. Writes a new draft revision, no snapshot, no public change. Stale revision returns a conflict and changes nothing. Test both.

Depends on: P3-30.

### P3-32 Restore validation failure report

When a snapshot's values fail current validation, restore fails without mutating the draft and lists the affected fields and values. Test by tightening a text length after a publish.

Depends on: P3-31, P3-12.

### P3-33 Unpublish

Add `POST /admin/content/{type}/{id}/unpublish` setting the published pointer to NULL and keeping every snapshot and the route reservation. Public route returns 404. Test republish reuses the path without conflict.

Depends on: P3-24.

### P3-34 Site generation column

Add `site.generation` from P3-05 and a store helper that bumps it inside the caller's transaction. Test that publish, unpublish and a builder write each bump it once.

Depends on: P3-05, P3-33.

### P3-35 Bounded public HTML cache

Add `internal/render/cache.go`: a bounded map keyed by generation and path with eviction. Public routes read through it; admin and preview bypass it. Test hit, miss, eviction and that a generation bump makes old entries unreachable.

Depends on: P3-34.

### P3-36 Stale in-flight render guard

Record the generation before rendering and only store the result if it still matches after. Test with a render that publishes mid-flight; the new generation must not receive the old body.

Depends on: P3-35.

### P3-37 Two-editor conflict on save and restore

Add a test in `internal/web` where two sessions load the same draft; the second save and a later restore return conflicts with submitted work shown in the form. No new code unless the test finds a gap.

Depends on: P3-31, P3-22.

### P3-38 Bullet 2 journey test

Add one integration test covering search, open, revise, publish, restore, republish and unpublish, asserting the public body and cache behaviour after each step. Add it to the phase gate.

Depends on: P3-27, P3-36, P3-37.

## Tracer bullet 3: publish an Event linked to a Location

Define and use date and time, address, coordinates and relationship fields through the builder, editor and public theme.

### P3-39 Field kind registry

Add `internal/content/kinds.go`: a registry mapping each kind to its validator, form control and renderer, with `short_text` and `long_text` registered. Every later kind adds one file. Test that an unknown kind is a configuration error.

Depends on: P3-08.

### P3-40 Per-type public rendering

Add `internal/render/item.go` and a generic template that renders any type's fields in order using the P3-39 renderers, plus per-type template lookup by type ID with a fallback. Test with a two-type site.

Depends on: P3-39, P3-11.

### P3-41 Date and time kind: validation

Add `internal/content/datetime.go` implementing P3-02f with the site default zone. Test nonexistent DST times, ambiguous times without an offset, and a valid transition.

Depends on: P3-02f, P3-39.

### P3-42 Date and time kind: form control and rendering

Add the form control with local time, zone and offset choice, and the public renderer showing the saved zone. Test the ambiguous-time offset prompt through the form.

Depends on: P3-41, P3-20.

### P3-43 Site default timezone setting

Add a `timezone` site setting on the P2-43b settings screen, defaulting to UTC. Test that a new datetime field uses it.

Depends on: P3-41.

### P3-44 Address kind

Add `internal/content/address.go` with the P3-02g structure, coordinate range checks and the form control and renderer. Test out-of-range coordinates and coordinates without an address line.

Depends on: P3-02g, P3-39.

### P3-45 Relationship kind: validation and references index

Add `internal/content/relationship.go` and the `item_references` table. Draft and snapshot writes maintain the index in the same transaction. Test that a target type outside the allowed set is rejected and the index matches stored values after save and publish.

Depends on: P3-02h, P3-39, P3-24.

### P3-46 Relationship kind: form control

Add a target picker that lists published and draft items of the allowed types by title. Test that the chosen ID, not the title, is stored.

Depends on: P3-45, P3-20.

### P3-47 Relationship kind: public and preview rendering

Render a related item as a link to its published path only when it is published; hide it otherwise. Preview uses the previewed item's draft and only published related items. Test both states.

Depends on: P3-45, P3-40.

### P3-48 Builder support for the bullet 3 kinds

Extend the P3-16 field form with `datetime`, `address` and `relationship` and their validation properties from P3-01c. Test that each can be added through the builder.

Depends on: P3-16, P3-41, P3-44, P3-45.

### P3-49 Bullet 3 journey test

Add one integration test: administrator defines Event and Location, editor publishes a Location, creates an Event with a date across a DST gap, links the Location, previews with the Location unpublished and published, and publishes. Assert the public body each time. Add to the phase gate.

Depends on: P3-48, P3-42, P3-47.

## Tracer bullet 4: complete the launch field and type matrix

Add the remaining non-file fields in small groups, each with a builder-to-form-to-published-page check. Finish the five launch types.

### P3-50 Email kind

Add `internal/content/email.go` with a bounded format check, form control and renderer. Test acceptance and rejection.

Depends on: P3-02a, P3-39.

### P3-51 Link kind

Add `internal/content/link.go` with scheme allowlist, optional text, control and renderer. Test rejected schemes and rendering with and without text.

Depends on: P3-02c, P3-39.

### P3-52 Phone kind

Add `internal/content/phone.go` with the character allowlist, control and renderer.

Depends on: P3-02c, P3-39.

### P3-53 Integer kind

Add `internal/content/integer.go` with bounds, control and renderer. Test bounds and non-integer input.

Depends on: P3-02d, P3-39.

### P3-54 Decimal kind

Add `internal/content/decimal.go` storing strings with configured places. Test rounding rejection and bounds.

Depends on: P3-53.

### P3-55 Yes or no kind

Add `internal/content/boolean.go` with the unset state for optional fields. Test that an optional field can be unset and a required one cannot.

Depends on: P3-02d, P3-39.

### P3-56 Single choice kind

Add `internal/content/choice.go` for single choice with configured options, control and renderer. Test an unknown value.

Depends on: P3-02e, P3-39.

### P3-57 Multiple choice kind

Extend P3-56 to multiple values. Test duplicates and an empty selection on a required field.

Depends on: P3-56.

### P3-58 Multiple values for text, link, email and relationship

Implement `multiple` on the kinds P3-01b allows, with add and remove controls on the form and list rendering. Test that a single-value field rejects an array and a multiple field stores order.

Depends on: P3-51, P3-50, P3-46.

### P3-59 Builder support for the remaining kinds

Extend the field form with the P3-50 to P3-57 kinds, their validation properties and the `multiple` toggle. Test each through the builder.

Depends on: P3-48, P3-58.

### P3-60 Vendored rich-text editor bundle

Add the P3-04 bundle under `internal/web/static/` with its pinned hash checked by a test, served by the existing static route. No field yet.

Depends on: P3-04.

### P3-61 Rich text sanitizer

Add `internal/richtext/sanitize.go` wrapping the P3-04 Go sanitizer with the P3-02b allowlist. Add a bounded fuzz target. Test script, event handler and unknown attribute removal.

Depends on: P3-04, P3-02b.

### P3-62 Rich text kind

Add `internal/content/richtext.go`: sanitize on save, form control using P3-60, and a renderer that emits the stored HTML. Test that unsafe input never reaches storage.

Depends on: P3-60, P3-61, P3-39.

### P3-63 Rich text internal links

Resolve `composure:item/<id>` links to published paths at render time and drop links to unpublished targets. The editor offers a picker like P3-46. Test both states and that the stored value keeps the ID.

Depends on: P3-62, P3-47.

### P3-64 Rich text fuzz gate in CI

Run the P3-61 fuzz target in the required CI check with the phase 2 bound.

Depends on: P3-61.

### P3-65 Launch type definitions

Decision (2026-09-30): `init` keeps creating Page only. Add a format v2 example document under `docs/phase3/examples/launch-types.json` defining all five launch types (Event, Location, Blog post, News, Page) with the completed kinds. An operator passes it to the phase 1 `init` configuration option to create a site with all five; no new import command. Test that a site initialized from the example lists all five and each generates a form.

Depends on: P3-59, P3-62.

### P3-66 Per-type public templates

Add barebones templates for the five types under `internal/render/` with a type index page per type. Test that a fresh site renders one published item of each type.

Depends on: P3-65, P3-40.

### P3-67 Restore after a model change

Add a test: publish, add a field and a choice option, restore the older snapshot; the restored draft carries the old values, the new field is empty and no snapshot data is lost. Fix gaps found.

Depends on: P3-32, P3-65.

### P3-68 Browser journey for Phase 3

Extend the phase 1 Chrome journey: builder adds a field, editor uses the list search, edits with rich text and a relationship, previews, publishes, restores and unpublishes, at desktop and narrow widths with axe checks. Add to the phase gate.

Depends on: P3-66, P3-63, P3-38.

### P3-69 Phase 3 acceptance mapping

Write `docs/phase3/acceptance.md` mapping FR-03, FR-05, FR-06, FR-13, FR-14 and the editing part of FR-04 and FR-08 to tests, runs and revisions.

Depends on: P3-68, P3-67, P3-49, P3-25.

### P3-70 Phase 4 handoff

Write the Phase 4 ticket draft and remaining-P0 estimate, owning the Phase 4 section of `docs/work_plan.md`.

Depends on: P3-69.

## Exit checklist

- An editor completes the core publishing journey for all five types (P3-66, P3-68).
- A new administrator-defined type gets usable list, edit and preview screens without code (P3-25).
- Draft changes and restored snapshots stay private until publication; unpublishing removes public access (P3-31, P3-33, P3-38).
- Model changes preserve existing content and never silently drop snapshot data (P3-12, P3-17, P3-67).
- Cache invalidates on every public-affecting change and stale renders cannot populate a new generation (P3-36).
- Timezone, DST, address, coordinate and relationship rules pass (P3-49).
- Rich text is sanitized, fuzzed in CI and has no inline images (P3-61, P3-64).
- Search and filters combine across types (P3-27).

## Decisions made with Mark (2026-09-30)

1. Rich text: Tiptap editor, vendored bundle built once with Node, `bluemonday` sanitizer (P3-04).
2. Builder on/off: `COMPOSURE_CONFIG_EDITABLE=1` from the deployment templates; off unless set (P3-07).
3. Launch types: `init` creates Page only; the five types ship as an example document used with `init` (P3-65). Phase 7 initializes its release candidate from that example.
