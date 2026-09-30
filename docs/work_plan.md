# Composure phased work plan

Status: Active delivery plan, updated 2026-09-30. Phase 1 has completed draft creation/listing and active configuration export/validation. Draft editing is next; phase completion still requires the publishing journey and reviewed exit evidence.

Scope authority: [PRD](prd.md). Technical starting point: [Architecture plan](architecture_plan.md). The PRD takes precedence. This plan expands its delivery sequence into demonstrable milestones; it does not change v1 scope.

## Goal and approach

Deliver a CMS that editors can use for Events, Locations, Blog posts, News, and Pages, and that Mark can deploy and recover as an isolated site. The phase 1 contract supplements the PRD and architecture plan; do not treat contract decisions as implemented features.

Build one complete publishing journey first, then expand the model and workflows. Design data preservation, permissions, and audit recording early because later features depend on them. Deliver a usable admin throughout the build. Each phase consists of tracer bullets: small, integrated pieces of working behavior that pass through a real entry point, application logic, storage and an observable result. Each bullet ends with an executable acceptance check and a demonstration. Phase completion combines those checks and verifies interactions between bullets.

The sequence describes dependencies, not calendar commitments. Re-estimate after phase 1 and after the first full editorial workflow. Mark owns product decisions, human security review, and release acceptance. Implementation ownership can be assigned when phases become tasks.

## Tracer-bullet delivery rules

Build the bullets within each phase in order unless their dependencies are already satisfied. Finish and verify one thin path before broadening it. Avoid separate database, backend and interface batches that only connect at the end of a phase.

- Start each bullet with one concrete user or operator scenario and its observable acceptance check. Deliver the minimum working path through the actual admin, public HTTP routes or CLI and persisted storage.
- Use a real SQLite database and real filesystem in integration checks. Use a local SMTP capture service for account email. Production credentials and a live mail relay are not needed for routine tests.
- Check the successful path and its most consequential failure or permission boundary. Verify persistence after restart where relevant, and check the public response when an action affects publication.
- Keep automated acceptance checks runnable from one documented command. Use focused integration tests for rules and browser tests for critical interface journeys; avoid repeating every field combination through the browser.
- A bullet is complete when the integrated path works, its checks pass, its PR has merged, and the orchestrator has posted the completion summary and closed its ticket. A mock screen, isolated storage layer or stubbed publishing response is not a completed bullet.
- Thin initial implementations are allowed, but record the remaining PRD scope and assign it to a later bullet. No stub or temporary bypass may satisfy a v1 acceptance condition. The phase 1 prototype remains local until phase 2 access controls pass review.

The bullets below are delivery boundaries, not a complete task backlog. Split any bullet that cannot be demonstrated independently in a short implementation cycle, while retaining an entry point and observable result in every split.

## Merge and ticket completion

Use `develop` for ticket worktree bases, rebases, PR targets and completion checks. The main checkout's `.env` must set `BASE_BRANCH=develop`. Required merge checks and auto-merge enforcement apply to `develop`. Historical PRs merged into `main` remain historical evidence; verify their changes are present in `develop` before using them as prerequisites. Promoting `develop` to `main` is a separate release action and does not block ticket closure. This policy replaces older `main` targets and serial-queue wording in ticket descriptions and the phase tracker.

Independent tickets may run in parallel when their prerequisites are complete and their file ownership and contracts do not conflict. Start each ticket in its own numbered worktree and separate Orca Codex session. Use the `subagent-tdd-pipeline` stages and model profiles; exactly one `Luna` or `Sol` label selects the implementor. Documentation and verification-only work use the skill's applicable validation and independent-review route.

### Dispatch and recovery

1. Check the issue, dependencies, routing label, assignment, checkout and base before creating a worktree. Resolve scope or policy conflicts before launch.
2. Dispatch actionable leaf tickets first. Assess a parent after its children complete; a parent assessment does not need an idle agent session while a child is unfinished.
3. One coordinator owns each ticket's dispatch and recovery. After setup succeeds, verify the exact workspace, path, agent identity and readiness before sending the task.
4. Record the accepted send receipt, `turn_started` and the first task action before reporting the ticket started. Input acceptance alone is not proof of progress. Surface failed delivery and pending questions promptly.
5. Recover in the existing worktree. Inspect the current terminal before retrying; use the durable request receipt to avoid duplicate input. Replace an agent only after confirming the previous process exited. Keep a checkpoint for each active ticket with its stage, worktree, agent handle, revision, evidence and next action.

