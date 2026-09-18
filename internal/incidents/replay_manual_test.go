package incidents

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/timmersuk/logthing/internal/notification"
	"github.com/timmersuk/logthing/internal/storage"
)

func TestManualHistoricalReplay(t *testing.T) {
	root := os.Getenv("LOGTHING_REPLAY_DIR")
	if root == "" {
		t.Skip("set LOGTHING_REPLAY_DIR for a full historical replay")
	}
	store, err := storage.NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(Config{Interface: "wan", DownAfter: time.Minute, RecoveredAfter: time.Minute}, nil)
	if err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(engine, store, notification.DiscardNotifier{}, time.Now().UTC())
	if err := worker.reconcile(context.Background(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	incidents := engine.List(Query{})
	for _, incident := range incidents {
		if incident.Notify {
			t.Fatalf("historical incident %s was notification eligible", incident.ID)
		}
		t.Logf("%s %s %v", incident.State, incident.StartedAt, incident.ResolvedAt)
	}
	t.Logf("historical sustained incidents: %d", len(incidents))
}
