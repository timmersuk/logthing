package storage

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/timmersuk/logthing/internal/model"
)

const (
	defaultQueryLimit = 200
	maxScannerToken   = 4 * 1024 * 1024
)

type FileStore struct {
	root string
	mu   sync.Mutex
}

func NewFileStore(root string) (*FileStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("storage root is required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create storage root: %w", err)
	}
	return &FileStore{root: root}, nil
}

func (s *FileStore) Append(ctx context.Context, msg model.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if msg.ID == "" {
		return errors.New("message id is required")
	}
	if msg.ReceivedAt.IsZero() {
		msg.ReceivedAt = time.Now().UTC()
	}

	path := s.pathFor(msg)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create message partition: %w", err)
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(msg); err != nil {
		return fmt.Errorf("encode message: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open message partition: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	if _, err := file.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("append message: %w", err)
	}
	return nil
}

func (s *FileStore) Query(ctx context.Context, query Query) ([]model.Message, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = defaultQueryLimit
	}

	files, err := s.messageFiles()
	if err != nil {
		return nil, err
	}

	text := strings.ToLower(strings.TrimSpace(query.Text))
	hosts := selectedHosts(query.Hosts)
	messages := make([]model.Message, 0, limit)

	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		dayMessages, err := readMessageFile(file)
		if err != nil {
			return nil, err
		}
		for i := len(dayMessages) - 1; i >= 0; i-- {
			msg := dayMessages[i]
			if !withinWindow(msg, query.Since, query.Until) {
				continue
			}
			if len(hosts) > 0 && !matchesHost(msg, hosts) {
				continue
			}
			if text != "" && !matchesText(msg, text) {
				continue
			}
			messages = append(messages, msg)
		}
	}

	sort.Slice(messages, func(i, j int) bool {
		return messages[i].ReceivedAt.After(messages[j].ReceivedAt)
	})
	if query.Offset > 0 {
		if query.Offset >= len(messages) {
			return []model.Message{}, nil
		}
		messages = messages[query.Offset:]
	}
	if len(messages) > limit {
		messages = messages[:limit]
	}
	return messages, nil
}

func (s *FileStore) pathFor(msg model.Message) string {
	utc := msg.ReceivedAt.UTC()
	return filepath.Join(
		s.root,
		fmt.Sprintf("%04d", utc.Year()),
		fmt.Sprintf("%02d", utc.Month()),
		fmt.Sprintf("%02d", utc.Day()),
		sourceFilename(msg),
	)
}

func (s *FileStore) messageFiles() ([]string, error) {
	var files []string
	err := filepath.WalkDir(s.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".ndjson" {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list message partitions: %w", err)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	return files, nil
}

func readMessageFile(path string) ([]model.Message, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open message partition %s: %w", path, err)
	}
	defer func() {
		_ = file.Close()
	}()

	var messages []model.Message
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), maxScannerToken)
	for scanner.Scan() {
		var msg model.Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			return nil, fmt.Errorf("decode message partition %s: %w", path, err)
		}
		messages = append(messages, msg)
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read message partition %s: %w", path, err)
	}
	return messages, nil
}

func withinWindow(msg model.Message, since, until *time.Time) bool {
	if since != nil && msg.ReceivedAt.Before(*since) {
		return false
	}
	if until != nil && msg.ReceivedAt.After(*until) {
		return false
	}
	return true
}

func selectedHosts(values []string) map[string]struct{} {
	hosts := make(map[string]struct{}, len(values))
	for _, value := range values {
		host := strings.TrimSpace(value)
		if host == "" {
			continue
		}
		hosts[host] = struct{}{}
	}
	return hosts
}

func matchesHost(msg model.Message, hosts map[string]struct{}) bool {
	_, ok := hosts[msg.Hostname]
	return ok
}

func matchesText(msg model.Message, needle string) bool {
	haystack := strings.ToLower(strings.Join([]string{
		msg.ID,
		msg.Transport,
		msg.Source,
		msg.Hostname,
		msg.AppName,
		msg.ProcID,
		msg.MsgID,
		msg.Tag,
		msg.Message,
		jsonText(msg.StructuredData),
		jsonText(msg.Raw),
	}, " "))
	return strings.Contains(haystack, needle)
}

func jsonText(value any) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func sourceFilename(msg model.Message) string {
	source := strings.TrimSpace(msg.Source)
	if source == "" {
		source = strings.TrimSpace(msg.Hostname)
	}
	if source == "" {
		source = "unknown"
	}
	if host, _, err := net.SplitHostPort(source); err == nil {
		source = host
	}

	var filename strings.Builder
	for _, r := range source {
		switch {
		case r >= 'a' && r <= 'z':
			filename.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			filename.WriteRune(r)
		case r >= '0' && r <= '9':
			filename.WriteRune(r)
		case r == '.' || r == '-' || r == '_':
			filename.WriteRune(r)
		default:
			filename.WriteRune('_')
		}
	}

	cleaned := strings.Trim(filename.String(), "._-")
	if cleaned == "" {
		cleaned = "unknown"
	}
	return cleaned + ".ndjson"
}
