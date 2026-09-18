package incidents

import (
	"context"
	"testing"
	"time"

	"github.com/timmersuk/logthing/internal/model"
)

func TestEngineRequiresSustainedFailureAndRecovery(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})

	observe(t, engine, wanMessage("router-a", "offline", start, "down-1"), true)
	engine.Tick(context.Background(), start.Add(59*time.Second))
	assertIncidentCount(t, engine, 0)

	engine.Tick(context.Background(), start.Add(time.Minute))
	active := singleIncident(t, engine)
	if active.State != StateActive {
		t.Fatalf("state = %q, want %q", active.State, StateActive)
	}
	assertNotificationKinds(t, engine, NotificationOpened)

	observe(t, engine, wanMessage("router-a", "online", start.Add(70*time.Second), "up-1"), true)
	engine.Tick(context.Background(), start.Add(129*time.Second))
	if got := singleIncident(t, engine).State; got != StatePendingRecovery {
		t.Fatalf("state before recovery debounce = %q, want %q", got, StatePendingRecovery)
	}

	engine.Tick(context.Background(), start.Add(130*time.Second))
	resolved := singleIncident(t, engine)
	if resolved.State != StateResolved {
		t.Fatalf("state = %q, want %q", resolved.State, StateResolved)
	}
	if resolved.ResolvedAt == nil || !resolved.ResolvedAt.Equal(start.Add(130*time.Second)) {
		t.Fatalf("resolved_at = %v, want %v", resolved.ResolvedAt, start.Add(130*time.Second))
	}
	assertNotificationKinds(t, engine, NotificationOpened, NotificationResolved)
}

func TestEngineSuppressesShortFlap(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})
	observe(t, engine, wanMessage("router-a", "offline", start, "down"), true)
	observe(t, engine, wanMessage("router-a", "online", start.Add(3*time.Second), "up"), true)
	engine.Tick(context.Background(), start.Add(2*time.Minute))
	assertIncidentCount(t, engine, 0)
	assertNotificationKinds(t, engine)
}

func TestEngineRecoveryFlapKeepsSameIncident(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})
	observe(t, engine, wanMessage("router-a", "offline", start, "down-1"), true)
	engine.Tick(context.Background(), start.Add(time.Minute))
	original := singleIncident(t, engine)

	observe(t, engine, wanMessage("router-a", "online", start.Add(70*time.Second), "up-1"), true)
	observe(t, engine, wanMessage("router-a", "offline", start.Add(100*time.Second), "down-2"), true)
	engine.Tick(context.Background(), start.Add(3*time.Minute))

	current := singleIncident(t, engine)
	if current.ID != original.ID {
		t.Fatalf("incident ID changed from %q to %q", original.ID, current.ID)
	}
	if current.State != StateActive {
		t.Fatalf("state = %q, want %q", current.State, StateActive)
	}
	assertNotificationKinds(t, engine, NotificationOpened)
}

func TestEngineIsolatesHosts(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})
	observe(t, engine, wanMessage("router-a", "offline", start, "a-down"), true)
	observe(t, engine, wanMessage("router-b", "online", start.Add(70*time.Second), "b-up"), true)
	engine.Tick(context.Background(), start.Add(70*time.Second))

	incident := singleIncident(t, engine)
	if incident.Hostname != "router-a" || incident.State != StateActive {
		t.Fatalf("incident = %#v, want active router-a incident", incident)
	}
}

func TestEngineIgnoresUnsupportedEvidence(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})
	cases := []model.Message{
		wanMessage("", "offline", start, "empty-host"),
		wanMessage("router-a", "offline", start, "wrong-tag"),
		wanMessage("router-a", "offline", start, "wrong-interface"),
		wanMessage("router-a", "offline", start, "quoted-status"),
	}
	cases[1].Tag = "netifd"
	cases[2].Message = "interface wwan status offline"
	cases[3].Message = "diagnostic: (repeater.lua:1741) interface wan status offline"
	for _, message := range cases {
		observe(t, engine, message, true)
	}
	engine.Tick(context.Background(), start.Add(2*time.Minute))
	assertIncidentCount(t, engine, 0)
}

func TestEngineSuppressesNotificationsForHistoricalEvidence(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})
	observe(t, engine, wanMessage("router-a", "offline", start, "down"), false)
	engine.Tick(context.Background(), start.Add(time.Minute))

	if got := singleIncident(t, engine).State; got != StateActive {
		t.Fatalf("state = %q, want %q", got, StateActive)
	}
	assertNotificationKinds(t, engine)
}

func newTestEngine(t *testing.T, cfg Config) *Service {
	t.Helper()
	engine, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return engine
}

func wanMessage(host, state string, received time.Time, id string) model.Message {
	return model.Message{
		ID:         id,
		ReceivedAt: received,
		Hostname:   host,
		Tag:        "gl-repeater",
		Message:    "(repeater.lua:1741) interface wan status " + state,
		Transport:  "udp/tcp",
	}
}

func observe(t *testing.T, engine *Service, message model.Message, eligible bool) {
	t.Helper()
	if err := engine.Observe(context.Background(), message, eligible); err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
}

func singleIncident(t *testing.T, engine *Service) Incident {
	t.Helper()
	items := engine.List(Query{})
	if len(items) != 1 {
		t.Fatalf("incident count = %d, want 1", len(items))
	}
	return items[0]
}

func assertIncidentCount(t *testing.T, engine *Service, want int) {
	t.Helper()
	if got := len(engine.List(Query{})); got != want {
		t.Fatalf("incident count = %d, want %d", got, want)
	}
}

func assertNotificationKinds(t *testing.T, engine *Service, want ...NotificationKind) {
	t.Helper()
	jobs := engine.PendingNotifications()
	if len(jobs) != len(want) {
		t.Fatalf("notification count = %d, want %d (%#v)", len(jobs), len(want), jobs)
	}
	for i := range want {
		if jobs[i].Kind != want[i] {
			t.Fatalf("notification %d kind = %q, want %q", i, jobs[i].Kind, want[i])
		}
	}
}
