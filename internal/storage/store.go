package storage

import (
	"context"
	"time"

	"github.com/timmersuk/logthing/internal/model"
)

type Query struct {
	Text  string
	Limit int
	Since *time.Time
	Until *time.Time
}

type Store interface {
	Append(ctx context.Context, msg model.Message) error
	Query(ctx context.Context, query Query) ([]model.Message, error)
}
