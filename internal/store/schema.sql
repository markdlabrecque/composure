CREATE TABLE site (
  singleton             INTEGER PRIMARY KEY CHECK (singleton = 1),
  site_id               TEXT NOT NULL,
  created_at            TEXT NOT NULL,
  config_format_version INTEGER NOT NULL
) STRICT;

CREATE TABLE active_config (
  singleton  INTEGER PRIMARY KEY CHECK (singleton = 1),
  revision   INTEGER NOT NULL CHECK (revision >= 1),
  document   TEXT NOT NULL CHECK (json_valid(document)),
  applied_at TEXT NOT NULL,
  applied_by TEXT NOT NULL
) STRICT;

CREATE TABLE items (
  id                      TEXT PRIMARY KEY,
  type_id                 TEXT NOT NULL,   -- "page" in phase 1; a type ID from the active config
  title                   TEXT NOT NULL,   -- working draft title
  path                    TEXT NOT NULL,   -- working draft path, normalized
  fields                  TEXT NOT NULL CHECK (json_valid(fields)),  -- working draft field values
  draft_revision          INTEGER NOT NULL CHECK (draft_revision >= 1),
  created_at              TEXT NOT NULL,
  created_by              TEXT NOT NULL,
  updated_at              TEXT NOT NULL,
  updated_by              TEXT NOT NULL,
  published_snapshot_id   TEXT REFERENCES snapshots(id)   -- NULL until first publish
) STRICT;

CREATE TABLE snapshots (
  id                    TEXT PRIMARY KEY,
  item_id               TEXT NOT NULL REFERENCES items(id),
  seq                   INTEGER NOT NULL CHECK (seq >= 1),
  type_id               TEXT NOT NULL,
  config_revision       INTEGER NOT NULL,   -- active_config.revision when published
  source_draft_revision INTEGER NOT NULL,   -- items.draft_revision that was published
  title                 TEXT NOT NULL,
  path                  TEXT NOT NULL,
  fields                TEXT NOT NULL CHECK (json_valid(fields)),
  published_at          TEXT NOT NULL,
  published_by          TEXT NOT NULL,
  UNIQUE (item_id, seq)
) STRICT;

CREATE TABLE routes (
  path       TEXT PRIMARY KEY,   -- normalized public path; primary key is the uniqueness rule
  kind       TEXT NOT NULL CHECK (kind IN ('item')),   -- 'redirect' added in phase 5
  item_id    TEXT NOT NULL UNIQUE REFERENCES items(id),
  claimed_at TEXT NOT NULL
) STRICT;

CREATE TRIGGER snapshots_no_update BEFORE UPDATE ON snapshots
BEGIN SELECT RAISE(ABORT, 'snapshots are immutable'); END;

CREATE TRIGGER snapshots_no_delete BEFORE DELETE ON snapshots
BEGIN SELECT RAISE(ABORT, 'snapshots are immutable'); END;
