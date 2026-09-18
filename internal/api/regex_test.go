package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/timmersuk/logthing/internal/model"
	"github.com/timmersuk/logthing/internal/storage"
)

func TestRegexHistoryAndStream(t *testing.T) {
	store, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	messages := []model.Message{
		{ID: "one", ReceivedAt: base, Hostname: "router", Message: "PPPoE connected"},
		{ID: "two", ReceivedAt: base.Add(time.Second), Hostname: "router", Message: "LCP echo"},
		{ID: "three", ReceivedAt: base.Add(2 * time.Second), Hostname: "other", Message: "DNS query"},
	}
	for _, msg := range messages {
		if err := store.Append(context.Background(), msg); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		query  string
		want   int
		status int
	}{
		{"/pppoe|lcp/", 2, 200}, {"pppoe|lcp", 0, 200}, {"pppoe", 1, 200},
		{`/\bLCP\b/`, 1, 200}, {`/\D+/`, 3, 200}, {"/(?i:pppoe)/", 1, 200},
		{"/[/", 0, 400}, {"/(?=lcp)/", 0, 400},
	} {
		t.Run(tc.query, func(t *testing.T) {
			events := &fakeSubscriber{messages: make(chan model.Message, len(messages)), subscribed: make(chan struct{})}
			for _, msg := range messages {
				events.messages <- msg
			}
			close(events.messages)
			router := newTestRouterWithEvents(t, store, events)
			for _, path := range []string{"/api/v1/messages", "/api/v1/messages/stream"} {
				req := httptest.NewRequest(http.MethodGet, path+"?q="+url.QueryEscape(tc.query), nil)
				req.SetBasicAuth("admin", "secret")
				res := httptest.NewRecorder()
				router.ServeHTTP(res, req)
				if res.Code != tc.status {
					t.Fatalf("%s status=%d body=%s", path, res.Code, res.Body.String())
				}
				if tc.status == 400 {
					if !strings.Contains(res.Body.String(), "invalid regex filter") {
						t.Fatal(res.Body.String())
					}
					continue
				}
				count := strings.Count(res.Body.String(), "event: message\n")
				if path == "/api/v1/messages" {
					var body messagesResponse
					if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
						t.Fatal(err)
					}
					count = len(body.Data)
				}
				if count != tc.want {
					t.Fatalf("%s count=%d want=%d", path, count, tc.want)
				}
			}
		})
	}
	router := newTestRouter(t, store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages?q="+url.QueryEscape("/pppoe|lcp/")+"&host=router&limit=1&offset=1", nil)
	req.SetBasicAuth("admin", "secret")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	var body messagesResponse
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if res.Code != 200 || len(body.Data) != 1 || body.Data[0].ID != "one" || body.Meta.HasMore {
		t.Fatalf("filtered pagination: %s", res.Body.String())
	}
}
