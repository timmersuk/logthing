package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/timmersuk/logthing/internal/model"
	"github.com/timmersuk/logthing/internal/storage"
)

const maxImportLine = 4 * 1024 * 1024

type importMessagesResponse struct {
	Status   string `json:"status"`
	Imported int    `json:"imported"`
	Skipped  int    `json:"skipped"`
}

type importResult struct {
	imported int
	skipped  int
}

type importValidationError struct {
	message string
}

func (e importValidationError) Error() string {
	return e.message
}

func importMessages(ctx context.Context, store storage.Store, reader io.Reader) (importResult, error) {
	var result importResult

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), maxImportLine)

	lineNumber := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		lineNumber++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			result.skipped++
			continue
		}

		msg, err := parseImportedMessage(line, lineNumber)
		if err != nil {
			return result, err
		}
		if err := store.Append(ctx, msg); err != nil {
			return result, fmt.Errorf("append imported message on line %d: %w", lineNumber, err)
		}
		result.imported++
	}
	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("read import body: %w", err)
	}

	return result, nil
}

func parseImportedMessage(line []byte, lineNumber int) (model.Message, error) {
	var raw map[string]any
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return model.Message{}, invalidImportLine(lineNumber, "invalid JSON: %v", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return model.Message{}, invalidImportLine(lineNumber, "invalid trailing JSON")
	}

	timestampText := strings.TrimSpace(importString(raw, "timestamp"))
	if timestampText == "" {
		return model.Message{}, invalidImportLine(lineNumber, "timestamp is required")
	}

	timestamp, err := time.Parse(time.RFC3339Nano, timestampText)
	if err != nil {
		return model.Message{}, invalidImportLine(lineNumber, "invalid timestamp: %v", err)
	}
	timestamp = timestamp.UTC()

	facility, err := parseFacility(importString(raw, "facility"))
	if err != nil {
		return model.Message{}, invalidImportLine(lineNumber, "invalid facility: %v", err)
	}
	severity, err := parseSeverity(importString(raw, "severity"))
	if err != nil {
		return model.Message{}, invalidImportLine(lineNumber, "invalid severity: %v", err)
	}

	var priority *int
	if facility != nil && severity != nil {
		value := (*facility * 8) + *severity
		priority = &value
	}

	host := strings.TrimSpace(importString(raw, "host"))
	if host == "" {
		host = "unknown"
	}
	program := strings.TrimSpace(importString(raw, "program"))

	return model.Message{
		ID:         newImportID(),
		ReceivedAt: timestamp,
		Timestamp:  &timestamp,
		Transport:  "import",
		Priority:   priority,
		Facility:   facility,
		Severity:   severity,
		Hostname:   host,
		AppName:    program,
		ProcID:     strings.TrimSpace(importString(raw, "pid")),
		Tag:        program,
		Message:    importString(raw, "message"),
		Raw:        raw,
	}, nil
}

func invalidImportLine(lineNumber int, format string, args ...any) error {
	return importValidationError{message: fmt.Sprintf("line %d: %s", lineNumber, fmt.Sprintf(format, args...))}
}

func importString(raw map[string]any, key string) string {
	value, ok := raw[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case bool:
		return strconv.FormatBool(typed)
	default:
		return fmt.Sprint(typed)
	}
}

func parseSeverity(value string) (*int, error) {
	return parseSyslogCode(value, severityNames, 0, 7)
}

func parseFacility(value string) (*int, error) {
	return parseSyslogCode(value, facilityNames, 0, 23)
}

func parseSyslogCode(value string, names map[string]int, minValue, maxValue int) (*int, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return nil, nil
	}
	if parsed, ok := names[value]; ok {
		return &parsed, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return nil, fmt.Errorf("%q is not recognized", value)
	}
	if parsed < minValue || parsed > maxValue {
		return nil, fmt.Errorf("%d is outside %d-%d", parsed, minValue, maxValue)
	}
	return &parsed, nil
}

func newImportID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(id[:])
}

var severityNames = map[string]int{
	"emerg":         0,
	"emergency":     0,
	"alert":         1,
	"crit":          2,
	"critical":      2,
	"err":           3,
	"error":         3,
	"warning":       4,
	"warn":          4,
	"notice":        5,
	"info":          6,
	"informational": 6,
	"debug":         7,
}

var facilityNames = map[string]int{
	"kern":     0,
	"kernel":   0,
	"user":     1,
	"mail":     2,
	"daemon":   3,
	"auth":     4,
	"security": 4,
	"syslog":   5,
	"lpr":      6,
	"news":     7,
	"uucp":     8,
	"cron":     9,
	"authpriv": 10,
	"ftp":      11,
	"ntp":      12,
	"audit":    13,
	"alert":    14,
	"clock":    15,
	"local0":   16,
	"local1":   17,
	"local2":   18,
	"local3":   19,
	"local4":   20,
	"local5":   21,
	"local6":   22,
	"local7":   23,
}

func isImportValidationError(err error) bool {
	var validationErr importValidationError
	return errors.As(err, &validationErr)
}
