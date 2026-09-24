package incidents

import (
	"context"
	"reflect"
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
	if pending := singleIncident(t, engine); pending.State != StatePendingFailure {
		t.Fatalf("state = %q, want %q", pending.State, StatePendingFailure)
	}

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

func TestEngineDetectsNetifdWANFailureAndRecovery(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 23, 10, 36, 54, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})

	observe(t, engine, netifdMessage("router-a", "Interface 'wan' has lost the connection", start, "down"), true)
	observe(t, engine, netifdMessage("router-a", "Interface 'wan' is now down", start.Add(6*time.Second), "down-again"), true)
	engine.Tick(context.Background(), start.Add(time.Minute))
	if got := singleIncident(t, engine).State; got != StateActive {
		t.Fatalf("state = %q, want %q", got, StateActive)
	}

	observe(t, engine, netifdMessage("router-a", "Interface 'wan' is now up", start.Add(82*time.Second), "up"), true)
	engine.Tick(context.Background(), start.Add(142*time.Second))
	if got := singleIncident(t, engine).State; got != StateResolved {
		t.Fatalf("state = %q, want %q", got, StateResolved)
	}
}

func TestReachabilityFailureIgnoresInterfaceOnlyRecovery(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 18, 16, 17, 18, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})

	observe(t, engine, wanMessage("router-a", "offline", start, "reachability-down"), true)
	engine.Tick(context.Background(), start.Add(time.Minute))
	observe(t, engine, netifdMessage("router-a", "Interface 'wan' is now up", start.Add(70*time.Second), "interface-up"), true)
	engine.Tick(context.Background(), start.Add(3*time.Minute))
	if got := singleIncident(t, engine).State; got != StateActive {
		t.Fatalf("state after interface-only recovery = %q, want %q", got, StateActive)
	}

	observe(t, engine, wanMessage("router-a", "online", start.Add(4*time.Minute), "reachability-up"), true)
	engine.Tick(context.Background(), start.Add(5*time.Minute))
	if got := singleIncident(t, engine).State; got != StateResolved {
		t.Fatalf("state after reachability recovery = %q, want %q", got, StateResolved)
	}
}

func TestEngineSuppressesShortPrimaryAndFallbackFlaps(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 23, 15, 44, 50, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})

	observe(t, engine, netifdMessage("router-a", "Interface 'tethering' is now up", start, "4g-up"), true)
	observe(t, engine, netifdMessage("router-a", "Interface 'wwan' is now down", start.Add(time.Second), "starlink-down"), true)
	observe(t, engine, netifdMessage("router-a", "Interface 'wan' is now down", start.Add(2*time.Second), "wan-down"), true)
	observe(t, engine, netifdMessage("router-a", "Interface 'wwan' is now up", start.Add(31*time.Second), "starlink-up"), true)
	observe(t, engine, netifdMessage("router-a", "Interface 'wan' is now up", start.Add(32*time.Second), "wan-up"), true)
	engine.Tick(context.Background(), start.Add(2*time.Minute))

	assertIncidentCount(t, engine, 0)
}

func TestEngineAlertsOnSustainedBackupFailureWhilePrimaryIsHealthy(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})

	observe(t, engine, wanMessage("router-a", "online", start, "wan-up"), true)
	observe(t, engine, repeaterMessage("router-a", "wwan", "offline", start.Add(time.Second), "starlink-down"), true)
	engine.Tick(context.Background(), start.Add(61*time.Second))

	incident := singleIncident(t, engine)
	if incident.Interface != "wwan" || incident.State != StateActive {
		t.Fatalf("incident = %#v, want active wwan fallback incident", incident)
	}
	assertNotificationKinds(t, engine, NotificationFallbackOpened)
}

func TestStarlinkSelectionArmsMonitoringWithoutChangingIncidentState(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})
	selected := model.Message{ID: "selected", ReceivedAt: start, Hostname: "router-a", Tag: "gl-repeater", Message: "(repeater.lua:1253) switch to STARLINK"}

	observe(t, engine, selected, true)
	engine.Tick(context.Background(), start.Add(2*time.Minute))

	assertIncidentCount(t, engine, 0)
	if got := engine.Snapshot().Trackers["router-a\x00wwan"].Phase; got != phaseHealthy {
		t.Fatalf("tracker phase = %q, want armed healthy tracker", got)
	}
}

func TestEngineResolvesBackupIncidentAfterStableRecovery(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})

	observe(t, engine, netifdMessage("router-a", "Interface 'tethering' is now down", start, "4g-down"), true)
	engine.Tick(context.Background(), start.Add(time.Minute))
	observe(t, engine, netifdMessage("router-a", "Interface 'tethering' is now up", start.Add(70*time.Second), "4g-up"), true)
	engine.Tick(context.Background(), start.Add(130*time.Second))

	incident := singleIncident(t, engine)
	if incident.Interface != "tethering" || incident.State != StateResolved {
		t.Fatalf("incident = %#v, want resolved tethering fallback incident", incident)
	}
	assertNotificationKinds(t, engine, NotificationFallbackOpened, NotificationFallbackResolved)
}

