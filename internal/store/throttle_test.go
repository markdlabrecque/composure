package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"time"
)

// Proposed digest-only API for #77:
//
//	type AccountThrottleDigest [32]byte
//	type IPThrottleDigest [32]byte
//	NewThrottle(*Store, capacity int) (*Throttle, error)
//	(*Throttle).Admit(ctx, namespace string, account AccountThrottleDigest,
//	    ip IPThrottleDigest, at time.Time) (bool, error)
//	(*Throttle).Cleanup(ctx, at time.Time) error
//
// Capacity counts subject rows, not request pairs. Cleanup is callable by an
// idle-time scheduler without admitting a request. These tests do not select a
// production capacity or scheduler. Callers own exact CTHK key derivation and
// stable namespace constants; the library accepts no raw subjects or outcomes.
const throttleTestNamespace = "test_access"

func throttleTestAt() time.Time {
	return time.Date(2026, 9, 30, 12, 0, 0, 456000000, time.UTC)
}

func throttleTestKeys(n int) (AccountThrottleDigest, IPThrottleDigest) {
	// Synthetic opaque digests, not an alternative subject-key encoding.
	return AccountThrottleDigest(sha256.Sum256([]byte(fmt.Sprintf("opaque-account-%d", n)))),
		IPThrottleDigest(sha256.Sum256([]byte(fmt.Sprintf("opaque-ip-%d", n))))
}

func throttleTestLimiter(t *testing.T, s *Store, capacity int) *Throttle {
	t.Helper()
	limiter, err := NewThrottle(s, capacity)
	if err != nil {
		t.Fatal(err)
	}
	return limiter
}

func throttleTestAdmit(t *testing.T, limiter *Throttle, namespace string, account AccountThrottleDigest, ip IPThrottleDigest, at time.Time, want bool) {
	t.Helper()
	got, err := limiter.Admit(context.Background(), namespace, account, ip, at)
	if err != nil || got != want {
		t.Fatalf("admission=%t, want %t; error=%v", got, want, err)
	}
}

type throttleTestRow struct {
	namespace, subject, digest, started, expires, next string
	count                                              int
}

