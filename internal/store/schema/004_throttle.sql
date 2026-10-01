CREATE TABLE throttle_counters (
  namespace         TEXT NOT NULL CHECK (length(namespace) BETWEEN 1 AND 64),
  subject_type      TEXT NOT NULL CHECK (subject_type IN ('account', 'ip')),
  key_digest        BLOB NOT NULL CHECK (length(key_digest) = 32),
  window_started_at TEXT NOT NULL,
  window_expires_at  TEXT NOT NULL,
  attempt_count     INTEGER NOT NULL CHECK (attempt_count BETWEEN 1 AND 21),
  next_allowed_at   TEXT NOT NULL,
  PRIMARY KEY (namespace, subject_type, key_digest),
  CHECK (window_expires_at > window_started_at),
  CHECK (next_allowed_at >= window_started_at),
  CHECK (next_allowed_at <= window_expires_at)
) STRICT;

CREATE INDEX throttle_counters_expiry_idx
  ON throttle_counters(window_expires_at);
