package site_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/site"
)

// The clock is called after the lock and temporary database exist. Canceling
// there causes a real SQLite initialization error, without a persistence mock.
func TestConfigInitTicket12MidInitCleanup(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, concurrent := range []bool{false, true} {
			name := "absent"
			if existing {
				name = "empty"
			}
			if concurrent {
				name += "/concurrent"
			}
			t.Run(name, func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "site")
				if existing {
					if err := os.Mkdir(dir, 0755); err != nil {
						t.Fatal(err)
					}
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				called := false
				now := func() time.Time {
					called = true
					entries, err := os.ReadDir(dir)
					if err != nil {
						t.Fatal(err)
					}
					temporary, lock := false, false
					for _, entry := range entries {
						temporary = temporary || strings.HasPrefix(entry.Name(), "composure.db.init-")
						lock = lock || entry.Name() == ".composure-init.lock"
					}
					if !temporary || !lock {
						t.Fatal("failure injection did not reach temporary database and lock")
					}
					if _, err := os.Stat(filepath.Join(dir, "composure.db")); !os.IsNotExist(err) {
						t.Fatal("database visible before initialization committed")
					}
					if concurrent {
						if err := os.WriteFile(filepath.Join(dir, "concurrent.txt"), []byte("preserve this"), 0600); err != nil {
							t.Fatal(err)
						}
					}
					cancel()
					return time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
				}
				if err := site.Init(ctx, dir, true, true, io.Discard, now); err == nil {
					t.Fatal("injected cancellation unexpectedly initialized site")
				}
				if !called {
					t.Fatal("failure occurred before intended injection")
				}
				if !existing && !concurrent {
					if _, err := os.Stat(dir); !os.IsNotExist(err) {
						t.Errorf("new failed site directory remains: %v", err)
					}
					return
				}
				entries, err := os.ReadDir(dir)
				if err != nil {
					t.Fatal("removed existing or concurrently occupied directory:", err)
				}
				if concurrent {
					if len(entries) != 1 || entries[0].Name() != "concurrent.txt" {
						t.Errorf("cleanup left owned artifacts or removed canary: %v", entries)
					}
					value, err := os.ReadFile(filepath.Join(dir, "concurrent.txt"))
					if err != nil || string(value) != "preserve this" {
						t.Errorf("concurrent data changed: %q %v", value, err)
					}
				} else if len(entries) != 0 {
					t.Errorf("existing empty directory contains failed-init artifacts: %v", entries)
				}
			})
		}
	}
}

type init12PlanWriter struct {
	dir      string
	occupied bool
}

func (w *init12PlanWriter) Write(p []byte) (int, error) {
	// Occupy the previously empty destination after the final plan line, before
	// apply obtains its lock and rechecks the destination.
	if !w.occupied && strings.Contains(string(p), "example content:") {
		if err := os.WriteFile(filepath.Join(w.dir, "concurrent.txt"), []byte("untouched"), 0600); err != nil {
			return 0, err
		}
		w.occupied = true
	}
	return len(p), nil
}
func TestConfigInitTicket12RechecksTargetDuringApply(t *testing.T) {
	dir := t.TempDir()
	writer := &init12PlanWriter{dir: dir}
	called := false
	err := site.Init(context.Background(), dir, false, true, writer, func() time.Time { called = true; return time.Now() })
	if err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("apply did not reject concurrent occupation: %v", err)
	}
	if !writer.occupied {
		t.Fatal("injection never occupied destination")
	}
	if called {
		t.Error("apply created temporary database before refusing occupied destination")
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil || len(entries) != 1 || entries[0].Name() != "concurrent.txt" {
		t.Fatalf("apply damaged target or left artifacts: %v %v", entries, readErr)
	}
	value, readErr := os.ReadFile(filepath.Join(dir, "concurrent.txt"))
	if readErr != nil || string(value) != "untouched" {
		t.Errorf("apply altered concurrent file: %q %v", value, readErr)
	}
}
