package syslog

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	goSyslog "gopkg.in/mcuadros/go-syslog.v2"
	"gopkg.in/mcuadros/go-syslog.v2/format"

	"github.com/timmersuk/logthing/internal/model"
	"github.com/timmersuk/logthing/internal/storage"
)

type Config struct {
	UDPAddr string
	TCPAddr string
	Format  string
}

type Listener struct {
	cfg       Config
	store     storage.Store
	server    *goSyslog.Server
	channel   goSyslog.LogPartsChannel
	transport string
	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

func NewListener(cfg Config, store storage.Store) (*Listener, error) {
	if store == nil {
		return nil, errors.New("store is required")
	}
	if cfg.UDPAddr == "" && cfg.TCPAddr == "" {
		return nil, errors.New("at least one syslog listen address is required")
	}

	return &Listener{
		cfg:       cfg,
		store:     store,
		transport: transportName(cfg),
		done:      make(chan struct{}),
	}, nil
}

func (l *Listener) Start() error {
	l.channel = make(goSyslog.LogPartsChannel, 1024)
	handler := goSyslog.NewChannelHandler(l.channel)

	server := goSyslog.NewServer()
	setFormat(server, l.cfg.Format)
	server.SetHandler(handler)

	if l.cfg.UDPAddr != "" {
		if err := server.ListenUDP(l.cfg.UDPAddr); err != nil {
			return fmt.Errorf("listen syslog udp %s: %w", l.cfg.UDPAddr, err)
		}
	}
	if l.cfg.TCPAddr != "" {
		if err := server.ListenTCP(l.cfg.TCPAddr); err != nil {
			return fmt.Errorf("listen syslog tcp %s: %w", l.cfg.TCPAddr, err)
		}
	}

	if err := server.Boot(); err != nil {
		return fmt.Errorf("boot syslog listener: %w", err)
	}

	l.server = server
	l.wg.Add(1)
	go l.consume()
	return nil
}

func (l *Listener) Shutdown() {
	l.closeOnce.Do(func() {
		close(l.done)
		if l.server != nil {
			_ = l.server.Kill()
		}
	})
	l.wg.Wait()
}

func (l *Listener) consume() {
	defer l.wg.Done()

	for {
		select {
		case <-l.done:
			return
		case parts, ok := <-l.channel:
			if !ok {
				return
			}
			msg := messageFromParts(parts, l.transport)
			if err := l.store.Append(context.Background(), msg); err != nil {
				log.Printf("store syslog message: %v", err)
			}
		}
	}
}

func setFormat(server *goSyslog.Server, format string) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "rfc3164":
		server.SetFormat(goSyslog.RFC3164)
	case "rfc6587":
		server.SetFormat(goSyslog.RFC6587)
	case "rfc5424":
		server.SetFormat(goSyslog.RFC5424)
	default:
		server.SetFormat(goSyslog.Automatic)
	}
}

func transportName(cfg Config) string {
	switch {
	case cfg.UDPAddr != "" && cfg.TCPAddr != "":
		return "udp/tcp"
	case cfg.UDPAddr != "":
		return "udp"
	case cfg.TCPAddr != "":
		return "tcp"
	default:
		return ""
	}
}

func messageFromParts(parts format.LogParts, transport string) model.Message {
	raw := make(map[string]any, len(parts))
	for key, value := range parts {
		raw[key] = jsonSafeValue(value)
	}

	msg := model.Message{
		ID:             newID(),
		ReceivedAt:     time.Now().UTC(),
		Timestamp:      getTime(parts, "timestamp"),
		Transport:      transport,
		Source:         firstString(parts, "client", "source"),
		Priority:       getInt(parts, "priority"),
		Facility:       getInt(parts, "facility"),
		Severity:       getInt(parts, "severity"),
		Hostname:       firstString(parts, "hostname", "host"),
		AppName:        firstString(parts, "app_name", "appname"),
		ProcID:         firstString(parts, "proc_id", "procid"),
		MsgID:          firstString(parts, "msg_id", "msgid"),
		Tag:            firstString(parts, "tag"),
		Message:        firstString(parts, "message", "content", "msg"),
		StructuredData: getMap(parts, "structured_data"),
		Raw:            raw,
	}

	return msg
}

func newID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(id[:])
}

func firstString(parts format.LogParts, keys ...string) string {
	for _, key := range keys {
		if value, ok := parts[key]; ok {
			return stringify(value)
		}
	}
	return ""
}

func getInt(parts format.LogParts, key string) *int {
	value, ok := parts[key]
	if !ok {
		return nil
	}

	var parsed int
	switch typed := value.(type) {
	case int:
		parsed = typed
	case int8:
		parsed = int(typed)
	case int16:
		parsed = int(typed)
	case int32:
		parsed = int(typed)
	case int64:
		parsed = int(typed)
	case uint:
		parsed = int(typed)
	case uint8:
		parsed = int(typed)
	case uint16:
		parsed = int(typed)
	case uint32:
		parsed = int(typed)
	case uint64:
		parsed = int(typed)
	case float64:
		parsed = int(typed)
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return nil
		}
		parsed = i
	default:
		return nil
	}
	return &parsed
}

func getTime(parts format.LogParts, key string) *time.Time {
	value, ok := parts[key]
	if !ok {
		return nil
	}

	switch typed := value.(type) {
	case time.Time:
		ts := typed.UTC()
		return &ts
	case string:
		ts, err := time.Parse(time.RFC3339Nano, typed)
		if err != nil {
			return nil
		}
		ts = ts.UTC()
		return &ts
	default:
		return nil
	}
}

func getMap(parts format.LogParts, key string) map[string]any {
	value, ok := parts[key]
	if !ok {
		return nil
	}
	converted, ok := jsonSafeValue(value).(map[string]any)
	if !ok {
		return nil
	}
	return converted
}

func stringify(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []byte:
		return string(typed)
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprint(typed)
	}
}

func jsonSafeValue(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case string, bool, float64, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, time.Time:
		return typed
	case []byte:
		return string(typed)
	case map[string]any:
		converted := make(map[string]any, len(typed))
		for key, child := range typed {
			converted[key] = jsonSafeValue(child)
		}
		return converted
	case map[string]string:
		converted := make(map[string]any, len(typed))
		for key, child := range typed {
			converted[key] = child
		}
		return converted
	case map[string]map[string]string:
		converted := make(map[string]any, len(typed))
		for key, child := range typed {
			converted[key] = jsonSafeValue(child)
		}
		return converted
	case []string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		if _, err := json.Marshal(typed); err == nil {
			return typed
		}
		return fmt.Sprint(typed)
	}
}
