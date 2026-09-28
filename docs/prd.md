# Composure: product requirements document

Status: Draft for v1 scope  
Source: [Architecture plan](architecture_plan.md), September 24, 2026  
Product owner: Mark Labrecque

## Product summary

Composure is a personal CMS project for content sites. It gives editors a small, familiar place to manage website content and gives Mark a controlled alternative to WordPress for sites that do not need its plugin ecosystem. V1 delivers working CMS software that can be configured and deployed for a site.

The product promise is practical: editors can publish and update a content site without touching code, and Mark can deploy, back up, and maintain that site with a small, reviewed codebase. The architecture plan selects Go, SQLite, server-rendered pages, and one process per site. This PRD describes the user experience and release conditions those choices must support.

The admin interface must be modern, clean and intuitive, using front-end technologies that make it feel responsive and performant.

## Problem and intended users

WordPress gives many content sites more runtime and plugin exposure than they need. A custom replacement only makes sense if editors can still do their everyday work easily and Mark can operate it reliably. A developer-facing data panel would not meet that need.

| User | What they need to do |
| --- | --- |
| Editor | Find content, make changes, preview them, publish, and manage images without knowing the storage model. |
| Site administrator | Manage site configuration, accounts, the audit log, and permanent deletion. An administrator also needs the editor role to create or publish content. |
| Project maintainer | Deploy changes, investigate failures, and recover a site. |

Visitors use the resulting public website. They do not need a CMS account.

## Goals and release boundary

The first release must support Events, Locations, Blog posts, News, and Pages, and give Mark a repeatable way to run a site. Administrators define content types and their fields in the CMS interface. A barebones public theme ships with v1 so a fresh installation can display published content and navigation. V1 is complete when the CMS itself works end to end for those content types.

### Content model at launch

The launch field set includes short text, long text and rich text, email, URL or link, relationships between content items, and images. Images use named styles, with multiple crops available for different placements. Administrators can add these fields to a content type and set their labels, order, grouping, help text, required state, and applicable validation rules in the interface. Fields can allow one value or multiple values where that makes sense.

Each content item also needs a title or display name, a stable identifier, publication state, and created and updated metadata. Published items have a slug or URL path. Changing a published path creates a redirect from the old path to the new one. Redirect rules are visible and manageable from a top-level admin screen. An enabled redirect reserves its source path, even while suspended because its target is unpublished or trashed. A new item cannot claim that path until the rule is manually disabled or removed.

The launch field set also includes date and time with timezone, integer and decimal numbers, yes or no, single and multiple choice, phone, structured address with optional coordinates, and document uploads. Events need date and time; Locations can use address and coordinates. Launch uploads allow JPEG, PNG, and WebP images, plus PDF and DOCX documents.

Content type and field definitions and menu definitions are configuration. Administrators manage them in development or staging; production configuration is read-only in the interface and direct API requests. Git-reviewed configuration is applied only by explicit deployment. SQLite holds the active deployed copy. Deploy refuses an unexpected active revision rather than overwriting drift; see [deployment and storage decision](adr/0001-runtime-content-and-deployment.md). Content items, menu items, revisions, uploads, redirect rules, and editorial media choices live in the site's database and media storage. Editors publish content and manage menu items and redirects directly on the live site without merging them through Git. Configuration management must let the maintainer compare and apply changes without silently losing content.

Image styles follow Drupal's basic model: retain one original image and generate reusable variants from named style definitions. For launch, a style defines dimensions and either resize-to-fit or resize-and-crop. Administrators can define multiple styles in the interface; their definitions are configuration. Each image has one editor-set focal point shared by all crop styles. One image field can therefore produce multiple crops without separate crop editing for each style. Generated variants are replaceable outputs, not new editorial uploads.

Editors upload images and documents directly into fields on a content item. The image field shows a preview and focal-point control, and stores alternative text for that placement or an explicit decorative choice. Composure tracks source files and generated variants internally, but v1 has no media library screen or cross-item asset picker. Reusing a file in another item requires another upload.

