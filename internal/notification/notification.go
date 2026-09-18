package notification

import (
	"context"
	"errors"
	"time"
)

type Kind string

const (
	KindIncidentOpened   Kind = "incident_opened"
	KindIncidentResolved Kind = "incident_resolved"
	KindTest             Kind = "test"
)

type Notification struct {
	EventID    string
	Kind       Kind
	Severity   string
	Title      string
	Body       string
	OccurredAt time.Time
	IncidentID string
	Link       string
}

type Receipt struct {
	Adapter    string
	ExternalID string
}

type Notifier interface {
	Send(context.Context, Notification) (Receipt, error)
}

var ErrNotConfigured = errors.New("notification adapter is not configured")

type SendError struct {
	StatusCode int
	Permanent  bool
	RetryAfter time.Duration
	message    string
}

func (e *SendError) Error() string { return e.message }

func NewSendError(message string, permanent bool, retryAfter time.Duration) *SendError {
	return &SendError{Permanent: permanent, RetryAfter: retryAfter, message: message}
}

func AsSendError(err error, target **SendError) bool {
	return errors.As(err, target)
}

type DiscardNotifier struct{}

func (DiscardNotifier) Send(context.Context, Notification) (Receipt, error) {
	return Receipt{Adapter: "none"}, nil
}
