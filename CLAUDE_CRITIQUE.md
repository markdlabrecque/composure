# Composure plan: adversarial review

Reviewed: `docs/prd.md`, `docs/architecture_plan.md`, `docs/work_plan.md` (and `.lavish/work-plan.html`, which mirrors the work plan) on `main` at `2c764c6`.
Date: 2026-09-26

The review argues against the plan on purpose. It looks for what will break, what is missing, and where the documents contradict each other. The plan is detailed about editor-facing behaviour. It is thin on the few technical decisions that will decide whether that behaviour can be built.

## Verdict

The PRD describes behaviour well, and the tracer-bullet rules are sound. Three problems matter more than everything else:

1. **Scope and effort don't match.** The only estimate (21–34 days) was written for a much smaller product. Nobody has replanned since. V1 as written is several times larger.
2. **The hardest decisions are deferred or missing.** The plan never picks a content storage model, never says how configuration drift between the UI and Git is resolved, and never says how the database and files stay consistent. Almost every P0 requirement depends on these.
3. **Deployment and performance get proven last.** The first real server deploy happens in phase 6 and the first load test in phase 7. If either fails, a lot of built work needs redoing.

## Findings

Severity: **Critical** blocks v1 or risks data loss. **High** means likely rework or a security gap. **Medium** is a gap or contradiction worth fixing before its phase starts. **Low** is cleanup.

### Critical

#### C1. V1 scope has outgrown every estimate, and nobody has cut anything

The architecture plan's effort table adds up to 21–34 days. It covers auth, a config-defined content model, CRUD screens, uploads, rendering, snapshots and ops scripts. Since then the PRD has added:

- a UI field builder with 18 field kinds
- named image styles with focal points
- redirects with suspend, reserve and resume rules
- menu drafts with snapshots
- Trash plus a permanent-delete dependency preview
- three export formats and two import paths
- a config diff/deploy pipeline
- an audit log
- SMTP invitations and resets
- branding
- a cross-browser matrix
- load testing

The architecture plan admits the table is stale (`architecture_plan.md:200`), and the work plan pushes re-estimation to after phase 1. Nothing in the plan says which features get cut if phase 1 shows the build is much bigger than expected. Every feature is P0, so there is nothing to drop.

**Recommendation:** Rank the P0 list now: must-have, v1-if-time, v1.1. Good cut candidates:

- database and file export/import (see C5)
- manual external redirects
- menu draft snapshots
- decimal, phone and multi-choice fields
- UI-editable image styles (ship fixed styles in config)

#### C2. Configuration has two sources of truth, and drift is never resolved

The PRD makes configuration editable in the admin on the live site. Active config lives in SQLite. The same config is also exported to Git and applied by an explicit deploy (`prd.md:39`, `prd.md:47`). So an administrator can change a field on production after the last export, then Mark deploys from Git. The deploy "compares target with active". Nothing says what happens next:

- Does the deploy overwrite the production edit?
- Does it refuse?
- Does it merge?

This is the most painful part of Drupal's configuration management, and the plan has quietly taken it on. Neither the PRD nor the work plan mentions drift, three-way comparison, or a config lock.

**Recommendation:** Pick one before phase 1 bullet 3:
- (a) Config is editable only in dev/staging. Production is locked, and deploy is the only way to change it.
- (b) Production is the source of truth, and Git export is only a backup or audit record.

Option (a) fits "maintainer-managed sites" better and removes a whole class of bugs.

#### C3. The content storage model is undecided, and the architecture plan contradicts the PRD

Administrators define types and fields at runtime through the UI (FR-03, FR-14). The architecture plan still says "Types, fields and relations defined in config; migrations" and "Our own tables and migrations" (`architecture_plan.md:174`, `:205`). Those lines assume a code-defined schema. The options are different architectures:

| Model | Consequence |
|---|---|
| Table per type with runtime DDL | Schema changes at runtime. Migrations and snapshot history get hard. Title search across types needs a union. |
| EAV (entity-attribute-value) rows | Flexible, but queries, validation and performance get worse |
| JSON document per item/revision (SQLite JSON1) | Simplest for snapshots and exports. Needs rules for lazy upgrades and relationship indexing. |

Snapshot compatibility after model changes (FR-06), config deploy loss checks (FR-15), relationship cleanup on delete (FR-04), exports (FR-16) and cross-type search (FR-13) all depend on this choice. The work plan lists "supported model-change transformations" as a later implementation detail (`work_plan.md:488`). In fact it is the central design question.