### Completion

The ticket session owns review, checks, the verified completion summary, issue closure and cleanup. A reporter may carry out these actions under that session's coordination.

1. Confirm prerequisites are merged into or already present in `develop`, summarized and closed. Verification-only prerequisites require accepted evidence and closure, not a duplicate PR.
2. For changes, obtain independent reviewer approval and pass all required local gates on the candidate before pushing. Open a PR targeting `develop`, titled `#<number>: <title>`, with `Refs #<number>` and no automatic closing keywords.
3. Enable auto-merge only after approval and confirmation that the required CI check is configured. Require passing CI for the current candidate against the current `develop` base. New candidate or base changes require applicable gates and review again. Never bypass a failed or missing check.
4. Confirm GitHub reports the PR merged into `develop`; record the merge commit and CI-tested revision. For verification-only work, retain the inspected revision, commands, results, existing merge/CI evidence and independent review. Do not invent retrospective review or auto-merge evidence. Unresolved acceptance criteria keep the ticket open.
5. Post the completion summary on the issue with acceptance coverage, local/CI results, tested revision, review outcome, merged PR/commit where applicable, and limitations or follow-ups. Verify it exists before closing the issue and updating the tracker. Reuse existing summaries for the same result.
6. Invoke `retire-worktree` after closure. Dependent tickets may start only after prerequisite completion. Close parent issues after all required children and parent criteria have evidence; close the phase tracker after its verified phase summary.

Bootstrap CI comes first. Issue #14 establishes the shared runner, stable `Composure checks` status and CI-gated auto-merge on `develop`. Issue #4 activates real Go checks; issue #13 adds the browser gate to the same workflow. Local and CI commands stay aligned. Missing application or browser tests must never be reported as passing tests.

## Phase 1. Prove the foundation and publishing model

Outcome: A local Go application persists and renders one example Page, proving the proposed stack and the separation between draft and published content.

- Establish CI and auto-merge first, then the Go application, SQLite migrations and data access boundaries, server-rendered templates, local startup, and basic test checks.
- Fix the Page IDs, JSON draft/snapshot storage, configuration versions and URL ownership, plus the [deployment and retention boundaries](adr/0001-runtime-content-and-deployment.md). Later features may migrate the schema; do not build menu, Trash or reference indexes in phase 1.
- Sketch the shared admin layout and the create, edit, preview, and publish journey. Choose the small amount of browser enhancement needed for responsive forms within the Go architecture.
- Prove one local example Page can be saved, previewed, and published. This is an internal prototype until phase 2 supplies access controls.
- Define the minimal Page configuration format. Design audit entries in phase 2, media indexes in phase 4, menu/Trash storage in phase 5 and the full recovery manifest in phase 6. Issue #3 covers Page configuration only; portable partial export/import is deferred beyond v1.
- Add minimal agent conventions and actual test commands before application implementation. Re-estimate the revised must-have scope at phase exit, using the PRD's ordered cut candidates rather than the obsolete day table.

### Progress as of 2026-09-30

Closed prerequisites include #2, #3, #4, #5, #6, #11, #14, #19, #20, #23, #24, #25, #29 and #30, plus runner-verification children #35, #36, #37, #38, #39 and #40. Preserve their evidence and verify their delivered changes are present in `develop` during readiness checks. Do not redispatch completed work. #1 remains the phase tracker.

