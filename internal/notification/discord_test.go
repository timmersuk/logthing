package notification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDiscordNotifierSendsPortableNotification(t *testing.T) {
	t.Parallel()

	var payload map[string]any
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("wait"); got != "true" {
			t.Errorf("wait query = %q, want true", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"discord-message-1"}`))
	}))
	defer server.Close()

	notifier, err := newDiscord(server.URL, time.Second, server.Client())
	if err != nil {
		t.Fatalf("NewDiscord() error = %v", err)
	}
	receipt, err := notifier.Send(context.Background(), Notification{
		EventID: "event-1",
		Kind:    KindIncidentOpened,
		Title:   "Primary WAN down",
		Body:    "router-a wan has been offline for 1m",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if receipt.Adapter != "discord" || receipt.ExternalID != "discord-message-1" {
		t.Fatalf("receipt = %#v", receipt)
	}
	content, _ := payload["content"].(string)
	if !strings.Contains(content, "Primary WAN down") || !strings.Contains(content, "router-a") {
		t.Fatalf("content = %q", content)
	}
}

func TestDiscordNotifierClassifiesRetryableAndPermanentFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    int
		permanent bool
	}{
		{name: "rate limited", status: http.StatusTooManyRequests, permanent: false},
		{name: "server error", status: http.StatusBadGateway, permanent: false},
		{name: "bad request", status: http.StatusBadRequest, permanent: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "failure", test.status)
			}))
			defer server.Close()
			notifier, err := newDiscord(server.URL, time.Second, server.Client())
			if err != nil {
				t.Fatalf("NewDiscord() error = %v", err)
			}
			_, err = notifier.Send(context.Background(), Notification{Title: "test"})
			var sendErr *SendError
			if !AsSendError(err, &sendErr) {
				t.Fatalf("error = %v, want SendError", err)
			}
			if sendErr.Permanent != test.permanent {
				t.Fatalf("Permanent = %v, want %v", sendErr.Permanent, test.permanent)
			}
		})
	}
}

func TestDiscordNotifierDoesNotLeakWebhookURL(t *testing.T) {
	t.Parallel()

	secret := "secret-webhook-token"
	notifier, err := NewDiscord("https://127.0.0.1:1/"+secret, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewDiscord() error = %v", err)
	}
	_, err = notifier.Send(context.Background(), Notification{Title: "test"})
	if err == nil {
		t.Fatal("Send() error = nil, want failure")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked webhook secret: %v", err)
	}
}

func TestDiscordNotifierRejectsUnencryptedWebhook(t *testing.T) {
	t.Parallel()
	if _, err := NewDiscord("http://discord.example/api/webhooks/token", time.Second); err == nil {
		t.Fatal("NewDiscord() error = nil, want HTTPS validation error")
	}
}