**Recommendation:** Write an ADR (architecture decision record) choosing the storage model and the model-change rules before phase 1 bullet 2. Say which changes are allowed (rename, add, remove, change type, single↔multiple, tighten validation) and what each one does to existing drafts and snapshots. Update the architecture plan to match.

#### C4. Deleting orphaned files right away breaks rollback and restore

The PRD lets Composure delete an orphaned file "after the content change succeeds" (`prd.md:45`). The deploy rollback restores an earlier database snapshot (`prd.md:47`). If an editor replaces an image after the snapshot and the old file gets cleaned up, the restored database points at a file that no longer exists. The same thing happens with a full export taken while cleanup runs, and with any backup restored from before the deletion. The work plan tests cleanup "only after commit" but never tests cleanup against rollback.

**Recommendation:**
- Store files content-addressed (named by a hash of their bytes) and never change them after writing.
- Delete orphans in a separate garbage-collection pass, and only once they are older than the oldest backup or rollback snapshot you keep. Make that window configurable.
- In full export, back up the database first and then the files. The file set is then always a superset of what the database references.
- Add a phase 6 test: replace an image, roll back the deploy, and check the old image still renders.

#### C5. Three export formats are v1 scope with no v1 user

The PRD rules out moving an existing site (`prd.md:70`). Full export/restore covers backup and recovery. The database-only and file-only exports, with their imports, ID and conflict checks and variant regeneration, have no v1 journey that needs them. Import conflict rules (merge? replace? what if IDs collide?) are also undefined (`work_plan.md:488`), so FR-16 can't be tested as written.

**Recommendation:** Ship full export/restore only in v1. Move portable database and file export/import to the roadmap, where a real migration can define the conflict rules.

### High

#### H1. Deployment and load testing come too late for a tracer-bullet plan

The work plan's own rule says a tracer should touch every layer the feature will need. Production hosting (Hetzner, systemd, Caddy, TLS) is a layer, yet no bullet reaches it until phase 6. The load test is phase 7. The phase 1 prototype "remains local until phase 2 access controls pass review", but no later phase before 6 deploys either. Surprises on the host would come after most of the build:

- cgo or static-build problems (see H2)
- file permissions
- SQLite on the host's filesystem
- Caddy caching behaviour
- memory limits

**Recommendation:** Add a phase 2 bullet: deploy the authenticated Page journey to the real host with the systemd/Caddy template, behind auth or an IP allowlist. Run a quick smoke load test there. Phase 6 then hardens the deploy instead of building it from scratch.

#### H2. WebP encoding and image processing probably break "single static binary, few dependencies"

Uploaded images are re-encoded (`prd.md:123`). WebP is an allowed format, and styles produce resized crops. Go's standard library can't encode WebP, and `golang.org/x/image/webp` only decodes. Encoding WebP, and doing good-quality resizing at scale, usually means cgo (libwebp, libvips) or a WASM codec. That affects:

- the static-binary claim (`architecture_plan.md:171`)
- the build and deploy template
- the "few dependencies" driver

**Recommendation:** Pick the image library before phase 2 bullet 4. Options: re-encode WebP uploads as PNG/JPEG, add a pure-Go WebP encoder, or accept cgo and record that in the architecture plan. Also say when variants are generated. On upload or publish is safe. Generating lazily on the first public request lets visitors trigger CPU-heavy work (see H5).

#### H3. Upload security is described as a goal, not as controls

"Type validation and safe storage" and "images are re-encoded" (`prd.md:123`) leave out the attacks that usually get through:

- **Decompression bombs.** A small PNG can declare huge pixel dimensions. Nothing sets a maximum pixel count before decoding.
- **Metadata leaks.** Nothing says to strip EXIF/GPS data. Re-encoding usually removes it, but the requirement should say so.
- **DOCX is a ZIP.** It can be a zip bomb, and a macro-enabled `.docm` can be renamed `.docx`. Nothing requires a ZIP structure check.
- **PDFs can carry JavaScript.** Serving rules aren't stated: `Content-Disposition: attachment`, `X-Content-Type-Options: nosniff`, and a separate path or origin for user files.
- **Filenames and paths.** Nothing requires server-generated storage names.

**Recommendation:** Turn these into acceptance checks in phase 2 bullet 4 and phase 4 bullet 3. Add fuzz tests for the upload validators. Go's built-in fuzzing makes this cheap.

#### H4. Sign-in has no brute-force protection

The PRD allows any password, including very weak ones, after a warning (FR-01). It has no MFA, and it never asks for rate limiting, lockout or throttling on sign-in, invitation or reset endpoints. An admin panel on a public domain with weak passwords allowed and no throttling is the most likely way a site gets compromised. It cuts against the "Security" design driver, which the plan ranks first.

