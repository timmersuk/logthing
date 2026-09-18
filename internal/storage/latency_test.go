package storage

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLocalDatasetLatency(t *testing.T) {
	root := os.Getenv("LOGTHING_BENCH_DATA")
	if root == "" {
		t.Skip("set LOGTHING_BENCH_DATA to run read-only dataset timing")
	}
	s, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	coldStart := time.Now()
	if _, err := s.Query(context.Background(), Query{Limit: 201}); err != nil {
		t.Fatal(err)
	}
	t.Logf("cold index build=%s", time.Since(coldStart))
	entries := 0
	for _, index := range s.indexes {
		entries += len(index.entries)
	}
	t.Logf("indexed events=%d partitions=%d", entries, len(s.indexes))
	for _, offset := range []int{0, 200} {
		start := time.Now()
		rows, err := s.Query(context.Background(), Query{Limit: 201, Offset: offset})
		elapsed := time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("offset=%d rows=%d elapsed=%s", offset, len(rows), elapsed)
		if elapsed > time.Second {
			t.Errorf("page query exceeded 1s budget: %s", elapsed)
		}
	}
}
