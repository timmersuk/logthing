package realtime

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/timmersuk/logthing/internal/model"
	"github.com/timmersuk/logthing/internal/storage"
)

type recordingStore struct {
	appendErr error
	appends   []model.Message
	queries   []storage.Query
	results   []model.Message
}

func (s *recordingStore) Append(_ context.Context, msg model.Message) error {
	if s.appendErr != nil {
		return s.appendErr
	}
	s.appends = append(s.appends, msg)
	return nil
}

func (s *recordingStore) Query(_ context.Context, query storage.Query) ([]model.Message, error) {
	s.queries = append(s.queries, query)
	return s.results, nil
}

type recordingPublisher struct {
	messages []model.Message
}

func (p *recordingPublisher) Publish(msg model.Message) {
	p.messages = append(p.messages, msg)
}

func TestHubPublishesToSubscriber(t *testing.T) {
	hub := NewHub()
	messages, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	want := model.Message{ID: "one", ReceivedAt: time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)}
	hub.Publish(want)

	select {
	case got := <-messages:
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("message = %#v, want %#v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for published message")
	}
}

func TestPublishingStorePublishesAfterSuccessfulAppend(t *testing.T) {
	store := &recordingStore{}
	publisher := &recordingPublisher{}
	wrapped := NewPublishingStore(store, publisher)

	want := model.Message{ID: "one"}
	if err := wrapped.Append(context.Background(), want); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	if !reflect.DeepEqual(store.appends, []model.Message{want}) {
		t.Fatalf("store appends = %#v, want appended message", store.appends)
	}
	if !reflect.DeepEqual(publisher.messages, []model.Message{want}) {
		t.Fatalf("published messages = %#v, want appended message", publisher.messages)
	}
}

func TestPublishingStoreDoesNotPublishFailedAppend(t *testing.T) {
	store := &recordingStore{appendErr: errors.New("append failed")}
	publisher := &recordingPublisher{}
	wrapped := NewPublishingStore(store, publisher)

	err := wrapped.Append(context.Background(), model.Message{ID: "one"})
	if err == nil {
		t.Fatal("Append() error = nil, want error")
	}
	if len(publisher.messages) != 0 {
		t.Fatalf("published messages = %#v, want none", publisher.messages)
	}
}

func TestPublishingStoreDelegatesQuery(t *testing.T) {
	wantQuery := storage.Query{Text: "error", Limit: 10}
	wantMessages := []model.Message{{ID: "one"}}
	store := &recordingStore{results: wantMessages}
	wrapped := NewPublishingStore(store, nil)

	got, err := wrapped.Query(context.Background(), wantQuery)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if !reflect.DeepEqual(got, wantMessages) {
		t.Fatalf("messages = %#v, want %#v", got, wantMessages)
	}
	if !reflect.DeepEqual(store.queries, []storage.Query{wantQuery}) {
		t.Fatalf("queries = %#v, want delegated query", store.queries)
	}
}
