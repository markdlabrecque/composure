CREATE TABLE media (
  id           TEXT PRIMARY KEY,
  storage_name TEXT NOT NULL,
  kind         TEXT NOT NULL,
  size         INTEGER NOT NULL,
  width        INTEGER,
  height       INTEGER,
  created_by   TEXT NOT NULL
) STRICT;

CREATE TRIGGER media_no_update BEFORE UPDATE ON media
BEGIN SELECT RAISE(ABORT, 'media metadata is immutable'); END;
