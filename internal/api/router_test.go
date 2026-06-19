package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/timmersuk/logthing/internal/model"
	"github.com/timmersuk/logthing/internal/storage"
)

type fakeStore struct {
	messages []model.Message
}

func (s fakeStore) Append(context.Context, model.Message) error {
	return nil
}

func (s fakeStore) Query(context.Context, storage.Query) ([]model.Message, error) {
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
