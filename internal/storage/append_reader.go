package storage

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/timmersuk/logthing/internal/model"
)

type StoredRecord struct {
	Message model.Message
	File    string
	Offset  int64
}

func (s *FileStore) VisitAfter(
	ctx context.Context,
	cursors map[string]int64,
	visit func(StoredRecord) error,
) (map[string]int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	next := make(map[string]int64, len(cursors))
	for path, offset := range cursors {
		next[path] = offset
	}

	s.mu.Lock()
	files, err := s.messageFiles()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		relative, err := filepath.Rel(s.root, path)
		if err != nil {
			return nil, fmt.Errorf("resolve message partition path: %w", err)
		}
		key := filepath.ToSlash(relative)
		offset := next[key]
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat message partition %s: %w", key, err)
		}
		if offset < 0 || offset > info.Size() {
			offset = 0
		}

		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open message partition %s: %w", key, err)
		}
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("seek message partition %s: %w", key, err)
		}
		reader := bufio.NewReaderSize(file, maxScannerToken)
		current := offset
		for {
			line, readErr := reader.ReadBytes('\n')
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				_ = file.Close()
				return nil, fmt.Errorf("read message partition %s at %d: %w", key, current, readErr)
			}
			var message model.Message
			if err := json.Unmarshal(line, &message); err != nil {
				_ = file.Close()
				return nil, fmt.Errorf("decode message partition %s at %d: %w", key, current, err)
			}
			current += int64(len(line))
			if err := visit(StoredRecord{Message: message, File: key, Offset: current}); err != nil {
				_ = file.Close()
				return nil, err
			}
			next[key] = current
		}
		if err := file.Close(); err != nil {
			return nil, fmt.Errorf("close message partition %s: %w", key, err)
		}
		if _, exists := next[key]; !exists {
			next[key] = offset
		}
	}
	return next, nil
}