Related gaps:

- The "known-compromised" check isn't defined. It could be an HIBP k-anonymity API call (an external dependency and privacy question) or a bundled list (binary size).
- Nothing says sessions end when a password is reset, a role is removed or an account is deactivated. Deactivation is covered in phase 2 bullet 3. The other two are not.
- The reset request endpoint needs to hide whether an account exists.

**Recommendation:** Add a P0 requirement for per-account and per-IP sign-in throttling with backoff. Either make the password warning mandatory for administrator accounts or add optional TOTP (time-based one-time passwords). Specify session revocation on password and role changes.

#### H5. Visitors can write to SQLite through failed sign-ins

FR-18 logs every sign-in attempt, failures included. The sign-in form is public. During a credential-stuffing attack, every attempt becomes a SQLite write, and the site becomes the "visitor-write-heavy" case the architecture says needs Postgres (`architecture_plan.md:228`). The audit log also grows without limit, because the PRD sets no retention policy.

**Recommendation:** Throttle before logging (H4). Collapse repeated failures into counted entries. Set a retention period.

#### H6. Cache design and invalidation are unspecified, but the performance target depends on them

The load test needs 200 rps "cached". The plan never says what does the caching:

- in-process memory?
- Caddy?
- a CDN?

It also never says how invalidation tracks dependencies:

- Every page shows the menu, so a menu publish invalidates everything.
- An Event page shows its Location, so editing the Location must invalidate the Events that reference it.
- A redirect resuming on republish changes what an old URL returns.

The work plan says "invalidate affected cached pages when publication changes" (`work_plan.md:386`) with no rule for "affected". And if a CDN is in front, the 200 rps target measures the CDN, not Composure.

The targets are also very low for Go. 50 uncached rps is modest for server-rendered pages from SQLite. The load test may pass without ever exercising what could really go wrong:

- publishing during traffic
- image generation during traffic
- several sites competing for the same host

**Recommendation:** Decide the cache layer and invalidation rule (a site-wide generation counter bumped on any publish is simple and correct at this scale) in phase 3. Run the load test with no CDN. Add a mixed scenario: public load while an editor publishes and uploads images.

#### H7. Mark holds every review role

Mark is product owner, sole security reviewer, release acceptor, and effectively the reviewer of agent-written code. "Human review" (`prd.md:118`) with one reviewer is a single point of failure, and reviewing your own work misses the same blind spots twice.

**Recommendation:** Add automated gates: `govulncheck`, `gosec` or `staticcheck`, fuzzing on parsers and uploads, and dependency pinning. Get at least one outside review of auth and uploads before any client site goes live.

### Medium

#### M1. Phase 1 designs every data rule up front, which cuts against tracer bullets

Phase 1 must define stable IDs, config versions, drafts, snapshots, URL ownership, relationships, file references, menu publication, Trash, the config manifest, the export manifest and the audit format (`work_plan.md:340–344`). Yet its bullets only prove a single Page. Contracts designed before the features that use them tend to be wrong, and then everything built on them needs migrating.

**Recommendation:** In phase 1, decide only what a Page needs, plus the ADRs from C2, C3 and C4. Design the menu, Trash, export and audit contracts in the phase that first uses them, with a migration allowed.

#### M2. Concurrent editing can silently overwrite work

Each item has one working draft. The PRD says nothing about two editors changing the same item, and the work plan defers "conflict behaviour for concurrent edits" (`work_plan.md:488`). Without a rule, the last save wins and the other editor's work is lost with no warning.

**Recommendation:** Use a draft version number. Reject a save made against an old version and show the editor a clear message. This is cheap, and it belongs in phase 3 bullet 2.

#### M3. Preview semantics are undefined for related content and menus

FR-05 promises "the preview matches the content that will be published." It doesn't say:

- Does a draft Event preview show its Location as the published version or the draft?
- Does a page preview show the draft menu or the published one?
- What does preview show when a related item is unpublished?

**Recommendation:** Rule: preview shows this item's draft, and published versions of everything else. Document it and test it in phase 3 bullet 3.

#### M4. The URL model is undefined

Nothing says whether paths are:

- flat slugs
- prefixed by type (`/events/foo`)
- hierarchical

It also doesn't say what happens to existing URLs when a type's URL pattern changes in config. Mass redirects? Rejected at deploy? Reserved paths from suspended redirects can also block an editor indefinitely, with no error explaining which rule holds the path.

**Recommendation:** Specify the path scheme. Make pattern changes a config change that the deploy diff reports, with redirects created automatically. When a reserved path blocks an editor, name the reserving rule in the error.