- The Page storage/runtime and configuration contracts, bootstrap CI and required merge checks are complete. The application initializes and serves a stored Page and rejects incompatible site versions without changing data.
- [#11](https://github.com/markdlabrecque/composure/issues/11) completed active configuration export and validation in [PR #50](https://github.com/markdlabrecque/composure/pull/50), merged into `develop` at `90743fac181f08fc9ba582188785e9dc22f93301`. Its [completion summary](https://github.com/markdlabrecque/composure/issues/11#issuecomment-5912577676) records acceptance, review and local/hosted gate evidence.
- [#6](https://github.com/markdlabrecque/composure/issues/6) completed the generated create form, draft persistence, Pages list and read-only saved view in [PR #51](https://github.com/markdlabrecque/composure/pull/51), merged into `develop` at `e91ea04d4114cd3b1971fdae0fefc096f8a1bba0`. Its [completion summary](https://github.com/markdlabrecque/composure/issues/6#issuecomment-5913719350) records independent review, local and required hosted checks, real Chrome checks at desktop and narrow widths, and persistence after restart. The issue closed as completed on 2026-09-30. Draft creation leaves public routes and snapshots unchanged.

[#7](https://github.com/markdlabrecque/composure/issues/7), draft editing, is the next dependency-ready ticket. Preview, publication, republishing, second-site initialization and the combined browser/phase gates remain open. Authentication and audit behavior remain later-phase work; the application is still a local prototype.

### Remaining ticket waves

The table lists open work as of 2026-09-30 and preserves the original wave numbers. Arrows mean sequential. Each ticket still waits for its own merged, summarized and closed prerequisites and the relevant accepted contracts.

| Wave | Ticket | Outcome | Prerequisites / coordination |
| --- | --- | --- | --- |
| 4 | #7 | Edit an existing draft without history | #6 is complete; ready for pickup after normal dispatch checks. |
| 5 | #8 | Preview a saved draft with the public renderer | #7. |
| 6 | #9 | Publish an atomic immutable snapshot | #8. |
| 7 | #10 | Keep revised drafts private and preserve republish history | #9. |
| 8 | #12 | Initialize a second site from exported configuration and publish through its generated form | #10/#11; join the publishing and configuration lanes. |
| 9 | #26 | Exercise the real Page journey in Chrome, including narrow viewport and accessibility checks | #10/#12 and their accepted contracts; owns browser journey and pinned setup. |
| 10 | #31 | Add the fail-closed local phase gate | #26/#14 and retained #4/#5/#11/#12 suites; invoke the shared runner once, then the browser journey. |
| 11 | #32 | Require the complete phase gate in hosted `Composure checks` | #31/#26/#14 and retained feature suites; verify real failed and passing candidate runs. |
| 11 completion | #27 | Close local/hosted gate parent | #31/#32 and original parent criteria. |
| 12 | #33 | Map phase acceptance to reviewed tests, runs and revisions | #10/#12/#14/#26/#31/#32; owns `docs/phase1/acceptance.md`. |
| 13 | #34 | Write the Phase 2 handoff and evidence-based remaining-P0 estimate | #33/#32; owns the Phase 2 section of this plan. Use PRD audit requirements and #22's scope; schedule its detailed contract in Phase 2. |
| Phase completion | #28 → #13 → #1 | Close evidence/handoff parent, browser/gate milestone and phase tracker | #28 needs #33/#34; #13 needs #26/#27/#28; #1 needs all Phase 1 work and verified exit evidence. |

Issue #21 belongs to Phase 6 full recovery; #22 belongs to Phase 2 audit design. Neither blocks Phase 1. The older #34 requirement for an already accepted #22 design is replaced by a handoff that schedules that design with its first consumers in Phase 2.

Coordinate ownership before starting parallel work; serialize overlapping file edits if necessary. Recheck candidates against current `develop` before merging. A passing feature branch does not prove its integration with another parallel change.

### Tracer bullets

1. **Boot and read a stored Page.** Initialize a local site through the CLI, start the binary, and request a Page rendered from SQLite. Check the response after restart and a useful error for an incompatible database version.
2. **Edit through to public output.** Use a minimal local admin form to save a Page draft, preview it, and publish it. Check that a second draft edit leaves the public response unchanged and that publishing records an immutable snapshot.
3. **Prove configuration round-trip.** Export the minimal Page definition, validate it, and use it to initialize a second disposable site whose generated form can publish a Page. Reject an invalid definition before changing storage. This initial setup path becomes the explicit deployment path in phase 6.

Exit evidence: A draft edit leaves the published Page unchanged; publishing replaces the public version and records an immutable snapshot; data survives restart. Exported configuration initializes a second site whose generated form can publish a Page, and invalid configuration changes no storage. The retained local/hosted phase gate covers the real browser journey and negative cases. Record acceptance mappings, decisions, prototype limits and a Phase 2 task breakdown and estimate without opening Phase 2 implementation tickets.

Dependencies: None. Establishes the basis for FR-03, FR-05, FR-06, FR-08, FR-15, FR-16, and FR-18.

Decisions: The [Page state, storage and runtime contract](phase1/content-contract.md) (P1-01) fixes the Go toolchain and SQLite driver, package boundaries, IDs, draft and snapshot states, URL ownership, version checks, the CLI and HTTP routes, and the later-phase rules for relationships, files, menus, Trash, and redirects.

## Phase 2. Secure access and establish the admin

Outcome: Real users can enter the admin with the correct permissions, and sensitive actions have the required controls.

- Add setup and server CLI commands, first-administrator creation, and account recovery without email.
- Build sign-in, sign-out, secure sessions, CSRF controls, invitations, password resets through SMTP, and the PRD's advisory password warnings with explicit confirmation.
- Enforce independent administrator and editor roles in direct requests and CLI operations. Preserve the last active administrator, revoke deactivated users' access, and retain attribution.
- Define the bounded audit contract in #22 with its first authentication consumers. Build shared admin navigation, site naming and branding, validation patterns, and the administrator-only audit log. Record covered actions, including failures, without secrets.
- Establish safe upload handling before editorial file fields depend on it: allowed types, size limits, image re-encoding, safe storage and download behavior. Complete editorial media controls in phase 4.
- Have Mark review authentication, authorization, session and upload controls before dependent production workflows proceed. Review later security-sensitive changes as they arise.

### Tracer bullets

1. **Initialize, sign in and protect a draft.** Create the first administrator through the CLI, sign in through the admin, then sign out. Verify session and CSRF controls on the Page journey, rejected unauthorized preview requests, and a recorded sign-in outcome visible only to administrators.
2. **Invite and recover an account.** Send an invitation through a local SMTP capture service, follow it to create an account, and complete a password reset. Check expiry, token reuse, advisory password confirmation and CLI recovery without email.
3. **Exercise each role on a real action.** Assign roles, publish a Page as an editor, and change site branding as an administrator. Test forbidden direct requests, deactivation revoking an existing session, preserved attribution and protection of the last active administrator through both UI and CLI.
4. **Upload and retrieve one safe image.** Through a protected Page field, upload and retrieve a re-encoded image. Prove the PRD's pure-Go codec choice with CGO_ENABLED=0. Reject disguised/oversized files, excessive dimensions before full decoding, and unauthorized uploads; assert metadata removal and server-generated names. Add bounded validator fuzz tests and one-worker image processing. Review with auth before broadening support.
5. **Deploy the protected Page to Hetzner.** After access-control review, deploy behind an IP allowlist or outer authentication using real systemd/Caddy/TLS templates. Configure separate service users, MemoryMax and CPUQuota. Exercise publish, restart, filesystem permissions, readiness failure and structured error logs; run a short uncached/cached smoke load without CDN. Record host size, memory, CPU and latency. Phase 6 hardens this deployment rather than introducing it.

Exit evidence: Direct-request tests cover administrator-only, editor-only, combined-role, deactivated and unauthenticated users. Invitation and reset tokens expire and cannot be reused. Upload controls reject invalid input. Human review findings are resolved. Tests cover per-account/IP throttling before hashing, SMTP and audit writes; generic reset responses; session revocation after password/role changes; bounded counted failure logging and 90-day retention. CLI recovery creates an audit entry, and init grants both roles. The protected host smoke run has recorded evidence. Activate govulncheck, staticcheck and bounded fuzz gates as their components arrive.

Dependencies: Phase 1. Covers FR-01, FR-02, FR-09 and the foundation of FR-07, FR-16 and FR-18.

## Phase 3. Build configurable content and the publishing journey

Outcome: An administrator defines content in the interface, and an editor creates, finds, previews and publishes it without code changes.

- Build the content type and field builder, including labels, groups, ordering, help text, required state, validation, and supported single or multiple values.
- Add the non-file launch fields: short, long and rich text; email; links; relationships; timezone-aware date and time; integer and decimal numbers; yes or no; single and multiple choice; phone; structured address and optional coordinates.
- Generate usable list, edit and preview screens. Include title search and combined content-type and publication-status filters across types. Preserve entered work when validation fails.
- Complete private drafts and previews, direct publishing, unpublishing, publish-only snapshot history, and restoration of a snapshot as a new draft. Handle model changes without silently dropping historical data.
- Provide the five launch content types and barebones public templates. Select and review the pinned prebuilt rich-text editor and sanitizer before this phase; no inline images, and internal links use item IDs. Implement the PRD's bounded generation-keyed cache, including stale in-flight render tests and invalidation on every public-affecting change.

### Tracer bullets

1. **Define a type and publish its first item.** An administrator adds a simple field to a type in the UI; an editor fills the generated form, saves, previews and publishes it. Check required-field errors preserve input and that model changes preserve existing content.
2. **Find, revise and restore an item.** Search and combine filters, open a published item, publish a revision, restore the earlier snapshot as a draft and unpublish. Assert the public response at every step, private preview access and cache invalidation. Retain phase 1's stale-revision checks for save/publish and extend them to restore; two-editor conflicts preserve submitted work.
3. **Publish an Event linked to a Location.** Define and use timezone-aware date/time, address, coordinates and relationship fields through the builder, editor and public theme. Check invalid values, DST gaps and ambiguous times. Preview uses only this Event's draft, published related content and published menus; unpublished related items stay hidden.
4. **Complete the launch field and type matrix.** Add the remaining non-file fields in small groups, each with a builder-to-form-to-published-page check. Finish Events, Locations, Blog posts, News and Pages. Include rich-text output safety and snapshot restoration after a model change.

Exit evidence: An editor completes the core publishing journey for all five types. A new administrator-defined type gets usable screens without code. Draft changes and restored snapshots stay private until publication; unpublishing removes public access.

Dependencies: Phase 2. Covers FR-03, FR-05, FR-06, FR-13, the basic editing portion of FR-04, and most of FR-08 and FR-14.

## Phase 4. Complete images, documents and file retention

Outcome: Editors can add the media each content type needs and keep older published versions intact.

- Add field-based JPEG, PNG, WebP, PDF and DOCX uploads, useful errors, image previews, and document downloads.
- Apply the lower of global and category limits, with the PRD defaults of 25 MiB global, 10 MiB images and 25 MiB documents.
- Let administrators define named resize-to-fit and resize-and-crop styles. Give each image one shared focal point and each placement alternative text or an explicit decorative choice.
- Preserve original files, generate replaceable variants, and make replacement create a new stored file. Retain files referenced by drafts, snapshots or Trash. Use immutable content-addressed storage and separate recovery-safe GC under the storage/export lock; follow the ADR's retention and rollback pins.

### Tracer bullets

1. **Publish an image with two styles.** Define named styles, upload to a content field, set its focal point and placement text, then preview and publish. Verify both generated crops and the alternative-text or decorative choice in rendered output.
2. **Replace an image without breaking history.** Replace and publish a file, then restore an older snapshot. Verify both originals remain usable while referenced and variants can regenerate. Remove the last reference on a disposable item; verify no immediate deletion, retention-window expiry, rollback pins, reference recreation and failed-change safety. Race GC against full export when phase 6 lands.
3. **Publish every allowed file format.** Exercise JPEG, PNG, WebP, PDF and DOCX from upload through public rendering or download. Check configured global/category limits, image dimensions, metadata removal and useful errors. Fuzz image and DOCX validators; reject ZIP traversal, excessive expanded bytes/ratios/entry counts, macros and invalid OOXML. Assert attachment and nosniff headers for documents. Choose and record numeric DOCX expansion limits before implementing the validator. Extend retention checks to Trash when phase 5 makes that workflow available.

Exit evidence: One original generates multiple styles using its focal point. Replacing an image does not break a past snapshot. Retention and cleanup checks cover drafts, snapshots and Trash, including failed content changes.

Dependencies: Phases 2 and 3. Completes FR-07 and FR-14; extends FR-06 and FR-08.

## Phase 5. Finish navigation, redirects and content removal

Outcome: Editors can maintain a complete public site, including changing URLs and removing content without hidden side effects.

- Add configurable menu definitions and editorial menu drafts with labels, internal or external targets, hierarchy and order. Preview and publish each whole menu atomically, with publish-only snapshots and restoration to draft.
- Build the top-level Redirects screen, automatic permanent redirects on published-path changes, and manually managed permanent or temporary internal and external redirects.
- Reject URL conflicts and loops. Suspend redirects to unpublished or trashed items while reserving enabled source paths; resume them on republication. Keep manually disabled rules disabled.
- Complete Trash and restore as unpublished, without automatic expiry. Show administrators every dependent menu item, redirect and relationship affected by permanent deletion, then apply the confirmed cleanup together.
- Preserve immutable historical snapshots. When restoring a snapshot that references a permanently deleted item, omit the missing link and warn the editor.

### Tracer bullets

1. **Publish and restore a complete menu.** An administrator defines a menu; an editor drafts its links, hierarchy and order, previews it and publishes. Check that public navigation changes together and snapshot restoration remains private until publication. Hide unpublished/trashed targets publicly, flag them in the menu editor, and restore their links on republication.
2. **Change a URL and follow its lifecycle.** Publish an item, change its path, follow the automatic redirect, unpublish and republish. Check suspension, reserved source paths and resumption. Add manual internal and external rules, with checks for loops, conflicts and rules that stay manually disabled.
3. **Trash and restore a connected item.** Trash content referenced by a menu, relationship, redirect and image snapshot, then restore it. Check public removal, retention of files and unpublished restoration without automatic expiry.
4. **Permanently delete with visible consequences.** Show the administrator the complete dependency preview, cancel once, then confirm. Verify atomic cleanup, editor denial, audit recording and file retention. Restore a surviving historical snapshot and check its missing-reference warning without altering history.

Exit evidence: Demonstrate a menu publication, URL change, unpublish, republication, trash, restore and permanent deletion. Verify old URLs cannot expose private content, cancellation changes nothing, and the deletion preview matches the committed effects.

Dependencies: Phases 3 and 4. Completes FR-04, FR-08, FR-12 and FR-17 and checks their interactions with FR-05 and FR-06.

## Phase 6. Harden deployment and full recovery

Outcome: Mark can promote reviewed configuration and recover a complete site using supported commands.

- Complete configuration export, validation and diff commands for content types, fields, menus, image styles and other site settings. Active configuration lives in SQLite; exported versioned files support Git review. Keep deployment secrets external.
- Build explicit deployment that validates and compares configuration, rejects silent content loss, takes a database backup, applies the change, starts the release and checks health. Ordinary startup only reads and checks active configuration.
- Define the full recovery manifest in #21 with its first export/restore consumers. Ship full versioned ZIP recovery exports with a consistent SQLite backup followed by a pinned immutable file set and verified manifest. Portable partial export/import remains post-v1.
- Restore into a fresh directory after validating versions, hashes and references; preserve the active-administrator invariant. Data-changing commands show the intended effect and useful script errors. Keep deployment secrets external.
- Supply Caddy and systemd templates for independent site services, health and error reporting, and deployment and rollback instructions. Rehearse consistent database-and-file recovery. Document the operator's responsibility for scheduled, off-host backups.

### Tracer bullets

1. **Deploy a configuration change to a running site.** Export and diff a field change, deploy it with a database backup, and use the changed form to publish content. Reject content-losing changes and unexpected active revisions before mutation; verify ordinary restart does not import configuration and production UI/direct configuration mutations fail. Cover every allowed/rejected model transformation in the ADR, including retained snapshots.
2. **Export while files change.** Replace an image and remove its last live reference while full export and GC contend. Restore the package and assert all referenced files and historical images render. Inject interrupted copies and invalid hashes; incomplete packages never become successful exports or replace the live site.
3. **Recover a complete site.** Create a full export containing accounts, configuration, snapshots, Trash and files, restore it into a clean installation, sign in and exercise those workflows. Check consistency, the active-administrator invariant, externally supplied secrets and audit coverage.
4. **Deploy, fail and roll back one isolated service.** Run two sites with deployment templates, update one, inject a deployment failure and restore its prior release and database. Check health reporting, resource limits and continued operation of the other site. Replace an image after the rollback snapshot, then roll back and verify the old image renders. Add post-deploy publishes: database rollback must warn of lost work and capture current state before explicit confirmation. Prove compatible binary-only rollback preserves editorial work.

Exit evidence: Rehearse a fresh installation, a safe configuration change, rejection of a destructive change, failed deployment and rollback. Verify full recovery, variant regeneration and export/GC consistency. Restarting one site leaves another running. Audit entries cover deployments, exports and relevant CLI actions.

Dependencies: Phases 1 through 5. Completes FR-10, FR-11, FR-15 and FR-16; completes operational coverage of FR-18.

## Phase 7. Validate and release v1

Outcome: All P0 requirements have evidence on a fresh installation and Mark can accept the release.

- Run the PRD journeys across all five types and review every FR-01 through FR-18 acceptance condition. Close gaps discovered between features.
- Check the admin and public theme in current stable Chrome, Firefox, Safari and Edge on desktop, plus Safari on iOS and Chrome on Android. Verify clear actions, responsive forms, retained input after validation errors, keyboard operation, labelled controls and visible focus. Run axe checks on critical admin and bundled-theme journeys.
- Complete human review of security-sensitive code and deployed configuration, including changes since the earlier review. Rehearse full restore and deployment rollback on the release candidate.
- On the intended Hetzner host, sustain 200 cached requests per second for 10 minutes with p95 at or below 250 ms, and 50 uncached requests per second for 10 minutes with p95 at or below 500 ms. Both runs must have no application errors. Record host size, page mix and cache state; disable CDN and proxy response caching. Add mixed public load with publish/uploads and a two-site contention run; verify response correctness and no stale content after mutations.
- Increase load beyond the baseline and document the first observed bottleneck with measurements. Fix it for v1 if it prevents the required baseline.
- Finish editor and operator instructions and record the release evidence and remaining post-v1 work.

### Tracer bullets

1. **Rehearse a fresh site's working life.** Initialize a release candidate, configure it, invite an editor, publish all five types with media and navigation, revise URLs and exercise removal and recovery. Attach results to every P0 acceptance condition and fix gaps through focused bullets.
2. **Repeat critical journeys across browsers.** Run the create, validation-error, preview, publish and upload journeys on the required desktop and mobile browsers. Fix and recheck each observed failure through the affected journey.
3. **Measure the deployed public site.** Seed representative published pages on the intended host and run the cached and uncached baselines, checking response correctness as well as latency and errors. Increase load and record the first bottleneck.
4. **Accept the release candidate.** Complete human review, including independent auth/upload review before client launch, repeat deployment and recovery rehearsals on the candidate, and verify that the documented setup yields a working new site. Record Mark's acceptance against the release evidence.

Exit evidence: Mark accepts the completed P0 checklist, security review, browser checks, performance results and recovery rehearsals. Release a version that can initialize and serve a new site.

Dependencies: Phase 6. Validates all functional and quality requirements.

## Sequencing and risks

The main sequence is 1 → 2 → 3 → 4 → 5 → 6 → 7. Page storage and deployment safety boundaries start in phase 1. Audit recording and the first protected host deployment/load smoke test arrive in phase 2. Define detailed contracts when their first feature arrives. Phase 6 proves recovery and deployment interactions. Security, validation and usability are part of each phase; phase 7 verifies the finished release.

The most consequential implementation risks are snapshot compatibility after model changes, relationships across deletion and restoration, file retention across every content state, URL reservations and redirect chains, and consistent database-and-file recovery. Follow the ADR's data-preservation boundaries and settle feature-specific rules when each feature arrives.

Before phase 3 choose the rich-text bundle and sanitizer; before the phase 2 host test record host size and page mix. Concurrent edits, model-change rules and configuration ownership are already fixed by the Page contract and ADR. Partial import semantics are deferred with that feature. Calendar estimates need phase 1 evidence and available capacity; the architecture document's older day estimates are not commitments.

## Boundary after v1

Keep portable database/file export-import, scheduled publishing, a reusable media library, event sourcing and formal WCAG 2.2 AA audit work on the post-v1 roadmap. The keyboard, labels, focus and axe baseline is required in v1. Postgres, a plugin ecosystem, shared databases, a general-purpose site builder, visitor-write-heavy features and migration of a specific existing site are outside this release.
