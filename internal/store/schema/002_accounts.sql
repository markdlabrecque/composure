CREATE TABLE accounts (
  id                TEXT PRIMARY KEY,
  email             TEXT NOT NULL UNIQUE CHECK (email = lower(trim(email))),
  password_hash     TEXT NOT NULL,
  is_administrator  INTEGER NOT NULL CHECK (is_administrator IN (0, 1)),
  is_editor         INTEGER NOT NULL CHECK (is_editor IN (0, 1)),
  state             TEXT NOT NULL CHECK (state IN ('active', 'deactivated')),
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL,
  CHECK (is_administrator = 1 OR is_editor = 1)
) STRICT;
