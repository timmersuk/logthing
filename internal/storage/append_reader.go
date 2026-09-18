package storage

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
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

type Cursor struct {
	Offset      int64  `json:"offset"`
	Fingerprint string `json:"fingerprint"`
}

type PartitionChangedError struct{ File string }

func (e *PartitionChangedError) Error() string {
	return fmt.Sprintf("message partition %s was replaced or truncated", e.File)
}

func IsPartitionChanged(err error) bool {
	_, ok := PartitionChangedFile(err)
	return ok
}

func PartitionChangedFile(err error) (string, bool) {
	var changed *PartitionChangedError
	if !errors.As(err, &changed) {
		return "", false
	}
	return changed.File, true
}

func (s *FileStore) VisitAfter(
	ctx context.Context,
	cursors map[string]Cursor,
	visit func(StoredRecord) error,
) (map[string]Cursor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	next := make(map[string]Cursor, len(cursors))
	for path, cursor := range cursors {
		next[path] = cursor
	}

	s.mu.Lock()
	files, err := s.messageFiles()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	seen := make(map[string]struct{}, len(files))
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		relative, err := filepath.Rel(s.root, path)
		if err != nil {
			return nil, fmt.Errorf("resolve message partition path: %w", err)
		}
		key := filepath.ToSlash(relative)
		seen[key] = struct{}{}
		cursor := next[key]
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat message partition %s: %w", key, err)
		}
		if cursor.Offset < 0 || cursor.Offset > info.Size() {
			return nil, &PartitionChangedError{File: key}
		}

		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open message partition %s: %w", key, err)
		}
		if cursor.Fingerprint != "" {
			fingerprint, err := fileFingerprint(file, cursor.Offset)
			if err != nil {
				_ = file.Close()
				return nil, fmt.Errorf("fingerprint message partition %s: %w", key, err)
			}
			if cursor.Fingerprint != fingerprint {
				_ = file.Close()
				return nil, &PartitionChangedError{File: key}
			}
		}
		if _, err := file.Seek(cursor.Offset, io.SeekStart); err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("seek message partition %s: %w", key, err)
		}
		reader := bufio.NewReaderSize(file, maxScannerToken)
		current := cursor.Offset
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
		}
		fingerprint, err := fileFingerprint(file, current)
		if err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("fingerprint message partition %s: %w", key, err)
		}
		next[key] = Cursor{Offset: current, Fingerprint: fingerprint}
		if err := file.Close(); err != nil {
			return nil, fmt.Errorf("close message partition %s: %w", key, err)
		}
	}
	for key, cursor := range cursors {
		if cursor.Offset > 0 {
			if _, exists := seen[key]; !exists {
				return nil, &PartitionChangedError{File: key}
			}
		}
	}
	return next, nil
}

func fileFingerprint(file *os.File, size int64) (string, error) {
	const windowSize = 2048
	length := size
	if length > windowSize {
		length = windowSize
	}
	data := make([]byte, 0, length*2)
	first := make([]byte, length)
	if length > 0 {
		if _, err := file.ReadAt(first, 0); err != nil && err != io.EOF {
			return "", err
		}
		data = append(data, first...)
	}
	if size > windowSize {
		last := make([]byte, length)
		if _, err := file.ReadAt(last, size-length); err != nil && err != io.EOF {
			return "", err
		}
		data = append(data, last...)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:]), nil
}
