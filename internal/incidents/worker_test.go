package incidents

import (
	"context"
	"encoding/json"
	"os"
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

func TestWorkerNotifiesForUncheckpointedLiveRecordAfterRestart(t *testing.T) {
	root := t.TempDir()
	store, err := storage.NewFileStore(filepath.Join(root, "messages"))
	if err != nil {
		t.Fatal(err)
	}
	persistence := NewFilePersistence(filepath.Join(root, "state", "incidents.json"))
	cfg := Config{DownAfter: time.Minute, RecoveredAfter: time.Minute}
	before, _ := New(cfg, persistence)
	firstWorker := NewWorker(before, store, &recordingNotifier{}, time.Now().UTC())
	if err := firstWorker.reconcile(context.Background(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	downAt := time.Now().UTC().Add(time.Second)
	if err := store.Append(context.Background(), wanMessage("router-a", "offline", downAt, "uncheckpointed")); err != nil {
		t.Fatal(err)
	}
	after, _ := New(cfg, persistence)
	if err := after.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	notifier := &recordingNotifier{}
	restarted := NewWorker(after, store, notifier, downAt.Add(time.Second))
	if err := restarted.reconcile(context.Background(), downAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := notifier.kinds(); len(got) != 1 || got[0] != notification.KindIncidentOpened {
		t.Fatalf("notification kinds = %v, want opened", got)
	}
}

func TestWorkerRebuildsAfterPartitionReplacement(t *testing.T) {
	root := t.TempDir()
	store, err := storage.NewFileStore(filepath.Join(root, "messages"))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})
	worker := NewWorker(engine, store, &recordingNotifier{}, start.Add(-time.Second))
	if err := store.Append(context.Background(), wanMessage("router-a", "offline", start, "down")); err != nil {
		t.Fatal(err)
	}
	if err := worker.reconcile(context.Background(), start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	replacement := wanMessage("router-a", "online", start.Add(2*time.Minute), "replacement")
	data, err := json.Marshal(replacement)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "messages", "2026", "09", "18", "router-a.ndjson")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := worker.reconcile(context.Background(), start.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := len(engine.List(Query{})); got != 0 {
		t.Fatalf("incident count = %d, want rebuilt empty state", got)
	}
}

func TestWorkerRebuildPreservesEligibilityForNewPartition(t *testing.T) {
	root := t.TempDir()
	store, err := storage.NewFileStore(filepath.Join(root, "messages"))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})
	notifier := &recordingNotifier{}
	worker := NewWorker(engine, store, notifier, start.Add(-time.Second))
	if err := store.Append(context.Background(), wanMessage("router-a", "online", start, "old-online")); err != nil {
		t.Fatal(err)
	}
	if err := worker.reconcile(context.Background(), start.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	replacement := wanMessage("router-a", "online", start, "replacement-online")
	data, err := json.Marshal(replacement)
	if err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(root, "messages", "2026", "09", "17", "router-a.ndjson")
	if err := os.WriteFile(oldPath, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	downAt := start.Add(24 * time.Hour)
	if err := store.Append(context.Background(), wanMessage("router-a", "offline", downAt, "new-down")); err != nil {
		t.Fatal(err)
	}
	if err := worker.reconcile(context.Background(), downAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := notifier.kinds(); len(got) != 1 || got[0] != notification.KindIncidentOpened {
		t.Fatalf("notification kinds = %v, want opened", got)
	}
}

func TestWorkerRebuildPreservesEligibilityForNewRecordInChangedPartition(t *testing.T) {
	root := t.TempDir()
	store, err := storage.NewFileStore(filepath.Join(root, "messages"))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})
	notifier := &recordingNotifier{}
	worker := NewWorker(engine, store, notifier, start.Add(-time.Second))
	online := wanMessage("router-a", "online", start, "old-online")
	if err := store.Append(context.Background(), online); err != nil {
		t.Fatal(err)
	}
	checkpointAt := start.Add(time.Second)
	if err := worker.reconcile(context.Background(), checkpointAt); err != nil {
		t.Fatal(err)
	}

	replacementOnline := wanMessage("router-a", "online", start, "replacement-online")
	downAt := checkpointAt.Add(time.Second)
	down := wanMessage("router-a", "offline", downAt, "new-down")
	first, _ := json.Marshal(replacementOnline)
	second, _ := json.Marshal(down)
	body := append(append(append([]byte{}, first...), '\n'), second...)
	body = append(body, '\n')
	path := filepath.Join(root, "messages", "2026", "09", "18", "router-a.ndjson")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := worker.reconcile(context.Background(), downAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := notifier.kinds(); len(got) != 1 || got[0] != notification.KindIncidentOpened {
		t.Fatalf("notification kinds = %v, want opened", got)
	}
}

func TestWorkerHealthBecomesStale(t *testing.T) {
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})
	worker := NewWorker(engine, nil, &recordingNotifier{}, time.Now())
	old := time.Now().Add(-2 * time.Minute)
	worker.health = WorkerHealth{Running: true, LastReconciledAt: &old}
	if health := worker.Health(); !health.Stale {
		t.Fatal("health was not stale")
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
