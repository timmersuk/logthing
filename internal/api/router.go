package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/timmersuk/logthing/internal/model"
	"github.com/timmersuk/logthing/internal/storage"
)

const (
	defaultMessageLimit = 200
	maxMessageLimit     = 2000
)

type Config struct {
	Store       storage.Store
	Frontend    fs.FS
	SwaggerUI   fs.FS
	OpenAPISpec []byte
	TestEvent   TestEventSender
	Credentials Credentials
	BuildID     string
}

type TestEventSender func(context.Context, string) (TestEventResult, error)

type TestEventResult struct {
	Network string `json:"network"`
	Address string `json:"address"`
	Payload string `json:"payload"`
}

type server struct {
	store       storage.Store
	frontend    fs.FS
	swaggerUI   fs.FS
	openAPISpec []byte
	testEvent   TestEventSender
	auth        *basicAuth
	buildID     string
}

type errorResponse struct {
	Error string `json:"error"`
}

type messagesResponse struct {
	Data []model.Message `json:"data"`
	Meta messagesMeta    `json:"meta"`
}

type messagesMeta struct {
	Count int `json:"count"`
	Limit int `json:"limit"`
}

type healthcheckResponse struct {
	Status string `json:"status"`
	Build  string `json:"build_id,omitempty"`
}

type testEventRequest struct {
	Message string `json:"message,omitempty"`
}

type testEventResponse struct {
	Status  string        `json:"status"`
	Network string        `json:"network"`
	Address string        `json:"address"`
	Payload string        `json:"payload"`
	Message string        `json:"message"`
	Meta    testEventMeta `json:"meta"`
}

type testEventMeta struct {
	DirectBrowserSyslog bool   `json:"direct_browser_syslog"`
	Delivery            string `json:"delivery"`
}

func NewRouter(cfg Config) (http.Handler, error) {
	if cfg.Store == nil {
		return nil, errors.New("store is required")
	}
	if cfg.Frontend == nil {
		return nil, errors.New("frontend filesystem is required")
	}
	if cfg.SwaggerUI == nil {
		return nil, errors.New("swagger ui filesystem is required")
	}
	if len(cfg.OpenAPISpec) == 0 {
		return nil, errors.New("openapi spec is required")
	}
	auth, err := newBasicAuth(cfg.Credentials)
	if err != nil {
		return nil, err
	}

	srv := &server{
		store:       cfg.Store,
		frontend:    cfg.Frontend,
		swaggerUI:   cfg.SwaggerUI,
		openAPISpec: cfg.OpenAPISpec,
		testEvent:   cfg.TestEvent,
		auth:        auth,
		buildID:     cfg.BuildID,
	}

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/api/v1/messages", srv.withMethod(http.MethodGet, srv.handleMessages))
	apiMux.HandleFunc("/api/v1/test-event", srv.withMethod(http.MethodPost, srv.handleTestEvent))

	mux := http.NewServeMux()
	mux.HandleFunc("/healthcheck", srv.withMethod(http.MethodGet, srv.handleHealthcheck))
	mux.Handle("/swagger.json", auth.require(http.HandlerFunc(srv.withMethod(http.MethodGet, srv.handleOpenAPI))))
	mux.Handle("/swagger-ui", auth.require(http.HandlerFunc(srv.handleSwaggerUIRedirect)))
	mux.Handle("/swagger-ui/", auth.require(http.HandlerFunc(srv.handleSwaggerUI)))
	mux.Handle("/api/v1/", auth.require(apiMux))
	mux.Handle("/", auth.require(http.HandlerFunc(srv.handleFrontend)))

	return mux, nil
}

func (s *server) handleHealthcheck(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthcheckResponse{
		Status: "ok",
		Build:  s.buildID,
	})
}

func (s *server) handleMessages(w http.ResponseWriter, r *http.Request) {
	query, err := parseMessagesQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}

	messages, err := s.store.Query(r.Context(), query)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "query messages"})
		return
	}

	writeJSON(w, http.StatusOK, messagesResponse{
		Data: messages,
		Meta: messagesMeta{
			Count: len(messages),
			Limit: query.Limit,
		},
	})
}

func (s *server) handleTestEvent(w http.ResponseWriter, r *http.Request) {
	if s.testEvent == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "test event sender is not configured"})
		return
	}

	var req testEventRequest
	if r.Body != nil && r.ContentLength != 0 {
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil && !errors.Is(err, io.EOF) {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid test event request"})
			return
		}
	}

	message := strings.TrimSpace(req.Message)
	result, err := s.testEvent(r.Context(), message)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, testEventResponse{
		Status:  "sent",
		Network: result.Network,
		Address: result.Address,
		Payload: result.Payload,
		Message: message,
		Meta: testEventMeta{
			DirectBrowserSyslog: false,
			Delivery:            "server-side syslog sender",
		},
	})
}

func (s *server) handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(s.openAPISpec)
}

func (s *server) handleSwaggerUIRedirect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodHead}, ", "))
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}
	http.Redirect(w, r, "/swagger-ui/", http.StatusMovedPermanently)
}

func (s *server) handleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodHead}, ", "))
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}

	name := strings.TrimPrefix(path.Clean(strings.TrimPrefix(r.URL.Path, "/swagger-ui/")), "/")
	if name == "." || name == "" {
		name = "index.html"
	}
	if !serveStaticFile(w, r, s.swaggerUI, name, false) {
		http.NotFound(w, r)
	}
}

func (s *server) handleFrontend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodHead}, ", "))
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}

	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "." || name == "" {
		name = "index.html"
	}

	if serveStaticFile(w, r, s.frontend, name, true) {
		return
	}
	serveStaticFile(w, r, s.frontend, "index.html", true)
}

func serveStaticFile(w http.ResponseWriter, r *http.Request, files fs.FS, name string, noStoreIndex bool) bool {
	file, err := files.Open(name)
	if err != nil {
		return false
	}
	defer func() {
		_ = file.Close()
	}()

	stat, err := file.Stat()
	if err != nil || stat.IsDir() {
		return false
	}

	data, err := io.ReadAll(file)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "read frontend asset"})
		return true
	}

	if noStoreIndex && name == "index.html" {
		w.Header().Set("Cache-Control", "no-store")
	}
	http.ServeContent(w, r, name, stat.ModTime(), bytes.NewReader(data))
	return true
}

func (s *server) withMethod(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
			return
		}
		next(w, r)
	}
}

func parseMessagesQuery(r *http.Request) (storage.Query, error) {
	values := r.URL.Query()

	limit := defaultMessageLimit
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil {
			return storage.Query{}, fmt.Errorf("invalid limit %q", rawLimit)
		}
		if parsed <= 0 || parsed > maxMessageLimit {
			return storage.Query{}, fmt.Errorf("limit must be between 1 and %d", maxMessageLimit)
		}
		limit = parsed
	}

	since, err := parseOptionalTime(values.Get("since"))
	if err != nil {
		return storage.Query{}, fmt.Errorf("invalid since: %w", err)
	}
	until, err := parseOptionalTime(values.Get("until"))
	if err != nil {
		return storage.Query{}, fmt.Errorf("invalid until: %w", err)
	}

	return storage.Query{
		Text:  values.Get("q"),
		Limit: limit,
		Since: since,
		Until: until,
	}, nil
}

func parseOptionalTime(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
