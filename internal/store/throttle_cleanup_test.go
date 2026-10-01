package store

import (
	"context"
	"reflect"
	"testing"
	"testing/synctest"
	"time"
)

// Proposed additive maintenance seam for #77:
//
//	NewThrottle(*Store, capacity int, ...ThrottleOption) (*Throttle, error)
//	WithThrottleMaintenance(context.Context, <-chan time.Time) ThrottleOption
//	(*Throttle).MaintenanceDone() <-chan struct{}
//
// NewThrottle starts maintenance, including without options. The option replaces
// its ticker with a caller-owned channel and supplies an additional cancellation
// context. Each injected tick's timestamp is the cleanup clock, not time.Now.
// The worker must not close the injected channel. No default interval is chosen
// here. Existing constructor calls remain valid and use a production ticker.
//
// MaintenanceDone closes only after the worker exits. Store.Close cancels and
// joins every registered worker before returning; canceling the option's context
// also stops that worker without closing the store. Cleanup SQL must use the
// worker's cancelable context. These tests never start a loop themselves or call
// Cleanup. synctest.Wait synchronizes worker progress without sleeps or polling.

func throttleCleanupLimiter(t *testing.T, s *Store, ctx context.Context, capacity int) (*Throttle, chan time.Time) {
	t.Helper()
	ticks := make(chan time.Time, 1)
	limiter, err := NewThrottle(s, capacity, WithThrottleMaintenance(ctx, ticks))
	if err != nil {
		t.Fatal(err)
	}
	throttleCleanupRunning(t, limiter)
	return limiter, ticks
}

func throttleCleanupRunning(t *testing.T, limiter *Throttle) {
	t.Helper()
	if limiter.MaintenanceDone() == nil {
		t.Fatal("constructor must expose the lifetime of its maintenance worker")
	}
	select {
	case <-limiter.MaintenanceDone():
		t.Fatal("maintenance stopped before its owner closed or canceled")
	default:
	}
}

func throttleCleanupStopped(t *testing.T, limiter *Throttle) {
	t.Helper()
	select {
	case <-limiter.MaintenanceDone():
	default:
		t.Fatal("maintenance worker has not exited")
	}
}

func throttleCleanupTick(ticks chan time.Time, at time.Time) {
	ticks <- at
	synctest.Wait()
}

func TestThrottleMaintenanceAutomaticallyExpiresIdleCounters(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _ := accountSite(t)
		limiter, ticks := throttleCleanupLimiter(t, s, context.Background(), 6)
		account, ip := throttleTestKeys(1)
		activeAccount, activeIP := throttleTestKeys(2)
		started := throttleTestAt()
		throttleTestAdmit(t, limiter, throttleTestNamespace, account, ip, started, true)
		throttleTestAdmit(t, limiter, "test_other", account, ip, started, true)
		for i := 0; i < 6; i++ {
			throttleTestAdmit(t, limiter, throttleTestNamespace, activeAccount, activeIP, started.Add(time.Minute), true)
		}
		before, changes := throttleTestRows(t, s), throttleTestChanges(t, s)
		var active []throttleTestRow
		for _, row := range before {
			if row.count == 6 {
				active = append(active, row)
			}
		}
		if len(active) != 2 {
			t.Fatal("fixture must contain an active account/IP pair with backoff state")
		}

		// No more admissions or manual cleanup after the fixture is seeded.
		expires := started.Add(15 * time.Minute)
		throttleCleanupTick(ticks, expires.Add(-time.Millisecond))
		if !reflect.DeepEqual(throttleTestRows(t, s), before) || throttleTestChanges(t, s) != changes {
			t.Fatal("maintenance mutated counters before their fixed windows expired")
		}
		throttleCleanupTick(ticks, expires)
		if !reflect.DeepEqual(throttleTestRows(t, s), active) {
			t.Fatal("idle maintenance must delete expired counters across namespaces and preserve every active field")
		}
		if got := throttleTestChanges(t, s) - changes; got != 4 {
			t.Fatalf("maintenance performed %d row mutations, want only the four expired deletions", got)
		}
		throttleCleanupTick(ticks, expires.Add(time.Minute))
		if got := len(throttleTestRows(t, s)); got != 0 {
			t.Fatalf("idle maintenance left %d expired counters", got)
		}
		throttleCleanupRunning(t, limiter)
	})
}

