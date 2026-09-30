# Phase 3 work plan: parallel lanes

Status: Draft written 2026-09-30 from `docs/phase3/tickets.md`. Issue numbers are added when the tickets are opened after Phase 2 closes. Read `docs/phase2/work_plan.md` for the lane and wave definitions; the same rules apply.

## Rules that keep lanes from colliding

1. **One field kind, one file.** Every kind lives in its own `internal/content/<kind>.go` and registers itself with the P3-39 registry. Kind tickets never edit each other's files.
2. **One SQL file per change.** Continue the numbered `internal/store/schema/*.sql` scheme from Phase 2.
3. **Contract sections are separate files.** P3-01a and P3-02a create the two indexes; every other contract ticket adds one section file and one index link.
4. **Builder and editor are different files.** Builder routes live in `internal/web/model.go`; editorial routes in `internal/web/content.go`, `form.go` and `history.go`. Only route registrations touch `web.go`.
5. **Rich text is gated on P3-04.** Tiptap plus `bluemonday`, decided 2026-09-30. No rich text ticket starts until Mark signs the P3-04 review checklist.
6. **Rebase on `develop` before review.** A green branch does not prove integration with a parallel lane.

## Lanes

| Lane | Files owned | Tickets in order |
| --- | --- | --- |
| Content model contract | `docs/phase3/content-model.md` and `content-model/` | P3-01a → P3-01b → P3-01c → P3-01d |
| Field kind contracts | `docs/phase3/field-kinds.md` and `field-kinds/` (one file per ticket) | P3-02a, then P3-02b to P3-02h in parallel |
| Snapshot, cache, listing, mutability contracts | `docs/phase3/snapshots.md`, `cache.md`, `listing.md`, `config-mutability.md` | P3-03, P3-05, P3-06, P3-07 in parallel |
| Rich text decision | `docs/phase3/rich-text.md`, `docs/adr/0002-rich-text.md` | P3-04 |
| Config package | `internal/config/v2.go`, `changes.go` | P3-08 → P3-09 → P3-10 → P3-12 |
| Store: types and listing | `internal/store/schema/*.sql`, `listing.go`, `snapshots.go` | P3-11 → P3-26 → P3-28 |
| Store: generation and references | `internal/store/schema/*.sql`, `generation.go`, `references.go` | P3-34 → P3-45 (store part) |
| Content kinds | `internal/content/kinds.go` and one file per kind | P3-39, then each kind ticket in parallel |
| Render | `internal/render/item.go`, `cache.go`, per-type templates | P3-40 → P3-35 → P3-36 → P3-66 |
| Rich text package | `internal/richtext/`, `internal/web/static/` | P3-60 → P3-61 → P3-62 → P3-63 → P3-64 |
| Web builder | `internal/web/model.go`, `admin_model*.html` | P3-13 → P3-14 → P3-15 → P3-16 → P3-17 → P3-18 → P3-48 → P3-59 |
| Web editorial | `internal/web/content.go`, `form.go`, `admin_content*.html` | P3-19 → P3-20 → P3-21 → P3-22 → P3-23 → P3-24 → P3-33 → P3-27 |
| Web history | `internal/web/history.go`, `admin_history*.html` | P3-29 → P3-30 → P3-31 → P3-32 |
| Web field controls | `internal/web/controls/` (one file per kind) | P3-42, P3-46, P3-58 |
| Settings | `internal/web/settings.go` | P3-43 |
| Launch types | `docs/phase3/examples/launch-types.json` | P3-65 |
| Journey and gate tests | `internal/web/*_journey_test.go`, browser journey, CI | P3-25 → P3-37 → P3-38 → P3-49 → P3-67 → P3-68 |
| Phase docs | `docs/phase3/acceptance.md`, `docs/work_plan.md` Phase 4 section | P3-69 → P3-70 |

## Waves

| Wave | Tickets ready |
| --- | --- |
| 1 | P3-01a, P3-04, P3-05 |
| 2 | P3-01b, P3-06 |
| 3 | P3-01c, P3-08 |
| 4 | P3-01d, P3-02a, P3-09, P3-10, P3-39 |
| 5 | P3-02b to P3-02h, P3-03, P3-07, P3-11, P3-40 |
| 6 | P3-12, P3-13, P3-19, P3-26, P3-28, P3-41, P3-44, P3-50 to P3-56, P3-60, P3-61 |
| 7 | P3-14, P3-20, P3-27, P3-29, P3-43, P3-54, P3-57, P3-62, P3-64 |
| 8 | P3-15, P3-21, P3-30, P3-42 |
| 9 | P3-16, P3-22, P3-23, P3-31 |
| 10 | P3-17, P3-24, P3-32 |
| 11 | P3-18, P3-25, P3-33, P3-45 |
| 12 | P3-34, P3-46, P3-47, P3-48 |
| 13 | P3-35, P3-37, P3-58, P3-63 |
| 14 | P3-36, P3-49, P3-59 |
| 15 | P3-38, P3-65 |
| 16 | P3-66, P3-67 |
| 17 | P3-68 |
| 18 | P3-69 |
| 19 | P3-70 |

Wave placement counts stated dependencies and lane order. A ticket may start as soon as its own prerequisites are merged and closed.

## Critical path

P3-01a → P3-01b → P3-08 → P3-10 → P3-11 → P3-19 → P3-20 → P3-21 → P3-22 → P3-23 → P3-24 → P3-33 → P3-34 → P3-35 → P3-36 → P3-38 → P3-65 → P3-66 → P3-68 → P3-69 → P3-70

Twenty-one tickets; nineteen waves. Peak concurrency is wave 6 with about sixteen ready tickets. Four to six agents keep most waves busy.

## Model routing

Default every ticket to `Luna`. Route these to `Sol` and apply the review cap: P3-12 (model change validator), P3-36 (stale render guard), P3-41 (DST rules), P3-45 (references index in-transaction), P3-61 (sanitizer). They carry the data-loss and injection risk.

## Decisions carried from the ticket draft

1. Tiptap editor and `bluemonday` sanitizer (P3-04).
2. `COMPOSURE_CONFIG_EDITABLE=1` from deployment templates turns the builder on (P3-07).
3. `init` creates Page only; the five launch types ship as an example document (P3-65).
