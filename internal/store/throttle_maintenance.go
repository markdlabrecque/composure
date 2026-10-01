package store

import (
	"context"
	"errors"
	"time"
)

type storeWorker struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type ThrottleOption func(*throttleMaintenanceConfig)

type throttleMaintenanceConfig struct {
	owner    context.Context
	ownerSet bool
	ticks    <-chan time.Time
	interval time.Duration
}

// WithThrottleMaintenance uses caller-owned ticks as the cleanup schedule and
// cancels the worker when owner is canceled. Each tick value is the cleanup time.
// The tick channel remains owned by the caller and is never closed here.
func WithThrottleMaintenance(owner context.Context, ticks <-chan time.Time) ThrottleOption {
	return func(config *throttleMaintenanceConfig) {
		config.owner = owner
		config.ownerSet = true
		config.ticks = ticks
	}
}

// MaintenanceDone reports when the store-owned maintenance worker has exited.
func (t *Throttle) MaintenanceDone() <-chan struct{} {
	if t == nil || t.worker == nil {
		return nil
	}
	return t.worker.done
}

func startThrottleMaintenance(store *Store, limiter *Throttle, config throttleMaintenanceConfig) error {
	if config.owner == nil {
		return errors.New("throttle maintenance requires a context")
	}
	ctx, cancel := context.WithCancel(store.lifecycle)
	stopOwner := context.AfterFunc(config.owner, cancel)
	worker := &storeWorker{cancel: cancel, done: make(chan struct{})}
	limiter.worker = worker
	if err := store.registerWorker(worker); err != nil {
		stopOwner()
		cancel()
		return err
	}
	go func() {
		defer close(worker.done)
		defer stopOwner()
		defer cancel()
		if config.ticks != nil {
			for {
				select {
				case <-ctx.Done():
					return
				case at, ok := <-config.ticks:
					if !ok {
						return
					}
					_, _ = store.db.ExecContext(ctx, `DELETE FROM throttle_counters WHERE window_expires_at <= ?`, at.UTC().Truncate(time.Millisecond).Format(throttleTimestampLayout))
				}
			}
		}
		ticker := time.NewTicker(config.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case at := <-ticker.C:
				_, _ = store.db.ExecContext(ctx, `DELETE FROM throttle_counters WHERE window_expires_at <= ?`, at.UTC().Truncate(time.Millisecond).Format(throttleTimestampLayout))
			}
		}
	}()
	return nil
}