Replacing a file creates a new stored file rather than overwriting the old one. A file remains while any draft, published snapshot, or trashed item references it. After the last reference is removed, separate garbage collection may delete the file only after the recovery retention window and retained rollback pins permit it. Content-addressed files are immutable; exports pin their file set against cleanup. See the deployment and storage decision for consistency and retention rules.

Configuration is applied by an explicit deployment command. The deploy process validates and compares the target configuration with the active version, rejects changes that would silently lose content, takes a database backup, applies the change, starts the new release, and checks the site. An ordinary service start reads the active configuration and checks its version; it does not import or change configuration. A failed deployment must have a documented way to restore the prior release and database snapshot.

Composure sends account invitation and password-reset email through a configured external SMTP relay. The application does not operate a mail server. Non-secret sender and relay settings can be part of site configuration; relay credentials are deployment secrets and are not exported to Git.

Success means:

- An editor can create, preview, publish, revise, unpublish, trash, and restore content in the admin without developer help.
- Draft changes stay private until publication, and editors can preview the page they intend to publish.
- The public site serves published items for the five launch content types, navigation, and media.
- Mark can deploy an update, create a full site export, and restore it.
- Security-sensitive code passes human review before v1 release. Load testing checks the agreed baseline on the intended hosting.

The first release is limited to maintainer-managed, editor-driven sites. It has no plugin marketplace, third-party code extensions, shared site database, or general-purpose site builder. Visitor-write-heavy features such as busy forms, comments, and per-pageview event collection require a separate decision before inclusion. A Postgres adapter is outside the first release.

## Release ranking

P0 remains the must-have release gate, not an estimate. Portable database-only and file-only export/import move to v1.1; full recovery stays P0. If capacity is insufficient after phase 1, cut candidates, in order, are manual external redirects, menu snapshot restoration, decimal/phone/multiple-choice fields, then UI-editable image styles in favour of configuration-defined styles. These are not silent optional requirements: Mark must approve a PRD and acceptance-test change before removing any remaining P0. Security, recovery and data-preservation controls cannot be traded away. Do not use the obsolete 21–34 day estimate for scheduling.

## Roadmap after v1

- Portable current-content database and original-file export/import, once a real migration defines merge, replacement and ID-conflict rules.

- Scheduled publishing, so an editor can choose when content goes live without returning to the admin at that time.
- A media library for finding, managing, and reusing uploaded files across content items.
- Event sourcing for content and administrative changes, if a later version needs events as its system of record. V1's audit log records selected actions but does not drive application state.
- WCAG 2.2 AA conformance work for the admin interface and bundled public theme, including an audit and fixes.

### Command-line tool at launch

Composure includes a CLI for starting the server, initializing a site, recovering access, exporting and validating configuration, showing a configuration diff, explicit deployment, and full export/restore. From phase 2, initialization gives the first user both administrator and editor roles. The full export is a ZIP recovery artifact with a versioned manifest, the complete SQLite database and all stored files, including snapshots, Trash, configuration and accounts. Deployment secrets remain external. Restore validates checksums, versions and reference completeness into a fresh site before cutover; v1 does not merge sites. Routine content creation, editing, and publishing belong in the admin interface. Commands that change data must validate input, show the intended effect before applying it, and return useful errors for scripts. Moving a specific existing site and portable partial imports are outside v1 scope.

## Core user journeys

