# Phase 1 format contract index

This page points to the accepted Page format and validation contracts. Those documents define the behavior; this index records how they fit together and where implementation work continues.

| Concern | Accepted source |
| --- | --- |
| Page configuration shape, stable IDs, field order, and generated form | [Page configuration v1](page-config-v1.md) and its [default](examples/page-config-default.json) and [custom](examples/page-config-custom.json) examples |
| Validation errors, canonical export bytes, and explicit new-site initialization | [Validation and export contract](config-validation.md) and its [negative fixture matrix](config-validation.md#fixture-matrix) |
| SQLite storage, version checks, startup, and init plan/apply behavior | [Phase 1 content contract](content-contract.md), especially [storage](content-contract.md#4-site-directory-and-storage) and [versions, startup, and initialization](content-contract.md#8-versions-startup-and-initialization) |
| Production deployment and safe model changes | [Runtime and deployment ADR](../adr/0001-runtime-content-and-deployment.md) |
| Product scope and phase sequencing | [PRD](../prd.md) and the [current work plan](../work_plan.md#phase-1-prove-the-foundation-and-publishing-model) |

The file's `format_version`, SQLite `user_version`, `site.config_format_version`, and `active_config.revision` are separate markers. The file contains the Page definition only. Validation reads the file without opening a site. Export reads the active SQLite document and writes the requested configuration file without changing site state. Init validates before writes; a plan without `--apply` changes nothing. Ordinary restart checks stored versions and never imports a configuration file.

## Parent #3 acceptance trace

| Parent condition | Contract and example evidence | Review status |
| --- | --- | --- |
| Minimal and customized Page definitions parse and produce the expected field form | [Page configuration v1](page-config-v1.md#page-fields-and-generated-form), [default example](examples/page-config-default.json), [custom example](examples/page-config-custom.json) | Parsed and hand-checked in the [#23 evidence report](../reports/2026-09-30-ticket-23.md) |
| Invalid IDs, versions, kinds, field types, missing properties, and references are covered | [Fixture matrix](config-validation.md#fixture-matrix). Phase 1 has no reference property, so a reference fixture is inapplicable; full reference integrity belongs to Phase 6/#21. | Mark's resolution and author check are in the [#23 evidence report](../reports/2026-09-30-ticket-23.md) |
| Configuration examples exclude content, accounts, secrets, and recovery data | [Closed Page format](page-config-v1.md#document-shape) and [export exclusions](config-validation.md#end-to-end-path). `seed-page.json` is separate init example content, not a configuration example or export. | Checked against the examples and storage contract in the [#23 evidence report](../reports/2026-09-30-ticket-23.md) |
| #19 and #20 receive combined stronger review | The accepted format and validation contracts above, with the integration trace in the [#23 evidence report](../reports/2026-09-30-ticket-23.md). | Independent verdict and resolutions are retained with the #23 PR/issue completion evidence; this index is not an approval. |
| #11 and #12 can implement without choosing format or conflict policy | [Validation, export, and init rules](config-validation.md), plus stored active configuration and startup rules in the [content contract](content-contract.md). | Resolved behavior and future implementation limits are recorded in the [#23 evidence report](../reports/2026-09-30-ticket-23.md) |

## Implementation handoff and deferred work

| Work | Boundary |
| --- | --- |
| [#4](https://github.com/markdlabrecque/composure/issues/4) | Boot and serve the default Page from SQLite; ordinary restart does not import a file. |
| [#11](https://github.com/markdlabrecque/composure/issues/11) | Implement validation and deterministic export from `active_config.document`, including the documented error and atomic replacement rules. |
| [#12](https://github.com/markdlabrecque/composure/issues/12) | Initialize a second site from the exported definition without copying editorial content; its normal round trip omits `--example`. |
| Phase 2 / #22 | Design and implement audit records with their first consumers. This does not block the Page format. |
| Phase 6 / #21 | Define full recovery and reference-integrity checks. Phase 1 has no relationship or file-reference fields. |

Production configuration remains deploy-only. Expected active revision/hash checks, drift rejection, preflight, and model-change application belong to a future deploy command under the ADR. Phase 1 does not add runtime DDL, deployment, a recovery ZIP, or configuration import on restart.
