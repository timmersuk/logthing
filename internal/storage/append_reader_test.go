package storage

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/timmersuk/logthing/internal/model"
)

func TestFileStoreVisitAfterResumesAtCompleteLine(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	received := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	first := model.Message{ID: "one", ReceivedAt: received, Hostname: "router-a", Message: "first"}
	second := model.Message{ID: "two", ReceivedAt: received.Add(time.Second), Hostname: "router-a", Message: "second"}
	if err := store.Append(context.Background(), first); err != nil {
		t.Fatalf("Append(first) error = %v", err)
	}

	var visited []string
	cursors, err := store.VisitAfter(context.Background(), nil, func(record StoredRecord) error {
		visited = append(visited, record.Message.ID)
		return nil
	})
	if err != nil {
		t.Fatalf("VisitAfter() error = %v", err)
	}
	if len(visited) != 1 || visited[0] != "one" {
		t.Fatalf("visited = %#v, want [one]", visited)
	}

	if err := store.Append(context.Background(), second); err != nil {
		t.Fatalf("Append(second) error = %v", err)
	}
	visited = nil
	next, err := store.VisitAfter(context.Background(), cursors, func(record StoredRecord) error {
		visited = append(visited, record.Message.ID)
		return nil
	})
	if err != nil {
		t.Fatalf("VisitAfter(resume) error = %v", err)
	}
	if len(visited) != 1 || visited[0] != "two" {
		t.Fatalf("visited after resume = %#v, want [two]", visited)
	}
	if len(next) != 1 {
		t.Fatalf("cursor count = %d, want 1", len(next))
	}
}

func TestFileStoreVisitAfterLeavesPartialLinePending(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	path := filepath.Join(root, "2026", "09", "18", "router.ndjson")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"id":"partial"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	count := 0
	cursors, err := store.VisitAfter(context.Background(), nil, func(record StoredRecord) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("VisitAfter() error = %v", err)
	}
	if count != 0 {
		t.Fatalf("visited %d records, want 0", count)
	}
	if got := cursors[filepath.ToSlash(filepath.Join("2026", "09", "18", "router.ndjson"))]; got != 0 {
		t.Fatalf("cursor = %d, want 0", got)
	}
}

func TestFileStoreVisitAfterReadsPartitionsOldestFirst(t *testing.T) {
	t.Parallel()
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	newer := model.Message{ID: "newer", ReceivedAt: time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)}
	older := model.Message{ID: "older", ReceivedAt: time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)}
	if err := store.Append(context.Background(), newer); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(context.Background(), older); err != nil {
		t.Fatal(err)
	}
	var visited []string
	_, err = store.VisitAfter(context.Background(), nil, func(record StoredRecord) error {
		visited = append(visited, record.Message.ID)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(visited, []string{"older", "newer"}) {
		t.Fatalf("visited = %v", visited)
	}
}