1. **Create and publish content.** An editor signs in, chooses a clearly named content type, creates an item, saves a draft, previews it, and publishes it directly. The public page shows the published version.
2. **Revise published content.** An editor opens an existing item, saves changes as a draft while the current version stays public, previews the draft, and publishes it. Each publish action records a snapshot. An editor can restore an older snapshot as a new draft, then preview and publish it.
3. **Unpublish content.** An editor takes an item off the public site while keeping it in the admin for later editing or republication. Redirects to that item are suspended and automatically resume if it is republished, unless an editor manually disabled the rule.
4. **Trash and restore content.** An editor moves an unwanted item to Trash, removing it from normal content lists and the public site. Redirects to it are suspended until republication. Restoring the item returns its content as unpublished for review; it does not make the item public. Trash has no automatic expiry. Before permanent deletion, an administrator sees the target item, every menu item and redirect that will also be deleted, and every content item whose relationship field will lose a reference. The administrator confirms the complete change before it takes effect.
5. **Manage redirects.** When an editor changes a published item's path, the old path permanently redirects to the new one. The editor can find and manage that rule alongside manually created permanent or temporary redirects in a top-level admin screen. Manual rules can point to another path on the site or to an external website.
6. **Add files to content.** An editor uploads an allowed image or document into a field on the content form. The image field allows a focal point and placement-specific alternative text or a decorative choice. Invalid or oversized files receive a useful error.
7. **Maintain navigation.** An administrator defines a menu in configuration. An editor changes its links, labels, hierarchy, and order in a working draft, previews the whole menu, then publishes all changes together. Each menu publication records a snapshot that can be restored as a new draft.
8. **Configure a site.** An administrator defines a content type and fields in the interface, then an editor creates an item using the resulting form. Mark can export and apply the site's configuration, deploy its service, and restore its data and media from backup when needed.

## Functional requirements

Priority **P0** means required for v1.

