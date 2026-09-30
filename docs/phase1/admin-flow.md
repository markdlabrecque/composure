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
                                      Preview saved draft link and save-first guidance
POST /admin/pages/{id}              → save draft        (#7)
GET  /admin/pages/{id}/preview      → latest saved draft rendered by the public Page template (#8)
                                      preview context is outside the shared <main> content

Planned:
POST /admin/pages/{id}/publish      → publish snapshot  (#9)
```

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