func throttleTestRows(t *testing.T, s *Store) []throttleTestRow {
	t.Helper()
	rows, err := s.db.Query(`SELECT namespace,subject_type,hex(key_digest),window_started_at,
		window_expires_at,attempt_count,next_allowed_at FROM throttle_counters
		ORDER BY namespace,subject_type,key_digest`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var result []throttleTestRow
	for rows.Next() {
		var row throttleTestRow
		if err := rows.Scan(&row.namespace, &row.subject, &row.digest, &row.started, &row.expires, &row.count, &row.next); err != nil {
			t.Fatal(err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func throttleTestChanges(t *testing.T, s *Store) int64 {
	t.Helper()
	var n int64
	if err := s.db.QueryRow("SELECT total_changes()").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func throttleTestUnchanged(t *testing.T, s *Store, before []throttleTestRow, changes int64) {
	t.Helper()
	if !reflect.DeepEqual(throttleTestRows(t, s), before) {
		t.Fatal("rejection changed persisted counters")
	}
	if got := throttleTestChanges(t, s); got != changes {
		t.Fatalf("rejection performed SQLite mutations: total_changes increased by %d", got-changes)
	}
}

func throttleTestPair(t *testing.T, s *Store, count int, started, next time.Time) {
	t.Helper()
	rows := throttleTestRows(t, s)
	if len(rows) != 2 || rows[0].subject != "account" || rows[1].subject != "ip" {
		t.Fatal("admission must persist one independent account row and one IP row")
	}
	format := func(at time.Time) string { return at.UTC().Format("2006-01-02T15:04:05.000Z") }
	for _, row := range rows {
		if row.namespace != throttleTestNamespace || row.count != count || row.started != format(started) ||
			row.expires != format(started.Add(15*time.Minute)) || row.next != format(next) {
			t.Fatalf("counter state differs: count=%d, start=%s, expiry=%s, next=%s", row.count, row.started, row.expires, row.next)
		}
	}
}

func TestThrottlePersistsSuppliedDigestBytes(t *testing.T) {
	s, _ := accountSite(t)
	limiter := throttleTestLimiter(t, s, 2)
	account, ip := throttleTestKeys(1)
	throttleTestAdmit(t, limiter, throttleTestNamespace, account, ip, throttleTestAt(), true)
	for _, tc := range []struct {
		subject string
		digest  [32]byte
	}{{"account", [32]byte(account)}, {"ip", [32]byte(ip)}} {
		var digest []byte
		var storageType string
		if err := s.db.QueryRow(`SELECT key_digest,typeof(key_digest) FROM throttle_counters
			WHERE namespace=? AND subject_type=?`, throttleTestNamespace, tc.subject).Scan(&digest, &storageType); err != nil {
			t.Fatal(err)
		}
		if storageType != "blob" || len(digest) != 32 || !bytes.Equal(digest, tc.digest[:]) {
			t.Fatal("counter must persist the supplied 32-byte digest as a BLOB")
		}
	}
}

func TestThrottleBackoffThresholdsAndCountClamp(t *testing.T) {
	s, _ := accountSite(t)
	limiter := throttleTestLimiter(t, s, 2)
	account, ip := throttleTestKeys(1)
	started := throttleTestAt()
	at := started
	// Each entry is the delay assigned AFTER that admission, including every
	// inclusive threshold and several admissions beyond the persisted clamp.
	delays := []time.Duration{
		0, 0, 0, 0, 0,
		time.Second, time.Second, time.Second, time.Second, time.Second,
		5 * time.Second, 5 * time.Second, 5 * time.Second, 5 * time.Second, 5 * time.Second,
		15 * time.Second, 15 * time.Second, 15 * time.Second, 15 * time.Second, 15 * time.Second,
		60 * time.Second, 60 * time.Second, 60 * time.Second, 60 * time.Second,
	}
	for i, delay := range delays {
		throttleTestAdmit(t, limiter, throttleTestNamespace, account, ip, at, true)
		count := i + 1
		if count > 21 {
			count = 21
		}
		next := at.Add(delay)
		throttleTestPair(t, s, count, started, next)
		if delay > 0 {
			before, changes := throttleTestRows(t, s), throttleTestChanges(t, s)
			throttleTestAdmit(t, limiter, throttleTestNamespace, account, ip, next.Add(-time.Millisecond), false)
			throttleTestUnchanged(t, s, before, changes)
		}
		at = next // Exact cooldown boundary must admit, without sleeping.
	}
}

func TestThrottleFixedWindowExpiryCapsDelayAndSurvivesRestart(t *testing.T) {
	s, path := accountSite(t)
	limiter := throttleTestLimiter(t, s, 2)
	account, ip := throttleTestKeys(1)
	started := throttleTestAt()
	at := started
	for attempt := 1; attempt <= 20; attempt++ {
		throttleTestAdmit(t, limiter, throttleTestNamespace, account, ip, at, true)
		switch {
		case attempt >= 16:
			at = at.Add(15 * time.Second)
		case attempt >= 11:
			at = at.Add(5 * time.Second)
		case attempt >= 6:
			at = at.Add(time.Second)
		}
	}
	expires := started.Add(15 * time.Minute)
	throttleTestAdmit(t, limiter, throttleTestNamespace, account, ip, expires.Add(-30*time.Second), true)
	throttleTestPair(t, s, 21, started, expires)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	limiter = throttleTestLimiter(t, reopened, 2)
	before, changes := throttleTestRows(t, reopened), throttleTestChanges(t, reopened)
	for _, offset := range []time.Duration{29 * time.Second, time.Second, time.Millisecond} {
		throttleTestAdmit(t, limiter, throttleTestNamespace, account, ip, expires.Add(-offset), false)
	}
	throttleTestUnchanged(t, reopened, before, changes)
	throttleTestAdmit(t, limiter, throttleTestNamespace, account, ip, expires, true)
	throttleTestPair(t, reopened, 1, expires, expires)
}

func TestThrottleSubjectsAndNamespacesAreIndependent(t *testing.T) {
	for _, delayed := range []string{"account", "ip"} {
		t.Run(delayed, func(t *testing.T) {
			s, _ := accountSite(t)
			limiter := throttleTestLimiter(t, s, 64)
			account, ip := throttleTestKeys(1)
			started := throttleTestAt()
			for i := 0; i < 6; i++ {
				throttleTestAdmit(t, limiter, throttleTestNamespace, account, ip, started, true)
			}
			before, changes := throttleTestRows(t, s), throttleTestChanges(t, s)
			for i := 2; i < 18; i++ {
				freshAccount, freshIP := throttleTestKeys(i)
				if delayed == "account" {
					freshAccount = account
				} else {
					freshIP = ip
				}
				throttleTestAdmit(t, limiter, throttleTestNamespace, freshAccount, freshIP, started, false)
			}
			// In particular, rejection must not allocate a row for the other,
			// never-seen subject, or write even an unchanged value back to SQLite.
			throttleTestUnchanged(t, s, before, changes)
			throttleTestAdmit(t, limiter, "test_other", account, ip, started, true)
			// The same digest bytes in opposite subject roles cannot collide.
			throttleTestAdmit(t, limiter, throttleTestNamespace, AccountThrottleDigest(ip), IPThrottleDigest(account), started, true)
			rows := throttleTestRows(t, s)
			if len(rows) != 6 {
				t.Fatalf("subject/namespace separation persisted %d rows, want 6", len(rows))
			}
			for _, row := range rows {
				if row.count != 1 && row.count != 6 {
					t.Fatal("independent counters have an unexpected count")
				}
			}
		})
	}
}

func TestThrottleOneWindowRollsOverWithoutResettingTheOther(t *testing.T) {
	s, _ := accountSite(t)
	limiter := throttleTestLimiter(t, s, 4)
	account, ip := throttleTestKeys(1)
	_, laterIP := throttleTestKeys(2)
	started := throttleTestAt()
	for i := 0; i < 6; i++ {
		throttleTestAdmit(t, limiter, throttleTestNamespace, account, ip, started, true)
	}
	later := started.Add(14 * time.Minute)
	throttleTestAdmit(t, limiter, throttleTestNamespace, account, laterIP, later, true)
	expires := started.Add(15 * time.Minute)
	throttleTestAdmit(t, limiter, throttleTestNamespace, account, laterIP, expires, true)
	format := func(at time.Time) string { return at.UTC().Format("2006-01-02T15:04:05.000Z") }
	foundAccount, foundIP := false, false
	for _, row := range throttleTestRows(t, s) {
		if row.subject == "account" {
			foundAccount = true
			if row.count != 1 || row.started != format(expires) || row.expires != format(expires.Add(15*time.Minute)) {
				t.Fatal("expired account window did not roll over independently")
			}
		}
		if row.subject == "ip" && row.started == format(later) {
			foundIP = true
			if row.count != 2 || row.expires != format(later.Add(15*time.Minute)) || row.next != format(expires) {
				t.Fatal("account rollover reset or extended the still-active IP window")
			}
		}
	}
	if !foundAccount || !foundIP {
		t.Fatal("independent account/IP rows missing after rollover")
	}
}

func TestThrottleDelayedPeerRejectsWithoutRefreshingExpiredCounter(t *testing.T) {
	s, _ := accountSite(t)
	limiter := throttleTestLimiter(t, s, 4)
	account, ip := throttleTestKeys(1)
	otherAccount, otherIP := throttleTestKeys(2)
	started := throttleTestAt()
	throttleTestAdmit(t, limiter, throttleTestNamespace, account, ip, started, true)
	expires := started.Add(15 * time.Minute)
	later := expires.Add(-500 * time.Millisecond)
	for i := 0; i < 6; i++ {
		throttleTestAdmit(t, limiter, throttleTestNamespace, otherAccount, otherIP, later, true)
	}
	before, changes := throttleTestRows(t, s), throttleTestChanges(t, s)
	throttleTestAdmit(t, limiter, throttleTestNamespace, account, otherIP, expires, false)
	throttleTestUnchanged(t, s, before, changes)
	throttleTestAdmit(t, limiter, throttleTestNamespace, account, otherIP, later.Add(time.Second), true)
	for _, row := range throttleTestRows(t, s) {
		if row.subject == "ip" && row.started == later.UTC().Format("2006-01-02T15:04:05.000Z") && row.count != 7 {
			t.Fatal("rejected expired-account request changed the active IP count")
		}
	}
}

func TestThrottleCapacityFailsClosedWithoutActiveEviction(t *testing.T) {
	s, _ := accountSite(t)
	limiter := throttleTestLimiter(t, s, 3)
	account, ip := throttleTestKeys(1)
	started := throttleTestAt()
	for i := 0; i < 6; i++ {
		throttleTestAdmit(t, limiter, throttleTestNamespace, account, ip, started, true)
	}
	before, changes := throttleTestRows(t, s), throttleTestChanges(t, s)
	throttleTestAdmit(t, limiter, throttleTestNamespace, account, ip, started, false)
	throttleTestUnchanged(t, s, before, changes)
	// One remaining slot cannot reserve a new pair. A flood must not evict
	// existing active keys, allocate one half, or cause per-request writes.
	for i := 2; i < 34; i++ {
		freshAccount, freshIP := throttleTestKeys(i)
		throttleTestAdmit(t, limiter, throttleTestNamespace, freshAccount, freshIP, started.Add(time.Second), false)
	}
	throttleTestUnchanged(t, s, before, changes)
	// Existing keys still work at capacity; capacity pressure cannot reset them.
	throttleTestAdmit(t, limiter, throttleTestNamespace, account, ip, started.Add(time.Second), true)
	throttleTestPair(t, s, 7, started, started.Add(2*time.Second))
	freshAccount, freshIP := throttleTestKeys(100)
	expires := started.Add(15 * time.Minute)
	throttleTestAdmit(t, limiter, throttleTestNamespace, freshAccount, freshIP, expires, true)
	throttleTestPair(t, s, 1, expires, expires)
}

func TestThrottleCleanupExpiresCountersDuringIdle(t *testing.T) {
	s, _ := accountSite(t)
	limiter := throttleTestLimiter(t, s, 4)
	account, ip := throttleTestKeys(1)
	otherAccount, otherIP := throttleTestKeys(2)
	started := throttleTestAt()
	throttleTestAdmit(t, limiter, throttleTestNamespace, account, ip, started, true)
	throttleTestAdmit(t, limiter, throttleTestNamespace, otherAccount, otherIP, started.Add(time.Minute), true)
	// Exercise the cleanup entry point with no admissions at all during idle.
	expires := started.Add(15 * time.Minute)
	for _, tc := range []struct {
		at   time.Time
		want int
	}{{expires.Add(-time.Millisecond), 4}, {expires, 2}, {expires.Add(time.Minute), 0}} {
		if err := limiter.Cleanup(context.Background(), tc.at); err != nil {
			t.Fatal(err)
		}
		rows := throttleTestRows(t, s)
		if len(rows) != tc.want {
			t.Fatalf("idle cleanup left %d rows, want %d", len(rows), tc.want)
		}
		for _, row := range rows {
			if row.count != 1 {
				t.Fatal("cleanup changed an active count")
			}
		}
	}
}

func TestThrottleReservationRollsBackOnSQLiteFailure(t *testing.T) {
	for _, action := range []string{"INSERT", "UPDATE"} {
		t.Run(action, func(t *testing.T) {
			s, _ := accountSite(t)
			limiter := throttleTestLimiter(t, s, 2)
			account, ip := throttleTestKeys(1)
			at := throttleTestAt()
			if action == "UPDATE" {
				throttleTestAdmit(t, limiter, throttleTestNamespace, account, ip, at, true)
			}
			before := throttleTestRows(t, s)
			_, err := s.db.Exec(`CREATE TRIGGER throttle_test_failure BEFORE ` + action + ` ON throttle_counters
				WHEN NEW.subject_type='ip' BEGIN SELECT RAISE(ABORT, 'test reservation failure'); END`)
			if err != nil {
				t.Fatal(err)
			}
			admitted, err := limiter.Admit(context.Background(), throttleTestNamespace, account, ip, at)
			if err == nil || admitted {
				t.Fatal("SQLite reservation failure must not report admission")
			}
			if !reflect.DeepEqual(before, throttleTestRows(t, s)) {
				t.Fatal("failed reservation committed one half of the pair")
			}
		})
	}
}

func TestThrottleConcurrentAdmissionAcrossStores(t *testing.T) {
	for _, capacityRace := range []bool{false, true} {
		t.Run(fmt.Sprintf("capacity_race_%t", capacityRace), func(t *testing.T) {
			s, path := accountSite(t)
			other, err := Open(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			capacity, wantAdmissions := 64, 6
			if capacityRace {
				capacity, wantAdmissions = 2, 1
			}
			limiters := []*Throttle{throttleTestLimiter(t, s, capacity), throttleTestLimiter(t, other, capacity)}
			type result struct {
				admitted bool
				err      error
			}
			results := make(chan result, 16)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := 0; i < 16; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					n := 1
					if capacityRace {
						n = i + 1
					}
					account, ip := throttleTestKeys(n)
					<-start
					admitted, err := limiters[i%2].Admit(context.Background(), throttleTestNamespace, account, ip, throttleTestAt())
					results <- result{admitted, err}
				}(i)
			}
			close(start)
			wg.Wait()
			close(results)
			admissions := 0
			for result := range results {
				if result.err != nil {
					t.Fatal(result.err)
				}
				if result.admitted {
					admissions++
				}
			}
			if admissions != wantAdmissions {
				t.Fatalf("concurrent admissions=%d, want %d", admissions, wantAdmissions)
			}
			next := throttleTestAt()
			if !capacityRace {
				next = next.Add(time.Second)
			}
			throttleTestPair(t, s, wantAdmissions, throttleTestAt(), next)
		})
	}
}

func TestThrottleDoesNotLogCounterDigests(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)
	s, _ := accountSite(t)
	limiter := throttleTestLimiter(t, s, 2)
	account, ip := throttleTestKeys(1)
	at := throttleTestAt()
	for i := 0; i < 6; i++ {
		throttleTestAdmit(t, limiter, throttleTestNamespace, account, ip, at, true)
	}
	throttleTestAdmit(t, limiter, throttleTestNamespace, account, ip, at, false)
	freshAccount, freshIP := throttleTestKeys(2)
	throttleTestAdmit(t, limiter, throttleTestNamespace, freshAccount, freshIP, at.Add(time.Second), false)
	if _, err := s.db.Exec(`CREATE TRIGGER throttle_test_log_failure BEFORE UPDATE ON throttle_counters
		BEGIN SELECT RAISE(ABORT, 'test reservation failure'); END`); err != nil {
		t.Fatal(err)
	}
	if admitted, err := limiter.Admit(context.Background(), throttleTestNamespace, account, ip, at.Add(time.Second)); admitted || err == nil {
		t.Fatal("expected reservation failure")
	}
	for _, digest := range [][32]byte{[32]byte(account), [32]byte(ip), [32]byte(freshAccount), [32]byte(freshIP)} {
		for _, encoding := range []string{hex.EncodeToString(digest[:]), fmt.Sprintf("%X", digest),
			base64.StdEncoding.EncodeToString(digest[:]), base64.RawURLEncoding.EncodeToString(digest[:]), fmt.Sprint(digest)} {
			if bytes.Contains(output.Bytes(), []byte(encoding)) || bytes.Contains(output.Bytes(), digest[:]) {
				t.Fatal("counter digest leaked into logs")
			}
		}
	}
}