func TestThrottleMaintenanceStopsOnCancellationWithoutClosingStore(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _ := accountSite(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		limiter, ticks := throttleCleanupLimiter(t, s, ctx, 2)
		account, ip := throttleTestKeys(1)
		throttleTestAdmit(t, limiter, throttleTestNamespace, account, ip, throttleTestAt(), true)
		before, changes := throttleTestRows(t, s), throttleTestChanges(t, s)
		cancel()
		synctest.Wait()
		throttleCleanupStopped(t, limiter)
		throttleCleanupTick(ticks, throttleTestAt().Add(15*time.Minute))
		if !reflect.DeepEqual(throttleTestRows(t, s), before) || throttleTestChanges(t, s) != changes {
			t.Fatal("canceled maintenance processed a later tick or closed the owner's store")
		}
	})
}

func TestThrottleMaintenanceStoreCloseJoinsEveryConstructorWorker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, path := accountSite(t)
		first, firstTicks := throttleCleanupLimiter(t, s, context.Background(), 2)
		second, secondTicks := throttleCleanupLimiter(t, s, context.Background(), 2)
		// Exercise the unchanged constructor too. Automatic maintenance must
		// not exist only when a test explicitly supplies its tick source.
		defaultLimiter := throttleTestLimiter(t, s, 2)
		throttleCleanupRunning(t, defaultLimiter)
		account, ip := throttleTestKeys(1)
		throttleTestAdmit(t, first, throttleTestNamespace, account, ip, throttleTestAt(), true)
		before := throttleTestRows(t, s)
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		// Close itself must join, rather than merely request eventual shutdown.
		for _, limiter := range []*Throttle{first, second, defaultLimiter} {
			throttleCleanupStopped(t, limiter)
		}
		throttleCleanupTick(firstTicks, throttleTestAt().Add(15*time.Minute))
		throttleCleanupTick(secondTicks, throttleTestAt().Add(15*time.Minute))
		reopened, err := Open(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()
		if !reflect.DeepEqual(throttleTestRows(t, reopened), before) {
			t.Fatal("closed store's maintenance processed a later tick")
		}
	})
}

func TestThrottleMaintenanceCancellationInterruptsPendingCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _ := accountSite(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		limiter, ticks := throttleCleanupLimiter(t, s, ctx, 2)
		account, ip := throttleTestKeys(1)
		throttleTestAdmit(t, limiter, throttleTestNamespace, account, ip, throttleTestAt(), true)
		before := throttleTestRows(t, s)
		// Store's one connection is held so cleanup waits in database/sql.
		// Cancellation must terminate the worker without releasing this lease.
		conn, err := s.db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		waits := s.db.Stats().WaitCount
		throttleCleanupTick(ticks, throttleTestAt().Add(15*time.Minute))
		if s.db.Stats().WaitCount <= waits {
			t.Fatal("constructor worker did not attempt scheduled cleanup")
		}
		cancel()
		synctest.Wait()
		throttleCleanupStopped(t, limiter)
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(throttleTestRows(t, s), before) {
			t.Fatal("canceled pending cleanup later mutated counters")
		}
	})
}

func TestThrottleMaintenanceRetriesOnLaterTickAfterSQLiteFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _ := accountSite(t)
		limiter, ticks := throttleCleanupLimiter(t, s, context.Background(), 2)
		account, ip := throttleTestKeys(1)
		throttleTestAdmit(t, limiter, throttleTestNamespace, account, ip, throttleTestAt(), true)
		before := throttleTestRows(t, s)
		if _, err := s.db.Exec(`CREATE TRIGGER throttle_cleanup_test_failure BEFORE DELETE ON throttle_counters
			WHEN OLD.subject_type='ip' BEGIN SELECT RAISE(ABORT, 'test cleanup failure'); END`); err != nil {
			t.Fatal(err)
		}
		expires := throttleTestAt().Add(15 * time.Minute)
		throttleCleanupTick(ticks, expires)
		if !reflect.DeepEqual(throttleTestRows(t, s), before) {
			t.Fatal("failed scheduled cleanup committed partial deletions")
		}
		throttleCleanupRunning(t, limiter)
		if _, err := s.db.Exec("DROP TRIGGER throttle_cleanup_test_failure"); err != nil {
			t.Fatal(err)
		}
		throttleCleanupTick(ticks, expires)
		if got := len(throttleTestRows(t, s)); got != 0 {
			t.Fatalf("scheduled cleanup did not recover after SQLite failure: %d rows remain", got)
		}
	})
}

func TestThrottleMaintenanceClosedDatabaseErrorDoesNotPreventOwnerShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _ := accountSite(t)
		limiter, ticks := throttleCleanupLimiter(t, s, context.Background(), 2)
		// Simulate the database becoming unavailable independently of its owner.
		if err := s.db.Close(); err != nil {
			t.Fatal(err)
		}
		throttleCleanupTick(ticks, throttleTestAt().Add(15*time.Minute))
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		throttleCleanupStopped(t, limiter)
	})
}
