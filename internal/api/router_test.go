package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/timmersuk/logthing/internal/model"
	"github.com/timmersuk/logthing/internal/storage"
)

type fakeStore struct {
	messages []model.Message
	queries  *[]storage.Query
	appends  *[]model.Message
}

func (s fakeStore) Append(_ context.Context, msg model.Message) error {
	if s.appends != nil {
		*s.appends = append(*s.appends, msg)
	}
	return nil
}

func (s fakeStore) Query(_ context.Context, query storage.Query) ([]model.Message, error) {
	if s.queries != nil {
		*s.queries = append(*s.queries, query)
	}
	return s.messages, nil
}

func TestMessagesRequireBasicAuth(t *testing.T) {
	router := newTestRouter(t, fakeStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages", nil)
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestMessagesReturnDataWithBasicAuth(t *testing.T) {
	receivedAt := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	router := newTestRouter(t, fakeStore{
		messages: []model.Message{{ID: "one", ReceivedAt: receivedAt, Hostname: "edge-a"}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages", nil)
	req.SetBasicAuth("admin", "secret")
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}

	var body messagesResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data) != 1 || body.Data[0].ID != "one" {
		t.Fatalf("response data = %#v, want message one", body.Data)
	}
}

func TestMessagesReturnPaginationMeta(t *testing.T) {
	receivedAt := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	var queries []storage.Query
	router := newTestRouter(t, fakeStore{
		messages: []model.Message{
			{ID: "one", ReceivedAt: receivedAt, Hostname: "edge-a"},
			{ID: "two", ReceivedAt: receivedAt.Add(time.Second), Hostname: "edge-b"},
		},
		queries: &queries,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages?limit=1&offset=10&host=edge-a&host=edge-b", nil)
	req.SetBasicAuth("admin", "secret")
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}

	var body messagesResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Meta.Count != 1 || body.Meta.Limit != 1 || body.Meta.Offset != 10 || !body.Meta.HasMore {
		t.Fatalf("meta = %#v, want count 1, limit 1, offset 10, has_more true", body.Meta)
	}
	if len(queries) != 1 {
		t.Fatalf("store query count = %d, want 1", len(queries))
	}
	if queries[0].Limit != 2 || queries[0].Offset != 10 || !reflect.DeepEqual(queries[0].Hosts, []string{"edge-a", "edge-b"}) {
		t.Fatalf("store query = %#v, want overfetch limit, offset, and hosts", queries[0])
	}
}

func TestParseMessagesQueryCleansHostsAndOffset(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages?host=edge-a&host=%20&host=edge-b&host=edge-a&limit=50&offset=100", nil)

	query, err := parseMessagesQuery(req)
	if err != nil {
		t.Fatalf("parseMessagesQuery() error = %v", err)
	}

	if query.Limit != 50 || query.Offset != 100 {
		t.Fatalf("query limit/offset = %d/%d, want 50/100", query.Limit, query.Offset)
	}
	if !reflect.DeepEqual(query.Hosts, []string{"edge-a", "edge-b"}) {
		t.Fatalf("query hosts = %#v, want edge-a and edge-b", query.Hosts)
	}
}

func TestParseMessagesQueryRejectsNegativeOffset(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages?offset=-1", nil)

	_, err := parseMessagesQuery(req)
	if err == nil {
		t.Fatal("parseMessagesQuery() error = nil, want error")
	}
}

func TestImportMessagesRequiresBasicAuth(t *testing.T) {
	router := newTestRouter(t, fakeStore{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages/import", strings.NewReader(""))
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestImportMessagesAcceptsNDJSON(t *testing.T) {
	var appends []model.Message
	router := newTestRouter(t, fakeStore{appends: &appends})

	body := strings.NewReader(`{"timestamp":"2026-06-19T00:24:11+01:00","host":"potato","program":"pppd","pid":"2017","severity":"warning","facility":"daemon","message":" Connected to xx:xx:xx:xx:xx:xx via interface eth0"}` + "\n\n")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages/import", body)
	req.SetBasicAuth("admin", "secret")
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}

	var response importMessagesResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Status != "imported" || response.Imported != 1 || response.Skipped != 1 {
		t.Fatalf("response = %#v, want one imported and one skipped", response)
	}
	if len(appends) != 1 {
		t.Fatalf("appended messages = %d, want 1", len(appends))
	}

	msg := appends[0]
	if msg.ID == "" {
		t.Fatal("imported message ID is empty")
	}
	if msg.Transport != "import" || msg.Hostname != "potato" || msg.AppName != "pppd" || msg.Tag != "pppd" || msg.ProcID != "2017" {
		t.Fatalf("imported message fields = %#v, want mapped syslog fields", msg)
	}
	if msg.Timestamp == nil || !msg.Timestamp.Equal(time.Date(2026, 6, 18, 23, 24, 11, 0, time.UTC)) {
		t.Fatalf("timestamp = %v, want UTC converted imported timestamp", msg.Timestamp)
	}
	if msg.ReceivedAt != *msg.Timestamp {
		t.Fatalf("received_at = %v, want imported timestamp %v", msg.ReceivedAt, *msg.Timestamp)
	}
	if msg.Facility == nil || *msg.Facility != 3 {
		t.Fatalf("facility = %v, want daemon value 3", msg.Facility)
	}
	if msg.Severity == nil || *msg.Severity != 4 {
		t.Fatalf("severity = %v, want warning value 4", msg.Severity)
	}
	if msg.Priority == nil || *msg.Priority != 28 {
		t.Fatalf("priority = %v, want 28", msg.Priority)
	}
	if msg.Message != " Connected to xx:xx:xx:xx:xx:xx via interface eth0" {
		t.Fatalf("message = %q, want imported message text", msg.Message)
	}
}

func TestImportMessagesRejectsInvalidNDJSON(t *testing.T) {
	router := newTestRouter(t, fakeStore{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages/import", strings.NewReader(`{"timestamp":"not-a-time"}`))
	req.SetBasicAuth("admin", "secret")
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusBadRequest)
	}
	if !strings.Contains(res.Body.String(), "line 1") {
		t.Fatalf("body = %q, want line number", res.Body.String())
	}
}

func TestHealthcheckDoesNotRequireAuth(t *testing.T) {
	router := newTestRouter(t, fakeStore{})

	req := httptest.NewRequest(http.MethodGet, "/healthcheck", nil)
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
}

func TestSwaggerRequiresBasicAuth(t *testing.T) {
	router := newTestRouter(t, fakeStore{})

	req := httptest.NewRequest(http.MethodGet, "/swagger.json", nil)
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestSwaggerReturnsSpecWithBasicAuth(t *testing.T) {
	router := newTestRouter(t, fakeStore{})

	req := httptest.NewRequest(http.MethodGet, "/swagger.json", nil)
	req.SetBasicAuth("admin", "secret")
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	if res.Body.String() != `{"openapi":"3.0.3"}` {
		t.Fatalf("body = %q, want OpenAPI spec", res.Body.String())
	}
}

func TestTestEventRequiresBasicAuth(t *testing.T) {
	router := newTestRouter(t, fakeStore{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/test-event", nil)
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestTestEventSendsWithBasicAuth(t *testing.T) {
	router := newTestRouter(t, fakeStore{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/test-event", nil)
	req.SetBasicAuth("admin", "secret")
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusAccepted)
	}

	var body testEventResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "sent" || body.Network != "udp" || body.Address != "127.0.0.1:5514" {
		t.Fatalf("response = %#v, want sent udp result", body)
	}
}

func newTestRouter(t *testing.T, store storage.Store) http.Handler {
	t.Helper()

	router, err := NewRouter(Config{
		Store: store,
		Frontend: fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("<html></html>")},
		},
		SwaggerUI: fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("<html>swagger</html>")},
		},
		OpenAPISpec: []byte(`{"openapi":"3.0.3"}`),
		TestEvent: func(context.Context, string) (TestEventResult, error) {
			return TestEventResult{
				Network: "udp",
				Address: "127.0.0.1:5514",
				Payload: "<13>1 2026-06-19T12:00:00Z host app proc test - test",
			}, nil
		},
		Credentials: Credentials{
			Username: "admin",
			Password: "secret",
		},
	})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	return router
}
