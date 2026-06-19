package syslog

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultTestMessage = "logthing test event"
	defaultFacility    = 1
	defaultSeverity    = 5
)

type Event struct {
	Timestamp time.Time
	Hostname  string
	AppName   string
	ProcID    string
	MsgID     string
	Facility  int
	Severity  int
	Message   string
}

type Sender struct {
	network string
	address string
	timeout time.Duration
}

type SendResult struct {
	Network string `json:"network"`
	Address string `json:"address"`
	Payload string `json:"payload"`
}

func NewSender(network, address string, timeout time.Duration) (*Sender, error) {
	network = strings.ToLower(strings.TrimSpace(network))
	if network == "" {
		network = "udp"
	}
	if network != "udp" && network != "tcp" {
		return nil, fmt.Errorf("unsupported syslog network %q", network)
	}
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, errors.New("syslog destination address is required")
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Sender{network: network, address: address, timeout: timeout}, nil
}

func (s *Sender) Send(ctx context.Context, event Event) (SendResult, error) {
	payload := FormatRFC5424(event)
	dialer := net.Dialer{Timeout: s.timeout}
	conn, err := dialer.DialContext(ctx, s.network, s.address)
	if err != nil {
		return SendResult{}, fmt.Errorf("connect to syslog destination: %w", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	deadline := time.Now().Add(s.timeout)
	if err := conn.SetDeadline(deadline); err != nil {
		return SendResult{}, fmt.Errorf("set connection deadline: %w", err)
	}
	if _, err := conn.Write([]byte(payload)); err != nil {
		return SendResult{}, fmt.Errorf("write syslog message: %w", err)
	}

	return SendResult{
		Network: s.network,
		Address: s.address,
		Payload: strings.TrimRight(payload, "\n"),
	}, nil
}

func FormatRFC5424(event Event) string {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.Hostname == "" {
		event.Hostname = localHostname()
	}
	if event.AppName == "" {
		event.AppName = "logthing"
	}
	if event.ProcID == "" {
		event.ProcID = strconv.Itoa(os.Getpid())
	}
	if event.MsgID == "" {
		event.MsgID = "test"
	}
	if event.Message == "" {
		event.Message = DefaultTestMessage
	}
	if event.Facility < 0 {
		event.Facility = defaultFacility
	}
	if event.Severity < 0 {
		event.Severity = defaultSeverity
	}

	priority := event.Facility*8 + event.Severity
	return fmt.Sprintf("<%d>1 %s %s %s %s %s - %s\n",
		priority,
		event.Timestamp.UTC().Format(time.RFC3339),
		token(event.Hostname),
		token(event.AppName),
		token(event.ProcID),
		token(event.MsgID),
		event.Message,
	)
}

func NewTestEvent(message string) Event {
	return Event{
		Timestamp: time.Now().UTC(),
		Hostname:  localHostname(),
		AppName:   "logthing",
		ProcID:    strconv.Itoa(os.Getpid()),
		MsgID:     "test",
		Facility:  defaultFacility,
		Severity:  defaultSeverity,
		Message:   message,
	}
}

func DefaultTestEventDestination(udpAddr, tcpAddr string) (string, string) {
	if target := loopbackTarget(udpAddr); target != "" {
		return "udp", target
	}
	if target := loopbackTarget(tcpAddr); target != "" {
		return "tcp", target
	}
	return "udp", ""
}

func loopbackTarget(listenAddr string) string {
	listenAddr = strings.TrimSpace(listenAddr)
	if listenAddr == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return listenAddr
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func localHostname() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		return "logthing"
	}
	return hostname
}

func token(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	value = strings.ReplaceAll(value, " ", "_")
	return value
}
