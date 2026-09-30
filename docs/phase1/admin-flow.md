# Phase 1 admin flow

The local admin uses ordinary HTML links and forms with full-page navigation.
JavaScript is not required.

The current Page flow supports:

```text
GET  /admin                         → 303 /admin/pages
GET  /admin/pages                   → Page list → /admin/pages/new
GET  /admin/pages/new               → configured Page form
POST /admin/pages                   → 303 /admin/pages/{id}/edit
GET  /admin/pages/{id}/edit         → editable saved draft form
                                      Preview saved draft link, save-first guidance,
                                      first-publication action for unpublished Pages
POST /admin/pages/{id}              → save draft        (#7)
GET  /admin/pages/{id}/preview      → latest saved draft rendered by the public Page template (#8)
                                      preview context is outside the shared <main> content
POST /admin/pages/{id}/publish      → validate current stored draft/config, then atomically
                                      create the first immutable snapshot and claim its path
                                      303 to edit with a publication notice
```

The publish form submits only the current `draft_revision`; unsaved values are
not sent. Publication revalidates the stored draft against the active
configuration inside the same SQLite write transaction that inserts the
snapshot, claims the route, and moves the item's published pointer. A path
already owned by another Page returns `409 path_taken` with the owning Page's
title and edit link. Invalid stored values return `422`; stale revisions and
owned paths return `409`. GET and other methods cannot publish.

Phase 1 supports only first publication. A Page that already has a published
snapshot cannot create another snapshot yet; republishing and history belong
to the later snapshot-history work.

Preview and the public route share the same Page renderer and content markup.
Preview reads only the stored draft, returns `Cache-Control: no-store`,
`X-Robots-Tag: noindex`, and HTML content, and accepts GET and HEAD without
changing storage. Unsaved form values and query parameters do not affect it.
The edit form links to the latest saved draft and tells editors to save before
previewing their changes. The create form has no preview link because an item
does not exist until it is saved.

Tickets that change public rendering must preserve the exact shared `<main>`
region in draft preview and public output unless the accepted Page renderer
contract changes explicitly.
