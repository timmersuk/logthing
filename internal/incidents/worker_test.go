package incidents

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/timmersuk/logthing/internal/notification"
	"github.com/timmersuk/logthing/internal/storage"
)

func TestWorkerReconcilesStoredEvidenceAndDeliversLifecycle(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := storage.NewFileStore(filepath.Join(root, "messages"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	persistence := NewFilePersistence(filepath.Join(root, "state", "incidents.json"))
	engine, err := New(Config{DownAfter: time.Minute, RecoveredAfter: time.Minute}, persistence)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := engine.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	notifier := &recordingNotifier{}
	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	worker := NewWorker(engine, store, notifier, start.Add(-time.Second))

	down := wanMessage("router-a", "offline", start, "down")
	if err := store.Append(context.Background(), down); err != nil {
		t.Fatalf("Append(down) error = %v", err)
	}
	if err := worker.reconcile(context.Background(), start.Add(time.Minute)); err != nil {
		t.Fatalf("reconcile(down) error = %v", err)
	}
	if got := notifier.kinds(); len(got) != 1 || got[0] != notification.KindIncidentOpened {
		t.Fatalf("notification kinds = %#v, want opened", got)
	}

	up := wanMessage("router-a", "online", start.Add(70*time.Second), "up")
	if err := store.Append(context.Background(), up); err != nil {
		t.Fatalf("Append(up) error = %v", err)
	}
	if err := worker.reconcile(context.Background(), start.Add(130*time.Second)); err != nil {
		t.Fatalf("reconcile(up) error = %v", err)
	}
	if got := notifier.kinds(); len(got) != 2 || got[1] != notification.KindIncidentResolved {
		t.Fatalf("notification kinds = %#v, want opened, resolved", got)
	}
}

func TestWorkerDoesNotNotifyForRecordsPresentAtStartup(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := storage.NewFileStore(filepath.Join(root, "messages"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	message := wanMessage("router-a", "offline", start, "historical")
	message.Transport = "import"
	if err := store.Append(context.Background(), message); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})
	notifier := &recordingNotifier{}
	worker := NewWorker(engine, store, notifier, start.Add(time.Hour))
	if err := worker.reconcile(context.Background(), start.Add(2*time.Hour)); err != nil {
		t.Fatalf("reconcile() error = %v", err)
	}
	if len(notifier.kinds()) != 0 {
		t.Fatalf("historical import sent notifications: %#v", notifier.kinds())
	}
	if got := singleIncident(t, engine).State; got != StateActive {
		t.Fatalf("rebuilt incident state = %q, want active", got)
	}
}

type recordingNotifier struct {
	mu       sync.Mutex
	messages []notification.Notification
}

func (n *recordingNotifier) Send(_ context.Context, message notification.Notification) (notification.Receipt, error) {
	n.mu.Lock()
	n.messages = append(n.messages, message)
	n.mu.Unlock()
	return notification.Receipt{Adapter: "recording", ExternalID: message.EventID}, nil
}

func (n *recordingNotifier) kinds() []notification.Kind {
	n.mu.Lock()
	defer n.mu.Unlock()
	kinds := make([]notification.Kind, len(n.messages))
	for index, message := range n.messages {
		kinds[index] = message.Kind
	}
	return kinds
}