| ID | Priority | Requirement | Acceptance evidence |
| --- | --- | --- | --- |
| FR-01 | P0 | Users can sign in and out. Authenticated admin access uses secure sessions. Administrators can invite users, and users can request a password reset through email sent via a configured SMTP relay. The CLI can create the first administrator and recover access when email is unavailable. Users may choose any password. When a chosen password falls short of the recommended length or appears in the pinned local common-password list, Composure warns the user and requires explicit confirmation before saving it. | An unauthenticated visitor cannot reach admin content or draft previews; sign-out ends the session; invitation and reset links are single-use and expire; account emails are delivered through the configured relay. A weak password can be saved only after the user confirms the warning; no minimum length, character-mix rule, or blocklist prevents the choice. |
| FR-02 | P0 | The CMS supports independent administrator and editor roles. A user can hold either role or both, receiving the union of their permissions. Editors manage content, menu items, and redirects, including direct publishing. Administrators manage site configuration, accounts, the audit log, and permanent deletion. Neither role grants the other role's permissions. The administrator role always has its full built-in permissions; its permissions cannot be removed. Administrators can deactivate user accounts, preserving their attribution on past content. User accounts are not permanently deleted in v1. Once a site is initialized, it must always have at least one active administrator account. The authorization design leaves room for more specific permissions later. | An editor-only user cannot change configuration, accounts, or the audit log or permanently delete content. An administrator-only user cannot create, edit, or publish content, menu items, or redirects. A user with both roles can do both sets of work. Authorization is enforced by direct requests; a deactivated user cannot sign in but remains named on past content; the last active administrator cannot be deactivated or lose the administrator role through the admin or CLI. |
| FR-03 | P0 | In development or staging, an administrator can create and change content types and fields in the interface, including field order, groups, labels, help text, required state, and relevant validation. | A new type produces usable list, edit, and preview screens without code changes; an existing type can gain a field without losing content. |
| FR-04 | P0 | Editors can list, find, create, edit, move to Trash, and restore items for each agreed content type. Restored items remain unpublished until explicitly published. Trashed items remain until an administrator explicitly deletes them permanently. The permanent-delete confirmation lists the target item and all dependent changes. On confirmation, Composure deletes menu items and redirects targeting it and removes relationship links from other content; those other content items remain. | Content lists show at least title, content type, publication status, and last update. Trashed items leave normal lists and public routes, appear in Trash, do not expire automatically, and return intact but unpublished when restored. Editors cannot permanently delete them. The confirmation identifies each deleted menu item by menu name and item title, each deleted redirect by source and target, and each affected content item by content type and title. Cancellation changes nothing; confirmation applies the deletion and reference cleanup together. |
| FR-05 | P0 | Editors can save drafts, preview them privately, publish directly, and unpublish content. Unpublishing or trashing an item removes redirects to it from active routing. | Drafts and unpublished items do not appear on the public site, including through old URLs; an unpublished item remains available in the admin; the preview matches the content that will be published. |
| FR-06 | P0 | Each publish action records an immutable content snapshot with author and time. Draft saves update the working draft without adding history. An editor can restore a snapshot as a new draft. | Past published versions can be listed and inspected; restoring one does not change the public version until the editor publishes it. If a referenced item was permanently deleted, restore omits that link and warns the editor while leaving the historical snapshot unchanged. Changes to the content model cannot silently discard other snapshot data during restoration. |
| FR-07 | P0 | Editors can upload JPEG, PNG, and WebP images and PDF and DOCX documents directly into content fields. Image placements support a focal point and alternative text or a decorative choice. Site configuration sets a global maximum file size and separate image and document limits. Files remain while referenced by drafts, published snapshots, or Trash. | The system enforces the lower of the global and category limit, rejects other formats and oversized files with a useful error, displays processed images, and serves allowed documents for download. Replacing a file preserves versions still in use; an orphan remains until the recovery-safe garbage-collection rules allow deletion. No media library or cross-item picker is required. |
| FR-08 | P0 | Public routing and a barebones bundled theme render published items for the five launch content types and navigation. | A fresh installation can render a published item of each type and a menu at working public URLs without custom templates; site-specific presentation can be added without changing the content workflow. |
| FR-09 | P0 | The admin uses site terminology, logo, and colours within a consistent shared interface. | Administrators can set labels and branding, and editors can identify content actions without developer terminology. |
| FR-10 | P0 | The maintainer can deploy and run each site as an isolated service with its own database and media storage. | A deployment or restart for one site does not require restarting another site service. |
| FR-11 | P0 | The maintainer can create and restore a full export of a site's SQLite database and stored files, including configuration, user accounts, published snapshots, and Trash. Deployment secrets are supplied separately. | A restore rehearsal produces a working installation with the expected content, images, snapshots, trashed items, and accounts. |
| FR-12 | P0 | Administrators define menus as configuration. Editors manage menu items as content, including their label, target, hierarchy, and order. Editors preview and publish the whole menu as one change, with a publish-only snapshot. | A menu definition can be exported with configuration; a set of item changes remains private until the editor publishes the menu; the public menu changes together and a previous version can be restored as a draft. |
| FR-13 | P0 | The CMS provides a content listing across the launch content types, with title search and filters for content type and publication status. | An editor can find a known item by title, narrow results by type and status, and open it from the results. Search and filters work together and return results across content types. |
| FR-14 | P0 | The field builder supports short text, long and rich text, email, links, relationships, images with multiple named styles and one shared focal point per image, date and time with timezone, integer and decimal numbers, yes or no, single and multiple choice, phone, structured address with optional coordinates, and documents. | An administrator can model and edit an item for each launch content type; one image produces distinct crops from its named styles and shared focal point. |
| FR-15 | P0 | The maintainer can export and apply versioned content model configuration, separately from editorial content. Production configuration is deploy-only; apply refuses unexpected active revision/hash changes and the ADR's unsupported model transformations. | A model change can be reviewed and promoted by explicit deployment without silently deleting content; an ordinary restart does not import it. Production UI/direct mutation and stale deploy plans are rejected. |
| FR-16 | P0 | The CLI supports site setup, configuration deployment and full export/restore using a versioned ZIP manifest. Partial portable exports and imports are post-v1. | A recovery round trip preserves content, files, configuration, accounts, snapshots and Trash. Invalid packages fail before cutover; routine editorial work stays in the admin. |
| FR-17 | P0 | A change to a published item's path creates a permanent redirect. A top-level Redirects screen lets editors view, create, edit, and manually disable permanent or temporary rules targeting internal paths or explicit external URLs. Enabled rules to unpublished or trashed items are suspended, reserve their source path, and resume automatically when the target is republished. Manually disabled rules stay disabled. | An old public URL reaches the new URL while the target is published; old URLs do not redirect to unpublished or trashed items; republication resumes suspended rules; manually disabled rules do not resume; the admin shows each rule's status; conflicting or looping rules cannot be activated. |
| FR-18 | P0 | Composure records an audit log for sign-in attempts, account and role changes, configuration deployments, content and menu publication or unpublication, permanent deletion, and exports. Each entry includes time, action, outcome, actor when known, and affected item or operation. Administrators can review the log. Draft saves do not create audit entries. | An administrator can find who performed a covered action and when, including failed sign-ins and CLI operations; entries do not contain passwords, reset tokens, uploaded file contents, or other secrets. The audit log does not control or reconstruct site state. |

