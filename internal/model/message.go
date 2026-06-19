package model

import "time"

// Message is the stored and API-facing representation of one received syslog event.
type Message struct {
	ID             string         `json:"id"`
	ReceivedAt     time.Time      `json:"received_at"`
	Timestamp      *time.Time     `json:"timestamp,omitempty"`
	Transport      string         `json:"transport,omitempty"`
	Source         string         `json:"source,omitempty"`
	Priority       *int           `json:"priority,omitempty"`
	Facility       *int           `json:"facility,omitempty"`
	Severity       *int           `json:"severity,omitempty"`
	Hostname       string         `json:"hostname,omitempty"`
	AppName        string         `json:"app_name,omitempty"`
	ProcID         string         `json:"proc_id,omitempty"`
	MsgID          string         `json:"msg_id,omitempty"`
	Tag            string         `json:"tag,omitempty"`
	Message        string         `json:"message,omitempty"`
	StructuredData map[string]any `json:"structured_data,omitempty"`
	Raw            map[string]any `json:"raw,omitempty"`
}
