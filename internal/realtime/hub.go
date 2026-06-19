package realtime

import (
	"context"
	"sync"

	"github.com/timmersuk/logthing/internal/model"
	"github.com/timmersuk/logthing/internal/storage"
)

const subscriberBuffer = 64

type Hub struct {
	mu          sync.Mutex
	subscribers map[chan model.Message]struct{}
}

func NewHub() *Hub {
	return &Hub{subscribers: make(map[chan model.Message]struct{})}
}

func (h *Hub) Subscribe() (<-chan model.Message, func()) {
	ch := make(chan model.Message, subscriberBuffer)

	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()

	return ch, func() {
		h.mu.Lock()
		delete(h.subscribers, ch)
		h.mu.Unlock()
		close(ch)
	}
}

func (h *Hub) Publish(msg model.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for ch := range h.subscribers {
		select {
		case ch <- msg:
		default:
		}
	}
}

type PublishingStore struct {
	store     storage.Store
	publisher interface {
		Publish(model.Message)
	}
}

func NewPublishingStore(store storage.Store, publisher interface {
	Publish(model.Message)
}) *PublishingStore {
	return &PublishingStore{
		store:     store,
		publisher: publisher,
	}
}

func (s *PublishingStore) Append(ctx context.Context, msg model.Message) error {
	if err := s.store.Append(ctx, msg); err != nil {
		return err
	}
	if s.publisher != nil {
		s.publisher.Publish(msg)
	}
	return nil
}

func (s *PublishingStore) Query(ctx context.Context, query storage.Query) ([]model.Message, error) {
	return s.store.Query(ctx, query)
}