## Product and quality requirements

### Editorial experience

- Admin screens use the site's content names. System fields appear only when editors need them to complete a task.
- Each content type has a consistent list, edit, and preview experience. Validation errors explain what to fix and preserve entered work.
- The admin and bundled public theme must work in the current stable Chrome, Firefox, Safari, and Edge on desktop, plus Safari on iOS and Chrome on Android. V1 requires keyboard operation, labelled controls, visible focus and automated axe checks on critical admin journeys and bundled theme pages. A full WCAG 2.2 AA audit remains post-v1.

### Security and privacy

- Authentication, authorization, sessions, CSRF protection, and upload handling are required launch controls. A human reviews security-sensitive changes and the deployed configuration. Before any client launch, someone other than the implementation author reviews auth and uploads. CI pins dependencies, runs govulncheck and staticcheck, and runs bounded parser/upload fuzz tests once those components exist.
- Throttle sign-in, invitation and reset requests before expensive hashing, SMTP or audit writes, using bounded per-account and per-IP counters with backoff, not permanent account lockout. Reset requests have indistinguishable responses for existing and unknown accounts. Revoke sessions on password changes, role changes and deactivation. Password warnings use a pinned local common-password list; no online password lookup or claim of exhaustive breach detection.
- Audit CLI access recovery explicitly. Aggregate repeated authentication failures into counted entries with first/last times and outcome; rate-limited requests must not cause one SQLite write each. Default audit retention is 90 days, configurable, with bounded aggregation memory and periodic deletion.
- The administrator and editor roles have separate permissions, which combine when assigned to the same user. The administrator role retains all of its built-in permissions. Site initialization, account changes, CLI operations, and full restores must preserve at least one active administrator account. Deactivation revokes access without erasing authorship.
- Invitation and password-reset links use random, single-use, expiring tokens. The relay password or token is stored as a deployment secret, never in exported site configuration.
- Drafts and revision data are accessible only to authorized users. Site installations keep their data separate.
- The audit log records selected security and high-impact editorial actions, including failures, without storing secrets. Only administrators can review it; ordinary draft saves do not add entries.
- Image and document uploads use an allowlist of JPEG, PNG, WebP, PDF, and DOCX, with type validation and safe storage. Before decoding, reject images over 40 megapixels or 16,384 pixels on either axis; re-encode without EXIF/GPS metadata. WebP input is decoded with pinned golang.org/x/image/webp and stored as PNG; JPEG/PNG encoding uses Go's standard library. Variants use golang.org/x/image/draw, are generated by bounded editorial work before publication, never by public requests. Use one image worker per site initially. DOCX validation checks ZIP entry count, total expanded bytes, compression ratio, paths, expected OOXML content types and absence of macros; reject encrypted or unsupported archives. Documents download from a dedicated media route with attachment disposition and nosniff, never as inline HTML. Storage names never derive from upload filenames. The default per-file limits are 25 MiB globally, 10 MiB for images, and 25 MiB for documents; a site can override each setting in configuration. The effective limit is the lower of the global and media-specific limit.
- Password choice is advisory: Composure recommends at least 15 characters and warns about shorter passwords or passwords present in the pinned local common-password list. Users can explicitly confirm and use a warned password. It does not impose a minimum length, character-mix rule, or routine forced changes. Passwords must be stored using a suitable password hashing scheme, never in plaintext.

