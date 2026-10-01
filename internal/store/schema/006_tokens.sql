CREATE TABLE tokens (
  id           TEXT PRIMARY KEY,
  purpose      TEXT NOT NULL CHECK (purpose IN ('invitation', 'password_reset')),
  token_digest BLOB NOT NULL UNIQUE CHECK (typeof(token_digest) = 'blob' AND length(token_digest) = 32),
  account_id   TEXT REFERENCES accounts(id),
  email        TEXT CHECK (email IS NULL OR email = lower(trim(email))),
  created_at   TEXT NOT NULL,
  expires_at   TEXT NOT NULL,
  used_at      TEXT,
  CHECK (
    (purpose = 'invitation' AND account_id IS NULL AND email IS NOT NULL)
    OR
    (purpose = 'password_reset' AND account_id IS NOT NULL AND email IS NULL)
  )
) STRICT;

CREATE INDEX tokens_account_id_idx ON tokens(account_id);
