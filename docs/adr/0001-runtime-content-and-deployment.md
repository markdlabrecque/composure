# Runtime content and deployment boundaries

Use JSON field documents per draft and immutable publication snapshot, with stable field IDs and transactional reference/path indexes, as already proposed in the Page contract. Runtime field builders must not issue DDL; application schema migrations remain code-owned. This avoids table-per-type migrations and EAV query complexity while keeping publication history intact.

Production configuration is deploy-only. Administrators edit configuration in development or staging, export it for Git review, then explicitly deploy it. SQLite holds the active deployed copy, not a second independently editable production source. This sacrifices live configuration editing to avoid three-way merges and accidental overwrites.

## Model changes

- Labels, ordering and help text may change without rewriting values. Renaming a label never changes its field ID.
- Adding an optional field is safe. Adding a required field or tightening validation requires a preflight report of affected drafts and published items; reject deployment until current values comply or an explicit reviewed migration supplies valid values.
- Removing a populated field or type, changing a field's type, or changing single/multiple cardinality is rejected in v1. Empty-field removal is allowed only after checking drafts, all retained snapshots and Trash. No implicit coercion or truncation.
- Keep prior configuration revisions for historical rendering. Snapshot inspection uses its recorded definition; restore validates against the active definition and reports incompatibilities without mutating the draft or snapshot. Missing deleted relationships follow the existing warning-and-omit rule.
- Add relationship and file indexes when their features arrive. Maintain them in the same transaction as the JSON document. Historical references remain distinguishable from live references.

## Deployment checks

A reviewed deploy plan records the target configuration hash and expected active revision/hash. Apply checks them again under the maintenance/write lock and refuses drift; it never merges or overwrites unexpected changes. Production rejects configuration mutations through UI and direct requests. The environment mode is an operator setting, not editable site configuration.

Prefer additive migrations and binary-only rollback when compatible. Database rollback enters maintenance mode, reports writes since the snapshot, requires explicit acknowledgement of their loss, and first captures a recovery export of the current state. Never silently discard post-deploy editorial work.

## Files and recovery

Files use hashes of validated stored bytes as server-generated names and are immutable. Finish and durably store a file before committing its database reference; a failed transaction may leave an orphan, never a committed missing file. A single per-site storage lock coordinates reference creation, export and garbage collection.

Garbage collection is separate from content transactions. It requires no draft, snapshot or Trash references, no retained rollback-snapshot references, and an unreferenced age beyond the configured recovery window, default 30 days. Reset the age if a reference is recreated. Operators must retain rollback pins as long as the corresponding database snapshot is supported. Disable GC if the retained recovery set cannot be established; age alone is not proof of safety.

Full export pins files against GC, takes a consistent SQLite backup using the backup API rather than copying a live WAL database, then copies immutable files and verifies manifest hashes and referenced-file completeness before releasing the pin. Extra unreferenced files are harmless. Full exports are self-contained; off-host database-only backups are not supported recovery artifacts. Restore validates into a fresh directory before cutover.
