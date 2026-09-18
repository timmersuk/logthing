package storage

import (
	"bufio"
	"container/heap"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/timmersuk/logthing/internal/model"
)

// Keep only positions and filter keys in memory, not decoded message bodies.
// Indexes are rebuilt after restart and extended when a partition grows.
type indexEntry struct {
	receivedAt time.Time
	host       string
	offset     int64
	length     int
}
type fileIndex struct {
	info    os.FileInfo
	entries []indexEntry
}

func (s *FileStore) queryIndexes(ctx context.Context) (map[string]*fileIndex, []string, error) {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	files, err := s.messageFiles()
	if err != nil {
		return nil, nil, err
	}
	next := make(map[string]*fileIndex, len(files))
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		// Snapshot the size between writes so a query never indexes a partial append.
		s.mu.Lock()
		f, err := os.Open(path)
		var info os.FileInfo
		if err == nil {
			info, err = f.Stat()
		}
		s.mu.Unlock()
		if err != nil {
			if f != nil {
				f.Close()
			}
			return nil, nil, err
		}
		previous := s.indexes[path]
		if previous != nil && os.SameFile(previous.info, info) && previous.info.Size() == info.Size() && previous.info.ModTime() == info.ModTime() {
			next[path] = previous
			f.Close()
			continue
		}
		idx := &fileIndex{info: info}
		var offset int64
		if previous != nil && os.SameFile(previous.info, info) && info.Size() > previous.info.Size() {
			offset = previous.info.Size()
			idx.entries = append(idx.entries, previous.entries...)
		}
		_, err = f.Seek(offset, io.SeekStart)
		if err == nil {
			err = scanIndex(ctx, f, info.Size()-offset, offset, idx)
		}
		f.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("index message partition %s: %w", path, err)
		}
		sort.Slice(idx.entries, func(i, j int) bool {
			a, b := idx.entries[i], idx.entries[j]
			if a.receivedAt.Equal(b.receivedAt) {
				return a.offset > b.offset
			}
			return a.receivedAt.After(b.receivedAt)
		})
		next[path] = idx
	}
	s.indexes = next
	return next, files, nil
}

func scanIndex(ctx context.Context, f *os.File, size, offset int64, idx *fileIndex) error {
	scanner := bufio.NewScanner(io.LimitReader(f, size))
	scanner.Buffer(make([]byte, 64*1024), maxScannerToken)
	// Preserve exact line lengths, including CRLF, for ReadAt offsets.
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		for i, b := range data {
			if b == '\n' {
				return i + 1, data[:i+1], nil
			}
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Bytes()
		var key struct {
			ReceivedAt time.Time `json:"received_at"`
			Host       string    `json:"hostname"`
		}
		if err := json.Unmarshal(line, &key); err != nil {
			return err
		}
		idx.entries = append(idx.entries, indexEntry{receivedAt: key.ReceivedAt, host: key.Host, offset: offset, length: len(line)})
		offset += int64(len(line))
	}
	return scanner.Err()
}

type indexCursor struct {
	path            string
	entries         []indexEntry
	position, order int
}
type queryHeap []indexCursor

func (h queryHeap) Len() int { return len(h) }
func (h queryHeap) Less(i, j int) bool {
	a, b := h[i].entries[h[i].position], h[j].entries[h[j].position]
	if a.receivedAt.Equal(b.receivedAt) {
		return h[i].order < h[j].order
	}
	return a.receivedAt.After(b.receivedAt)
}
func (h queryHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *queryHeap) Push(x any)   { *h = append(*h, x.(indexCursor)) }
func (h *queryHeap) Pop() any     { old := *h; x := old[len(old)-1]; *h = old[:len(old)-1]; return x }

func (s *FileStore) Query(ctx context.Context, query Query) ([]model.Message, error) {
	match, err := CompileTextFilter(query.Text)
	if err != nil {
		return nil, err
	}
	indexes, files, err := s.queryIndexes(ctx)
	if err != nil {
		return nil, err
	}
	limit := query.Limit
	if limit <= 0 {
		limit = defaultQueryLimit
	}
	hosts := selectedHosts(query.Hosts)
	text := strings.ToLower(strings.TrimSpace(query.Text))
	pending := queryHeap{}
	for order, path := range files {
		entries := indexes[path].entries
		position := 0
		if query.Until != nil {
			position = sort.Search(len(entries), func(i int) bool { return !entries[i].receivedAt.After(*query.Until) })
		}
		if position < len(entries) {
			pending = append(pending, indexCursor{path: path, entries: entries, position: position, order: order})
		}
	}
	heap.Init(&pending)
	opened := map[string]*os.File{}
	defer func() {
		for _, f := range opened {
			f.Close()
		}
	}()
	messages := make([]model.Message, 0, limit)
	skipped := 0
	for pending.Len() > 0 && len(messages) < limit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		cursor := heap.Pop(&pending).(indexCursor)
		entry := cursor.entries[cursor.position]
		if query.Since != nil && entry.receivedAt.Before(*query.Since) {
			break
		}
		cursor.position++
		if cursor.position < len(cursor.entries) {
			heap.Push(&pending, cursor)
		}
		if len(hosts) > 0 {
			if _, ok := hosts[entry.host]; !ok {
				continue
			}
		}
		// With no text filter the index alone is sufficient to skip earlier pages.
		if text == "" && skipped < query.Offset {
			skipped++
			continue
		}
		f := opened[cursor.path]
		if f == nil {
			f, err = os.Open(cursor.path)
			if err != nil {
				return nil, err
			}
			opened[cursor.path] = f
		}
		data := make([]byte, entry.length)
		if _, err = f.ReadAt(data, entry.offset); err != nil {
			return nil, fmt.Errorf("read indexed message: %w", err)
		}
		var msg model.Message
		if err = json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}
		if text != "" && !match(msg) {
			continue
		}
		if skipped < query.Offset {
			skipped++
			continue
		}
		messages = append(messages, msg)
	}
	return messages, nil
}
