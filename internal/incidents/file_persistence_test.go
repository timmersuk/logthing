package incidents

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestFilePersistenceRestoresPendingRecovery(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "incidents.json")
	persistence := NewFilePersistence(path)
	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	cfg := Config{DownAfter: time.Minute, RecoveredAfter: time.Minute}

	before, err := New(cfg, persistence)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	observe(t, before, wanMessage("router-a", "offline", start, "down"), true)
	if err := before.Tick(context.Background(), start.Add(time.Minute)); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	observe(t, before, wanMessage("router-a", "online", start.Add(70*time.Second), "up"), true)

	after, err := New(cfg, persistence)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := after.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := singleIncident(t, after).State; got != StatePendingRecovery {
		t.Fatalf("restored state = %q, want %q", got, StatePendingRecovery)
	}
	if err := after.Tick(context.Background(), start.Add(130*time.Second)); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if got := singleIncident(t, after).State; got != StateResolved {
		t.Fatalf("state after restored timer = %q, want %q", got, StateResolved)
	}
}

func TestFilePersistenceMissingFileLoadsEmptyState(t *testing.T) {
	t.Parallel()

	persistence := NewFilePersistence(filepath.Join(t.TempDir(), "missing", "incidents.json"))
	state, err := persistence.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if state.Version != 0 {
		t.Fatalf("version = %d, want empty state", state.Version)
	}
}

func TestFilePersistenceRestoresPendingFailureAndActiveRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "incidents.json")
	persistence := NewFilePersistence(path)
	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	cfg := Config{DownAfter: time.Minute, RecoveredAfter: time.Minute}
	pending, _ := New(cfg, persistence)
	observe(t, pending, wanMessage("router-a", "offline", start, "down"), true)
	restoredPending, _ := New(cfg, persistence)
	if err := restoredPending.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := singleIncident(t, restoredPending).State; got != StatePendingFailure {
		t.Fatalf("state = %q", got)
	}
	if err := restoredPending.Tick(context.Background(), start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	job := restoredPending.PendingNotifications()[0]
	retryAt := start.Add(3 * time.Minute)
	if err := restoredPending.MarkNotificationFailed(context.Background(), job.ID, "temporary", retryAt, false); err != nil {
		t.Fatal(err)
	}
	restoredActive, _ := New(cfg, persistence)
	if err := restoredActive.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := singleIncident(t, restoredActive).State; got != StateActive {
		t.Fatalf("state = %q", got)
	}
	jobs := restoredActive.PendingNotifications()
	if len(jobs) != 1 || jobs[0].Attempts != 1 || !jobs[0].NextAt.Equal(retryAt) {
		t.Fatalf("restored jobs = %#v", jobs)
	}
}