### Reliability and performance

- On the intended Hetzner host, the public site must sustain 200 requests per second for cached published pages and 50 requests per second for uncached published pages, each for 10 minutes. At least 95% of cached responses must complete within 250 ms and at least 95% of uncached responses within 500 ms, with no application errors. These are minimum v1 release targets, not maximum capacity estimates. The test setup must state host size, page mix and cache state. No CDN or proxy response cache is allowed in acceptance measurements. Also test public traffic while an editor publishes and uploads images, and contention between two sites on the same host.
- Load testing must also increase traffic beyond those targets until latency or errors rise, then record the first observed bottleneck and supporting measurements. Exceeding the v1 targets does not require a v1 fix unless the bottleneck prevents the agreed baseline.
- Failed deploys must have a documented rollback path. Composure provides full export and restore commands, but scheduling backups and storing them off the host are the operator's responsibility.
- Readiness checks database access and storage availability without exposing internals; failure returns 503. Structured logs include request IDs, and render failures include item IDs without draft values or secrets. Alert routing remains an operations decision.
- Public HTML uses a bounded in-process cache keyed by site publication generation and path. Any public-affecting publish, unpublish, trash, deletion, redirect or configuration change bumps the generation transactionally. Never cache admin or preview responses. An old in-flight render cannot populate the new generation.

### Editorial consistency

- Save, publish and snapshot restore compare the expected draft revision; stale requests return a conflict with submitted work preserved, never last-write-wins.
- Item preview shows that item's draft and published versions of related items and menus. Unpublished targets are hidden. Menu preview substitutes only its own draft. Published menus hide unpublished or trashed targets, flag them in the editor and restore links on republication.
- Paths are explicit per-item hierarchical paths using the Page contract's normalization and reserved namespace. V1 has no configurable per-type URL patterns or bulk pattern migration. Reservation errors identify the owning item or redirect.
- Store date-time values as a UTC instant plus an IANA timezone. The site default is UTC unless configured otherwise. Reject nonexistent DST local times; ambiguous times require an explicit offset choice. Render event endpoints in the saved zone with offsets when a DST transition makes them ambiguous.
- Before phase 3, select and security-review a pinned prebuilt rich-text editor bundle and a Go HTML sanitizer. No Node runtime or normal application build is required; updating the vendored bundle may use upstream Node tooling. Inline image embedding is out of v1; use image fields. Internal content links store item IDs and resolve published paths at render time.

## V1 release criteria

V1 is ready when all P0 requirements pass in a fresh installation and the admin and public site work end to end for the five launch content types. Mark must also complete a security review of auth and uploads, verify a full export and restore round trip, rehearse deployment and rollback, meet the public performance targets, and document the bottleneck found by increasing load.

## Delivery sequence

1. Establish CI and agent conventions, then validate one Page end to end with the chosen stack and minimal configuration format.
2. Build and review auth, sessions, roles and upload controls; deploy the protected Page to Hetzner and run a smoke load.
3. Build generated admin screens, drafts, preview, publish-only snapshots, and public rendering for the five launch content types.
4. Add the CLI, site branding, deployment templates, and full export and restore commands.
5. Run the v1 release checks above, then resolve gaps before release.

This sequence expresses dependencies, not a schedule. The architecture plan's effort estimates are rough and should be revised as implementation work proceeds.
