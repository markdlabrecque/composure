CREATE TABLE audit_events (
  id           TEXT PRIMARY KEY,
  time         TEXT NOT NULL,
  action       TEXT NOT NULL,
  actor        TEXT,
  target_kind  TEXT,
  target_id    TEXT,
  outcome      TEXT NOT NULL,
  count        INTEGER NOT NULL,
  failure_code TEXT,
  first_time   TEXT,
  last_time    TEXT,
  CHECK ((target_kind IS NULL AND target_id IS NULL) OR
         (target_kind IS NOT NULL AND target_id IS NOT NULL)),
  CHECK ((first_time IS NULL AND last_time IS NULL) OR
         (first_time IS NOT NULL AND last_time IS NOT NULL))
) STRICT;

CREATE INDEX audit_events_time_id_idx ON audit_events(time, id);