func TestInterfaceUpRecoversReachabilityDetectedBackupFailure(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})

	observe(t, engine, repeaterMessage("router-a", "wwan", "offline", start, "starlink-down"), true)
	engine.Tick(context.Background(), start.Add(time.Minute))
	observe(t, engine, netifdMessage("router-a", "Interface 'wwan' is now up", start.Add(70*time.Second), "starlink-up"), true)
	engine.Tick(context.Background(), start.Add(130*time.Second))

	if got := singleIncident(t, engine).State; got != StateResolved {
		t.Fatalf("state = %q, want %q", got, StateResolved)
	}
}

func TestEngineEscalatesActiveBackupFailureWhenPrimaryFails(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})

	observe(t, engine, repeaterMessage("router-a", "wwan", "offline", start, "starlink-down"), true)
	engine.Tick(context.Background(), start.Add(time.Minute))
	observe(t, engine, wanMessage("router-a", "offline", start.Add(70*time.Second), "wan-down"), true)
	engine.Tick(context.Background(), start.Add(130*time.Second))

	assertNotificationKindCounts(t, engine, map[NotificationKind]int{
		NotificationFallbackOpened:    1,
		NotificationOpened:            1,
		NotificationFallbackEscalated: 1,
	})
}

func TestEngineEscalatesBackupFailureThatStartsWhilePrimaryIsDown(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})

	observe(t, engine, wanMessage("router-a", "offline", start, "wan-down"), true)
	engine.Tick(context.Background(), start.Add(time.Minute))
	observe(t, engine, netifdMessage("router-a", "Interface 'tethering' is now down", start.Add(70*time.Second), "4g-down"), true)
	engine.Tick(context.Background(), start.Add(130*time.Second))

	assertNotificationKindCounts(t, engine, map[NotificationKind]int{
		NotificationOpened:            1,
		NotificationFallbackEscalated: 1,
	})
}

func TestEngineSimultaneousPrimaryAndBackupFailureSendsOnlyEscalation(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})

	observe(t, engine, wanMessage("router-a", "offline", start, "wan-down"), true)
	observe(t, engine, netifdMessage("router-a", "Interface 'tethering' is now down", start, "4g-down"), true)
	engine.Tick(context.Background(), start.Add(time.Minute))

	assertNotificationKindCounts(t, engine, map[NotificationKind]int{
		NotificationOpened:            1,
		NotificationFallbackEscalated: 1,
	})
}

func TestEngineProcessesDelayedPrimaryAndBackupTransitionsInTimeOrder(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})

	observe(t, engine, repeaterMessage("router-a", "wwan", "offline", start, "starlink-down"), true)
	observe(t, engine, wanMessage("router-a", "offline", start.Add(30*time.Second), "wan-down"), true)
	engine.Tick(context.Background(), start.Add(2*time.Minute))

	jobs := engine.DueNotifications(start.Add(2 * time.Minute))
	if len(jobs) != 3 {
		t.Fatalf("jobs = %#v, want fallback warning, primary opening, and escalation", jobs)
	}
	want := map[NotificationKind]time.Time{
		NotificationFallbackOpened:    start.Add(time.Minute),
		NotificationOpened:            start.Add(90 * time.Second),
		NotificationFallbackEscalated: start.Add(90 * time.Second),
	}
	for _, job := range jobs {
		if expected, exists := want[job.Kind]; !exists || !job.CreatedAt.Equal(expected) {
			t.Fatalf("job = %#v, want timestamps %#v", job, want)
		}
	}
}

func TestEngineProcessesRecoveryBeforeActivationAtTheSameTime(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})

	observe(t, engine, repeaterMessage("router-a", "wwan", "offline", start, "starlink-down"), true)
	engine.Tick(context.Background(), start.Add(time.Minute))
	observe(t, engine, repeaterMessage("router-a", "wwan", "online", start.Add(70*time.Second), "starlink-up"), true)
	observe(t, engine, wanMessage("router-a", "offline", start.Add(70*time.Second), "wan-down"), true)
	engine.Tick(context.Background(), start.Add(130*time.Second))

	assertNotificationKindCounts(t, engine, map[NotificationKind]int{
		NotificationFallbackOpened:   1,
		NotificationFallbackResolved: 1,
		NotificationOpened:           1,
	})
}

func TestEngineAlertsAgainForDistinctFallbackFailure(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})

	observe(t, engine, repeaterMessage("router-a", "wwan", "offline", start, "down-1"), true)
	observe(t, engine, repeaterMessage("router-a", "wwan", "offline", start.Add(10*time.Second), "down-1-repeat"), true)
	engine.Tick(context.Background(), start.Add(time.Minute))
	observe(t, engine, repeaterMessage("router-a", "wwan", "online", start.Add(70*time.Second), "up-1"), true)
	engine.Tick(context.Background(), start.Add(130*time.Second))
	observe(t, engine, repeaterMessage("router-a", "wwan", "offline", start.Add(140*time.Second), "down-2"), true)
	engine.Tick(context.Background(), start.Add(200*time.Second))

	assertNotificationKindCounts(t, engine, map[NotificationKind]int{
		NotificationFallbackOpened:   2,
		NotificationFallbackResolved: 1,
	})
}

