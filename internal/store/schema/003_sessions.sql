CREATE TABLE sessions (
  id           TEXT PRIMARY KEY,
  account_id   TEXT NOT NULL REFERENCES accounts(id),
  token_digest BLOB NOT NULL UNIQUE CHECK (typeof(token_digest) = 'blob' AND length(token_digest) = 32),
  created_at   TEXT NOT NULL,
  expires_at   TEXT NOT NULL,
  revoked_at   TEXT
) STRICT;

CREATE INDEX sessions_account_id_idx ON sessions(account_id);