#### M5. Menu behaviour when a target is unpublished or trashed is undefined

The PRD covers menu items on permanent deletion only. It doesn't say what a published menu does while its target is unpublished or in Trash: hide the item, show a dead link, or show a warning in the admin.

**Recommendation:** Hide the item publicly, flag it in the menu editor, and restore it on republish, mirroring the redirect behaviour.

#### M6. A fresh install can't publish without an extra step

FR-02 keeps the administrator and editor roles separate. An administrator can't create content. The CLI creates "the first administrator", and the PRD never says whether that user also gets the editor role. If not, the first journey needs an extra role assignment.

**Recommendation:** `init` gives the first user both roles by default.

#### M7. Rich text conflicts with "no Node" and a "modern" admin

The admin must be "modern, clean and intuitive" (`prd.md:13`) with no SPA build and no Node (`architecture_plan.md:167`, `:195`). Modern rich-text editors (ProseMirror/Tiptap, Lexical) are distributed as npm packages. The plan also leaves these open:

- which HTML sanitizer to use (probably `bluemonday`, a new dependency)
- whether inline images are allowed in rich text
- whether internal links in rich text survive path changes

**Recommendation:** Choose before phase 3. Either vendor a prebuilt editor bundle (and state that exception to "no Node") or use a limited Markdown field. Store internal links by content ID, not path.

#### M8. Rolling back a deploy loses editorial work

Rolling back to the pre-deploy database snapshot throws away everything editors published after the deploy. The documents never mention this window, and phase 6 bullet 4 doesn't test for it.

**Recommendation:** Document it. Prefer forward fixes, and keep migrations additive so a binary-only rollback is usually enough. Add a guard that warns before a database rollback that would drop recent publishes.

#### M9. Sites on one host aren't isolated for resources

One process per site isolates crashes but not CPU or memory. One site generating image variants or under a traffic spike slows every other site on the host.

**Recommendation:** Set `MemoryMax`/`CPUQuota` in the systemd template. Limit image-processing concurrency per process.

#### M10. No accessibility baseline in v1

V1 has no WCAG target (`prd.md:114`). The public theme will serve real organisations, and many have legal accessibility obligations (for example the Accessible British Columbia Act and AODA). Adding accessibility to an admin and theme after they're built costs more than building it in.

**Recommendation:** Set a minimal v1 bar: keyboard operable, labelled form controls, visible focus, and an automated axe check in the browser tests. Keep the full WCAG 2.2 AA audit on the roadmap.

### Low

- **L1. Documents contradict each other.** The architecture plan calls the first step a "Spike" (`architecture_plan.md:298`), but the work plan treats it as kept tracer code. The architecture plan's next actions skip redirects, menus, Trash, audit and SMTP. Either update the architecture plan or mark it as superseded where the PRD differs.
- **L2. Plan and tickets have diverged.** Issue [#14](https://github.com/markdlabrecque/composure/issues/14) ("establish CI and required auto-merge checks first") and PR [#15](https://github.com/markdlabrecque/composure/pull/15) set up CI before phase 1 bullet 1. The work plan says the immediate next step is phase 1 bullet 1 and never mentions CI. Add CI as a phase 0 or phase 1 prerequisite so the plan matches the tickets.
- **L3. "Agent-first" guidance comes last.** Agent guidance is the final next action (`architecture_plan.md:308`), but agents will write the code from day one. Add a minimal `AGENTS.md` with conventions and test commands in phase 1.
- **L4. Observability has no acceptance criteria.** "Enough health and error information" (`prd.md:131`) can't be tested. At minimum, require a health endpoint that checks the database and storage, structured logs with request IDs, and render failures logged with the item ID.
- **L5. Timezone details are open.** The PRD doesn't say whether date-time values are stored as UTC plus an IANA zone, what the site default zone is, or how events that cross a DST change display.
- **L6. The CLI recovery path needs an audit entry.** CLI access recovery bypasses email. It should always write an audit entry, and FR-18 should list it explicitly.

## Decisions to make before phase 1 bullet 2

| # | Decision | Recommended default |
|---|---|---|
| 1 | Configuration source of truth (C2) | Production config locked; deploy is the only way to change it |
| 2 | Content storage model and allowed model changes (C3) | JSON document per item and snapshot, with indexed relationship and path tables |
| 3 | File storage and orphan cleanup (C4) | Content-addressed, write-once, GC after the backup retention window |
| 4 | P0 ranking and cut list (C1, C5) | Drop database/file export and import from v1 |
| 5 | Image library and cgo stance (H2) | Decide and record in the architecture plan |
| 6 | Host deploy in phase 2 (H1) | Yes |