func TestEngineEscalatesEachPrimaryFailureDuringOneBackupIncident(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})

	observe(t, engine, repeaterMessage("router-a", "wwan", "offline", start, "starlink-down"), true)
	engine.Tick(context.Background(), start.Add(time.Minute))
	observe(t, engine, wanMessage("router-a", "offline", start.Add(70*time.Second), "wan-down-1"), true)
	engine.Tick(context.Background(), start.Add(130*time.Second))
	observe(t, engine, wanMessage("router-a", "online", start.Add(140*time.Second), "wan-up-1"), true)
	engine.Tick(context.Background(), start.Add(200*time.Second))
	observe(t, engine, wanMessage("router-a", "offline", start.Add(210*time.Second), "wan-down-2"), true)
	engine.Tick(context.Background(), start.Add(270*time.Second))

	assertNotificationKindCounts(t, engine, map[NotificationKind]int{
		NotificationFallbackOpened:    1,
		NotificationOpened:            2,
		NotificationResolved:          1,
		NotificationFallbackEscalated: 2,
	})
}

func TestPrimaryRecoveryDoesNotResolveBackupIncident(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})

	observe(t, engine, wanMessage("router-a", "offline", start, "wan-down"), true)
	observe(t, engine, repeaterMessage("router-a", "wwan", "offline", start, "starlink-down"), true)
	engine.Tick(context.Background(), start.Add(time.Minute))
	observe(t, engine, wanMessage("router-a", "online", start.Add(70*time.Second), "wan-up"), true)
	engine.Tick(context.Background(), start.Add(130*time.Second))

	incidents := engine.List(Query{})
	if len(incidents) != 2 {
		t.Fatalf("incidents = %#v, want primary and fallback", incidents)
	}
	states := make(map[string]State, len(incidents))
	for _, incident := range incidents {
		states[incident.Interface] = incident.State
	}
	if states["wan"] != StateResolved || states["wwan"] != StateActive {
		t.Fatalf("states = %#v, want resolved wan and active wwan", states)
	}
}

func TestRebuildRestoresFallbackNotificationKinds(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})
	observations := []Observation{
		{Message: netifdMessage("router-a", "Interface 'tethering' is now down", start, "down"), NotifyEligible: true},
		{Message: netifdMessage("router-a", "Interface 'tethering' is now up", start.Add(70*time.Second), "up"), NotifyEligible: true},
	}
	if err := engine.Rebuild(context.Background(), observations, nil, start.Add(130*time.Second)); err != nil {
		t.Fatal(err)
	}
	engine.Tick(context.Background(), start.Add(130*time.Second))

	assertNotificationKinds(t, engine, NotificationFallbackOpened, NotificationFallbackResolved)
}

func TestRebuildRestoresLaterCombinedRiskEscalation(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	engine := newTestEngine(t, Config{DownAfter: time.Minute, RecoveredAfter: time.Minute})
	messages := []model.Message{
		repeaterMessage("router-a", "wwan", "offline", start, "starlink-down"),
		wanMessage("router-a", "offline", start.Add(70*time.Second), "wan-down"),
		wanMessage("router-a", "offline", start.Add(130*time.Second), "wan-down-again"),
	}
	live := make([]Observation, len(messages))
	historical := make([]Observation, len(messages))
	for index, message := range messages {
		live[index] = Observation{Message: message, NotifyEligible: true}
		historical[index] = Observation{Message: message, NotifyEligible: false}
	}
	if err := engine.Rebuild(context.Background(), live, nil, start.Add(130*time.Second)); err != nil {
		t.Fatal(err)
	}
	state := engine.Snapshot()
	state.Jobs = map[string]NotificationJob{}
	if err := engine.ReplaceSnapshot(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if err := engine.Rebuild(context.Background(), historical, nil, start.Add(130*time.Second)); err != nil {
		t.Fatal(err)
	}

	assertNotificationKindCounts(t, engine, map[NotificationKind]int{
		NotificationFallbackOpened:    1,
		NotificationOpened:            1,
		NotificationFallbackEscalated: 1,
	})
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
	cases[2].Message = "interface lan status offline"
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
	return repeaterMessage(host, "wan", state, received, id)
}

func repeaterMessage(host, iface, state string, received time.Time, id string) model.Message {
	return model.Message{
		ID:         id,
		ReceivedAt: received,
		Hostname:   host,
		Tag:        "gl-repeater",
		Message:    "(repeater.lua:1741) interface " + iface + " status " + state,
		Transport:  "udp/tcp",
	}
}

func netifdMessage(host, message string, received time.Time, id string) model.Message {
	return model.Message{ID: id, ReceivedAt: received, Hostname: host, Tag: "netifd", Message: message, Transport: "udp/tcp"}
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

func assertNotificationKindCounts(t *testing.T, engine *Service, want map[NotificationKind]int) {
	t.Helper()
	got := make(map[NotificationKind]int)
	for _, job := range engine.PendingNotifications() {
		got[job.Kind]++
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("notification counts = %#v, want %#v", got, want)
	}
}
