package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// AccountThrottleDigest is an opaque, caller-derived account throttle key.
type AccountThrottleDigest [32]byte

// IPThrottleDigest is an opaque, caller-derived IP throttle key.
type IPThrottleDigest [32]byte

const throttleWindow = 15 * time.Minute
const throttleTimestampLayout = "2006-01-02T15:04:05.000Z"

// Throttle atomically reserves account and IP counters in the store.
type Throttle struct {
	store    *Store
	capacity int
	worker   *storeWorker
}

// NewThrottle constructs a throttle backed by this store.
func (s *Store) NewThrottle(capacity int, options ...ThrottleOption) (*Throttle, error) {
	return NewThrottle(s, capacity, options...)
}

// NewThrottle constructs a throttle whose capacity bounds active subject rows
// and starts its idle-counter maintenance worker.
func NewThrottle(store *Store, capacity int, options ...ThrottleOption) (*Throttle, error) {
	if store == nil || store.db == nil {
		return nil, errors.New("throttle requires an open store")
	}
	if capacity < 2 {
		return nil, errors.New("throttle capacity must allow at least an account/IP pair")
	}
	config := throttleMaintenanceConfig{interval: time.Minute}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("throttle option must not be nil")
		}
		option(&config)
	}
	if config.ownerSet && config.owner == nil {
		return nil, errors.New("throttle maintenance context must not be nil")
	}
	if config.owner == nil {
		config.owner = context.Background()
	}
	limiter := &Throttle{store: store, capacity: capacity}
	if err := startThrottleMaintenance(store, limiter, config); err != nil {
		return nil, err
	}
	return limiter, nil
}

type throttleCounter struct {
	found   bool
	started time.Time
	expires time.Time
	next    time.Time
	count   int
}

// Admit atomically increments both counters if neither subject is delayed and
// both rows fit within the configured active-row capacity. It persists only
// opaque digests; callers derive those keys and select stable namespaces.
func (t *Throttle) Admit(ctx context.Context, namespace string, account AccountThrottleDigest, ip IPThrottleDigest, at time.Time) (bool, error) {
	if namespace == "" || len(namespace) > 64 {
		return false, errors.New("throttle namespace must contain 1 to 64 bytes")
	}
	for _, b := range []byte(namespace) {
		if !((b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '-') {
			return false, errors.New("throttle namespace must be ASCII")
		}
	}
	now := at.UTC().Truncate(time.Millisecond)
	nowText := now.Format(throttleTimestampLayout)
	tx, err := t.store.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	accountCounter, err := readThrottleCounter(ctx, tx, namespace, "account", account[:])
	if err != nil {
		return false, err
	}
	ipCounter, err := readThrottleCounter(ctx, tx, namespace, "ip", ip[:])
	if err != nil {
		return false, err
	}
	if accountCounter.found && now.Before(accountCounter.expires) && now.Before(accountCounter.next) ||
		ipCounter.found && now.Before(ipCounter.expires) && now.Before(ipCounter.next) {
		return false, nil
	}

	var activeRows int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM throttle_counters WHERE window_expires_at > ?`, nowText).Scan(&activeRows); err != nil {
		return false, err
	}
	neededRows := 0
	if !accountCounter.found || !now.Before(accountCounter.expires) {
		neededRows++
	}
	if !ipCounter.found || !now.Before(ipCounter.expires) {
		neededRows++
	}
	if activeRows+neededRows > t.capacity {
		return false, nil
	}

	// Expired rows are reclaimed only when the new pair can be fully admitted.
	// Thus rejections never mutate storage, even at a capacity boundary.
	if _, err = tx.ExecContext(ctx, `DELETE FROM throttle_counters WHERE window_expires_at <= ?`, nowText); err != nil {
		return false, err
	}
	if err = reserveThrottleCounter(ctx, tx, namespace, "account", account[:], accountCounter, now); err != nil {
		return false, err
	}
	if err = reserveThrottleCounter(ctx, tx, namespace, "ip", ip[:], ipCounter, now); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// Cleanup removes counters whose fixed window has ended. It is safe to call
// independently of Admit, including during periods with no incoming traffic.
func (t *Throttle) Cleanup(ctx context.Context, at time.Time) error {
	nowText := at.UTC().Truncate(time.Millisecond).Format(throttleTimestampLayout)
	_, err := t.store.db.ExecContext(ctx, `DELETE FROM throttle_counters WHERE window_expires_at <= ?`, nowText)
	return err
}

func readThrottleCounter(ctx context.Context, tx *sql.Tx, namespace, subject string, digest []byte) (throttleCounter, error) {
	var counter throttleCounter
	var started, expires, next string
	err := tx.QueryRowContext(ctx, `SELECT window_started_at,window_expires_at,attempt_count,next_allowed_at
		FROM throttle_counters WHERE namespace=? AND subject_type=? AND key_digest=?`, namespace, subject, digest).
		Scan(&started, &expires, &counter.count, &next)
	if errors.Is(err, sql.ErrNoRows) {
		return counter, nil
	}
	if err != nil {
		return counter, err
	}
	counter.started, err = time.Parse(throttleTimestampLayout, started)
	if err != nil {
		return counter, fmt.Errorf("parse throttle window start: %w", err)
	}
	counter.expires, err = time.Parse(throttleTimestampLayout, expires)
	if err != nil {
		return counter, fmt.Errorf("parse throttle window expiry: %w", err)
	}
	counter.next, err = time.Parse(throttleTimestampLayout, next)
	if err != nil {
		return counter, fmt.Errorf("parse throttle deadline: %w", err)
	}
	counter.found = true
	return counter, nil
}

func reserveThrottleCounter(ctx context.Context, tx *sql.Tx, namespace, subject string, digest []byte, previous throttleCounter, now time.Time) error {
	count, started, expires := 1, now, now.Add(throttleWindow)
	if previous.found && now.Before(previous.expires) {
		count = previous.count + 1
		if count > 21 {
			count = 21
		}
		started, expires = previous.started, previous.expires
	}
	delay := time.Duration(0)
	switch {
	case count >= 21:
		delay = time.Minute
	case count >= 16:
		delay = 15 * time.Second
	case count >= 11:
		delay = 5 * time.Second
	case count >= 6:
		delay = time.Second
	}
	next := now.Add(delay)
	if next.After(expires) {
		next = expires
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO throttle_counters
		(namespace,subject_type,key_digest,window_started_at,window_expires_at,attempt_count,next_allowed_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(namespace,subject_type,key_digest) DO UPDATE SET
		window_started_at=excluded.window_started_at,window_expires_at=excluded.window_expires_at,
		attempt_count=excluded.attempt_count,next_allowed_at=excluded.next_allowed_at`,
		namespace, subject, digest, started.UTC().Format(throttleTimestampLayout),
		expires.UTC().Format(throttleTimestampLayout), count, next.UTC().Format(throttleTimestampLayout))
	return err
}
