package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/timmersuk/logthing/internal/model"
)

func TestFileStoreQueryReturnsLatestFirst(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	oldTime := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)

	for _, msg := range []model.Message{
		{ID: "old", ReceivedAt: oldTime, Hostname: "router-1", Message: "first"},
		{ID: "new", ReceivedAt: newTime, Hostname: "router-2", Message: "second"},
	} {
		if err := store.Append(context.Background(), msg); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	got, err := store.Query(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("len(Query()) = %d, want 2", len(got))
	}
	if got[0].ID != "new" || got[1].ID != "old" {
		t.Fatalf("Query() order = [%s %s], want [new old]", got[0].ID, got[1].ID)
	}
}

func TestFileStoreAppendSplitsFilesBySource(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	receivedAt := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	for _, msg := range []model.Message{
		{ID: "one", ReceivedAt: receivedAt, Source: "10.0.0.1:5514", Message: "first"},
		{ID: "two", ReceivedAt: receivedAt, Source: "edge-two.example:5514", Message: "second"},
	} {
		if err := store.Append(context.Background(), msg); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	for _, filename := range []string{"10.0.0.1.ndjson", "edge-two.example.ndjson"} {
		path := filepath.Join(root, "2026", "06", "19", filename)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected source partition %s: %v", path, err)
		}
	}
}

func TestFileStoreQuerySortsAcrossSourceFiles(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	receivedAt := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	for _, msg := range []model.Message{
		{ID: "old", ReceivedAt: receivedAt, Source: "z.example:5514", Message: "older"},
		{ID: "new", ReceivedAt: receivedAt.Add(time.Second), Source: "a.example:5514", Message: "newer"},
	} {
		if err := store.Append(context.Background(), msg); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	got, err := store.Query(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(Query()) = %d, want 2", len(got))
	}
	if got[0].ID != "new" || got[1].ID != "old" {
		t.Fatalf("Query() order = [%s %s], want [new old]", got[0].ID, got[1].ID)
	}
}

func TestFileStoreQueryFiltersText(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	receivedAt := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	for _, msg := range []model.Message{
		{ID: "one", ReceivedAt: receivedAt, Hostname: "edge-a", Message: "accepted login"},
		{ID: "two", ReceivedAt: receivedAt.Add(time.Second), Hostname: "edge-b", Message: "dropped packet"},
	} {
		if err := store.Append(context.Background(), msg); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	got, err := store.Query(context.Background(), Query{Text: "drop", Limit: 10})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}

	if len(got) != 1 || got[0].ID != "two" {
		t.Fatalf("Query(Text) = %#v, want only message two", got)
	}
}
