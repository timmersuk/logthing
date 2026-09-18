package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/timmersuk/logthing/internal/model"
)

func TestQueryIndexRefreshAndHistoricalOrdering(t *testing.T) {
	s, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	appendMessage := func(id, source string, delta int) {
		t.Helper()
		if err := s.Append(context.Background(), model.Message{ID: id, Source: source, Hostname: source, ReceivedAt: base.Add(time.Duration(delta) * time.Second), Message: "match"}); err != nil {
			t.Fatal(err)
		}
	}
	check := func(q Query, ids string) {
		t.Helper()
		rows, err := s.Query(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		for _, row := range rows {
			got += row.ID + ","
		}
		if got != ids {
			t.Fatalf("got %s want %s", got, ids)
		}
	}
	appendMessage("a", "one", 0)
	appendMessage("b", "two", 2)
	check(Query{}, "b,a,")
	appendMessage("c", "one", 1) // Historical import appended after newer data.
	appendMessage("d", "one", 3)
	check(Query{}, "d,b,c,a,")
	check(Query{Offset: 1, Limit: 2}, "b,c,")
	check(Query{Hosts: []string{"one"}, Text: "match", Offset: 1, Limit: 1}, "c,")
	until := base.Add(time.Second)
	check(Query{Until: &until}, "c,a,")
	path := filepath.Join(s.root, "2026", "09", "18", "one.ndjson")
	if err := os.WriteFile(path, []byte(fmt.Sprintf("{\"id\":\"replacement\",\"received_at\":%q}\n", base.Format(time.RFC3339))), 0600); err != nil {
		t.Fatal(err)
	}
	check(Query{}, "b,replacement,")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	check(Query{}, "b,")
}

func TestQueryIndexConcurrentAppend(t *testing.T) {
	s, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 100; i++ {
			if err := s.Append(ctx, model.Message{ID: fmt.Sprint(i), ReceivedAt: time.Now()}); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for i := 0; i < 20; i++ {
		if _, err := s.Query(ctx, Query{Limit: 10}); err != nil {
			t.Fatal(err)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	rows, err := s.Query(ctx, Query{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 100 {
		t.Fatalf("got %d rows, want 100", len(rows))
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Query(canceled, Query{}); err == nil {
		t.Fatal("expected cancellation")
	}
}

func TestQueryIndexCRLFAndEqualTimestamps(t *testing.T) {
	root := t.TempDir()
	line := "{\"id\":\"a\",\"received_at\":\"2026-09-18T12:00:00Z\"}\r\n"
	path := filepath.Join(root, "events.ndjson")
	if err := os.WriteFile(path, []byte(line+strings.Replace(line, "\"a\"", "\"b\"", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.Query(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "b" || rows[1].ID != "a" {
		t.Fatalf("unexpected rows: %#v", rows)
	}
	// Same-size edits with a changed mtime must invalidate the cached offsets/keys.
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(line+line, "\"a\"", "\"c\"")), 0600); err != nil {
		t.Fatal(err)
	}
	changed := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, changed, changed); err != nil {
		t.Fatal(err)
	}
	rows, err = s.Query(context.Background(), Query{})
	if err != nil || len(rows) != 2 || rows[0].ID != "c" {
		t.Fatalf("replacement: %#v, %v", rows, err)
	}
}
