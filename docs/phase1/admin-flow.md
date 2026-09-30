# Phase 1 admin flow

The local admin uses ordinary HTML links and forms with full-page navigation.
JavaScript is not required.

The current Page flow supports:

```text
GET  /admin                         → 303 /admin/pages
GET  /admin/pages                   → Page list → /admin/pages/new
GET  /admin/pages/new               → configured Page form
POST /admin/pages                   → 303 /admin/pages/{id}/edit
GET  /admin/pages/{id}/edit         → read-only saved draft
                                      proposed public path remains 404

Planned:
GET  /admin/pages/{id}/edit         → editable form     (#7)
POST /admin/pages/{id}              → save draft        (#7)
GET  /admin/pages/{id}/preview      → draft preview     (#8)
POST /admin/pages/{id}/publish      → publish snapshot  (#9)
```

The planned routes have no handlers yet; the saved view has no save or publish
control.
