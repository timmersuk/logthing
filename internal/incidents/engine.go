package incidents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/timmersuk/logthing/internal/model"
	"github.com/timmersuk/logthing/internal/storage"
)

type State string

const (
	StatePendingFailure  State = "pending_failure"
	StateActive          State = "active"
	StatePendingRecovery State = "pending_recovery"
	StateResolved        State = "resolved"
)

type NotificationKind string

const (
	NotificationOpened   NotificationKind = "incident_opened"
	NotificationResolved NotificationKind = "incident_resolved"
)

type Config struct {
	Interface      string
	DownAfter      time.Duration
	RecoveredAfter time.Duration
}

type Observation struct {
	Message        model.Message
	NotifyEligible bool
	SourceRef      string
}

type Incident struct {
	ID              string        `json:"id"`
	RuleVersion     int           `json:"rule_version"`
	Hostname        string        `json:"hostname"`
	Interface       string        `json:"interface"`
	State           State         `json:"state"`
	StartedAt       time.Time     `json:"started_at"`
	ActivatedAt     *time.Time    `json:"activated_at,omitempty"`
	RecoveryFirstAt *time.Time    `json:"recovery_first_at,omitempty"`
	ResolvedAt      *time.Time    `json:"resolved_at,omitempty"`
	LastEvidenceAt  time.Time     `json:"last_evidence_at"`
	EvidenceIDs     []string      `json:"evidence_ids"`
	EvidenceRefs    []string      `json:"evidence_refs,omitempty"`
	DownAfter       time.Duration `json:"down_after"`
	RecoveredAfter  time.Duration `json:"recovered_after"`
	Notify          bool          `json:"notify"`
}

func (s *Service) NotificationsFor(incidentID string) []NotificationJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	jobs := make([]NotificationJob, 0, 2)
	for _, job := range s.state.Jobs {
		if job.IncidentID == incidentID {
			jobs = append(jobs, job)
		}
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt.Before(jobs[j].CreatedAt) })
	return jobs
}

type NotificationJob struct {
	ID         string           `json:"id"`
	IncidentID string           `json:"incident_id"`
	Kind       NotificationKind `json:"kind"`
	CreatedAt  time.Time        `json:"created_at"`
	Attempts   int              `json:"attempts"`
	NextAt     time.Time        `json:"next_at"`
	LastError  string           `json:"last_error,omitempty"`
	SentAt     *time.Time       `json:"sent_at,omitempty"`
	Adapter    string           `json:"adapter,omitempty"`
	ExternalID string           `json:"external_id,omitempty"`
	Permanent  bool             `json:"permanent,omitempty"`
}

type Query struct {
	State  State
	Limit  int
	Offset int
}

type phase string

const (
	phaseHealthy         phase = "healthy"
	phasePendingFailure  phase = "pending_failure"
	phaseActive          phase = "active"
	phasePendingRecovery phase = "pending_recovery"
)

type tracker struct {
	Phase          phase     `json:"phase"`
	PendingSince   time.Time `json:"pending_since,omitempty"`
	RecoverySince  time.Time `json:"recovery_since,omitempty"`
	LastEvidenceAt time.Time `json:"last_evidence_at,omitempty"`
	StartMessageID string    `json:"start_message_id,omitempty"`
	StartSourceRef string    `json:"start_source_ref,omitempty"`
	IncidentID     string    `json:"incident_id,omitempty"`
	Notify         bool      `json:"notify"`
}

type Snapshot struct {
	Version          int                        `json:"version"`
	Bootstrapped     bool                       `json:"bootstrapped"`
	LastCheckpointAt time.Time                  `json:"last_checkpoint_at,omitempty"`
	Trackers         map[string]tracker         `json:"trackers"`
	Incidents        map[string]Incident        `json:"incidents"`
	Jobs             map[string]NotificationJob `json:"jobs"`
	Cursors          map[string]storage.Cursor  `json:"cursors,omitempty"`
}

type Persistence interface {
	Load(context.Context) (Snapshot, error)
	Save(context.Context, Snapshot) error
}

type Service struct {
	mu          sync.RWMutex
	cfg         Config
	persistence Persistence
	state       Snapshot
}

func New(cfg Config, persistence Persistence) (*Service, error) {
	if cfg.Interface == "" {
		cfg.Interface = "wan"
	}
	if cfg.DownAfter <= 0 {
		return nil, errors.New("down duration must be positive")
	}
	if cfg.RecoveredAfter <= 0 {
		return nil, errors.New("recovery duration must be positive")
	}
	state := Snapshot{
		Version:   1,
		Trackers:  make(map[string]tracker),
		Incidents: make(map[string]Incident),
		Jobs:      make(map[string]NotificationJob),
		Cursors:   make(map[string]storage.Cursor),
	}
	return &Service{cfg: cfg, persistence: persistence, state: state}, nil
}

func (s *Service) Load(ctx context.Context) error {
	if s.persistence == nil {
		return nil
	}
	state, err := s.persistence.Load(ctx)
	if err != nil {
		return err
	}
	if state.Version == 0 {
		return nil
	}
	if state.Version != 1 {
		return fmt.Errorf("unsupported incident state version %d", state.Version)
	}
	initializeSnapshot(&state)
	s.mu.Lock()
	s.state = state
	s.mu.Unlock()
	return nil
}

func (s *Service) Observe(ctx context.Context, message model.Message, notifyEligible bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observeLocked(message, notifyEligible, "")
	return s.saveLocked(ctx)
}

// Apply checkpoints evidence-derived state, outbox jobs, and source cursors in
// one durable snapshot. Replayed records are harmless, but a checkpoint should
// never claim source progress that its derived state does not contain.
func (s *Service) Apply(ctx context.Context, observations []Observation, cursors map[string]storage.Cursor, checkpointAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, observation := range observations {
		s.observeLocked(observation.Message, observation.NotifyEligible, observation.SourceRef)
	}
	s.state.Cursors = cloneCursors(cursors)
	s.state.Bootstrapped = true
	s.state.LastCheckpointAt = checkpointAt.UTC()
	return s.saveLocked(ctx)
}

func (s *Service) Rebuild(ctx context.Context, observations []Observation, cursors map[string]storage.Cursor, checkpointAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.state
	s.state = Snapshot{Version: 1, Bootstrapped: true, LastCheckpointAt: checkpointAt.UTC(), Trackers: map[string]tracker{}, Incidents: map[string]Incident{}, Jobs: map[string]NotificationJob{}, Cursors: cloneCursors(cursors)}
	for _, observation := range observations {
		s.observeLocked(observation.Message, observation.NotifyEligible, observation.SourceRef)
	}
	for key, incident := range s.state.Incidents {
		if old, exists := previous.Incidents[key]; exists {
			incident.Notify = incident.Notify || old.Notify
			s.state.Incidents[key] = incident
		}
	}
	for key, current := range s.state.Trackers {
		if incident, exists := s.state.Incidents[current.IncidentID]; exists {
			current.Notify = incident.Notify
			s.state.Trackers[key] = current
		}
	}
	for id, job := range previous.Jobs {
		if _, exists := s.state.Incidents[job.IncidentID]; exists {
			s.state.Jobs[id] = job
		}
	}
	for _, incident := range s.state.Incidents {
		if !incident.Notify || incident.ActivatedAt == nil {
			continue
		}
		s.enqueueLocked(incident.ID, NotificationOpened, *incident.ActivatedAt)
		if incident.State == StateResolved && incident.ResolvedAt != nil {
			s.enqueueLocked(incident.ID, NotificationResolved, *incident.ResolvedAt)
		}
	}
	return s.saveLocked(ctx)
}

func (s *Service) Bootstrapped() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.Bootstrapped
}

func (s *Service) LastCheckpointAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.LastCheckpointAt
}

func (s *Service) observeLocked(message model.Message, notifyEligible bool, sourceRef string) {
	evidence, ok := classify(message, s.cfg.Interface)
	if !ok {
		return
	}
	evidence.sourceRef = sourceRef
	key := evidence.hostname + "\x00" + evidence.iface
	current := s.state.Trackers[key]
	if !current.LastEvidenceAt.IsZero() && evidence.at.Before(current.LastEvidenceAt) {
		return
	}
	s.advanceLocked(evidence.at)
	current = s.state.Trackers[key]
	current.LastEvidenceAt = evidence.at

	switch evidence.online {
	case false:
		switch current.Phase {
		case "", phaseHealthy:
			id := incidentID(evidence.hostname, evidence.iface, evidence.at, evidence.messageID)
			current = tracker{
				Phase:          phasePendingFailure,
				PendingSince:   evidence.at,
				LastEvidenceAt: evidence.at,
				StartMessageID: evidence.messageID,
				StartSourceRef: evidence.sourceRef,
				IncidentID:     id,
				Notify:         notifyEligible,
			}
			s.state.Incidents[id] = Incident{
				ID: id, RuleVersion: 1, Hostname: evidence.hostname, Interface: evidence.iface,
				State: StatePendingFailure, StartedAt: evidence.at, LastEvidenceAt: evidence.at,
				EvidenceIDs: appendEvidence(nil, evidence.messageID), EvidenceRefs: appendEvidence(nil, evidence.sourceRef),
				DownAfter: s.cfg.DownAfter, RecoveredAfter: s.cfg.RecoveredAfter, Notify: notifyEligible,
			}
		case phasePendingFailure:
			current.Notify = current.Notify || notifyEligible
			if incident, exists := s.state.Incidents[current.IncidentID]; exists {
				incident.Notify = current.Notify
				incident.LastEvidenceAt = evidence.at
				incident.EvidenceIDs = appendEvidence(incident.EvidenceIDs, evidence.messageID)
				incident.EvidenceRefs = appendEvidence(incident.EvidenceRefs, evidence.sourceRef)
				s.state.Incidents[incident.ID] = incident
			}
		case phasePendingRecovery:
			current.Phase = phaseActive
			current.RecoverySince = time.Time{}
			if incident, exists := s.state.Incidents[current.IncidentID]; exists {
				incident.State = StateActive
				incident.RecoveryFirstAt = nil
				incident.LastEvidenceAt = evidence.at
				incident.EvidenceIDs = appendEvidence(incident.EvidenceIDs, evidence.messageID)
				incident.EvidenceRefs = appendEvidence(incident.EvidenceRefs, evidence.sourceRef)
				s.state.Incidents[incident.ID] = incident
			}
		case phaseActive:
			if incident, exists := s.state.Incidents[current.IncidentID]; exists {
				incident.LastEvidenceAt = evidence.at
				incident.EvidenceIDs = appendEvidence(incident.EvidenceIDs, evidence.messageID)
				incident.EvidenceRefs = appendEvidence(incident.EvidenceRefs, evidence.sourceRef)
				s.state.Incidents[incident.ID] = incident
			}
		}
	case true:
		switch current.Phase {
		case "":
			current = tracker{Phase: phaseHealthy, LastEvidenceAt: evidence.at}
		case phasePendingFailure:
			delete(s.state.Incidents, current.IncidentID)
			current = tracker{Phase: phaseHealthy, LastEvidenceAt: evidence.at}
		case phaseActive:
			current.Phase = phasePendingRecovery
			current.RecoverySince = evidence.at
			if incident, exists := s.state.Incidents[current.IncidentID]; exists {
				incident.State = StatePendingRecovery
				recovery := evidence.at
				incident.RecoveryFirstAt = &recovery
				incident.LastEvidenceAt = evidence.at
				incident.EvidenceIDs = appendEvidence(incident.EvidenceIDs, evidence.messageID)
				incident.EvidenceRefs = appendEvidence(incident.EvidenceRefs, evidence.sourceRef)
				s.state.Incidents[incident.ID] = incident
			}
		case phasePendingRecovery:
			if incident, exists := s.state.Incidents[current.IncidentID]; exists {
				incident.LastEvidenceAt = evidence.at
				incident.EvidenceIDs = appendEvidence(incident.EvidenceIDs, evidence.messageID)
				incident.EvidenceRefs = appendEvidence(incident.EvidenceRefs, evidence.sourceRef)
				s.state.Incidents[incident.ID] = incident
			}
		}
	}
	s.state.Trackers[key] = current
}

func (s *Service) Tick(ctx context.Context, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := s.advanceLocked(now)
	if !changed {
		return nil
	}
	return s.saveLocked(ctx)
}

func (s *Service) advanceLocked(now time.Time) bool {
	changed := false
	for key, current := range s.state.Trackers {
		switch current.Phase {
		case phasePendingFailure:
			if now.Sub(current.PendingSince) < s.cfg.DownAfter {
				continue
			}
			activated := current.PendingSince.Add(s.cfg.DownAfter)
			incident, exists := s.state.Incidents[current.IncidentID]
			if !exists {
				continue
			}
			incident.State = StateActive
			incident.ActivatedAt = &activated
			incident.Notify = current.Notify
			s.state.Incidents[incident.ID] = incident
			current.Phase = phaseActive
			s.state.Trackers[key] = current
			if incident.Notify {
				s.enqueueLocked(incident.ID, NotificationOpened, activated)
			}
			changed = true
		case phasePendingRecovery:
			if now.Sub(current.RecoverySince) < s.cfg.RecoveredAfter {
				continue
			}
			resolved := current.RecoverySince.Add(s.cfg.RecoveredAfter)
			incident, exists := s.state.Incidents[current.IncidentID]
			if !exists {
				continue
			}
			incident.State = StateResolved
			incident.ResolvedAt = &resolved
			s.state.Incidents[incident.ID] = incident
			if incident.Notify {
				s.enqueueLocked(incident.ID, NotificationResolved, resolved)
			}
			s.state.Trackers[key] = tracker{Phase: phaseHealthy, LastEvidenceAt: current.LastEvidenceAt}
			changed = true
		}
	}
	return changed
}

func (s *Service) enqueueLocked(incidentID string, kind NotificationKind, at time.Time) {
	id := incidentID + ":" + string(kind)
	if _, exists := s.state.Jobs[id]; exists {
		return
	}
	s.state.Jobs[id] = NotificationJob{
		ID:         id,
		IncidentID: incidentID,
		Kind:       kind,
		CreatedAt:  at,
		NextAt:     at,
	}
}

func (s *Service) List(query Query) []Incident {
	s.mu.RLock()
	items := make([]Incident, 0, len(s.state.Incidents))
	for _, incident := range s.state.Incidents {
		if query.State != "" && incident.State != query.State {
			continue
		}
		items = append(items, cloneIncident(incident))
	}
	s.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool { return items[i].StartedAt.After(items[j].StartedAt) })
	start := query.Offset
	if start < 0 {
		start = 0
	}
	if start >= len(items) {
		return []Incident{}
	}
	end := len(items)
	if query.Limit > 0 && start+query.Limit < end {
		end = start + query.Limit
	}
	return items[start:end]
}

func (s *Service) Get(id string) (Incident, bool) {
	s.mu.RLock()
	incident, ok := s.state.Incidents[id]
	s.mu.RUnlock()
	return cloneIncident(incident), ok
}

func (s *Service) PendingNotifications() []NotificationJob {
	s.mu.RLock()
	items := make([]NotificationJob, 0, len(s.state.Jobs))
	for _, job := range s.state.Jobs {
		if job.SentAt == nil && !job.Permanent {
			items = append(items, job)
		}
	}
	s.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	return items
}

func (s *Service) DueNotifications(now time.Time) []NotificationJob {
	s.mu.RLock()
	items := make([]NotificationJob, 0, len(s.state.Jobs))
	for _, job := range s.state.Jobs {
		if job.SentAt == nil && !job.Permanent && !job.NextAt.After(now) {
			items = append(items, job)
		}
	}
	s.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	return items
}

func (s *Service) MarkNotificationSent(ctx context.Context, id, adapter, externalID string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, exists := s.state.Jobs[id]
	if !exists {
		return fmt.Errorf("notification job %q not found", id)
	}
	sent := at.UTC()
	job.SentAt = &sent
	job.Adapter = adapter
	job.ExternalID = externalID
	job.LastError = ""
	s.state.Jobs[id] = job
	return s.saveLocked(ctx)
}

func (s *Service) MarkNotificationFailed(ctx context.Context, id, message string, next time.Time, permanent bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, exists := s.state.Jobs[id]
	if !exists {
		return fmt.Errorf("notification job %q not found", id)
	}
	job.Attempts++
	job.LastError = message
	job.NextAt = next.UTC()
	job.Permanent = permanent
	s.state.Jobs[id] = job
	return s.saveLocked(ctx)
}

func (s *Service) Cursors() map[string]storage.Cursor {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneCursors(s.state.Cursors)
}

func (s *Service) SetCursors(ctx context.Context, cursors map[string]storage.Cursor) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Cursors = cloneCursors(cursors)
	return s.saveLocked(ctx)
}

func (s *Service) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSnapshot(s.state)
}

func (s *Service) ReplaceSnapshot(ctx context.Context, state Snapshot) error {
	initializeSnapshot(&state)
	s.mu.Lock()
	s.state = state
	err := s.saveLocked(ctx)
	s.mu.Unlock()
	return err
}

func (s *Service) saveLocked(ctx context.Context) error {
	if s.persistence == nil {
		return nil
	}
	return s.persistence.Save(ctx, cloneSnapshot(s.state))
}

type evidence struct {
	hostname  string
	iface     string
	online    bool
	at        time.Time
	messageID string
	sourceRef string
}

func classify(message model.Message, iface string) (evidence, bool) {
	hostname := strings.TrimSpace(message.Hostname)
	if hostname == "" || message.Tag != "gl-repeater" || message.ReceivedAt.IsZero() {
		return evidence{}, false
	}
	prefix := "interface " + iface + " status "
	status := strings.TrimSpace(message.Message)
	index := strings.Index(status, prefix)
	if index < 0 || !validRepeaterPrefix(status[:index]) {
		return evidence{}, false
	}
	state := strings.TrimSpace(status[index+len(prefix):])
	if state != "offline" && state != "online" {
		return evidence{}, false
	}
	return evidence{
		hostname:  hostname,
		iface:     iface,
		online:    state == "online",
		at:        message.ReceivedAt.UTC(),
		messageID: message.ID,
	}, true
}

func validRepeaterPrefix(value string) bool {
	const prefix = "(repeater.lua:"
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, ") ") {
		return false
	}
	line := strings.TrimSuffix(strings.TrimPrefix(value, prefix), ") ")
	if line == "" {
		return false
	}
	for _, character := range line {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func incidentID(hostname, iface string, started time.Time, messageID string) string {
	sum := sha256.Sum256([]byte(hostname + "\x00" + iface + "\x00" + started.UTC().Format(time.RFC3339Nano) + "\x00" + messageID))
	return hex.EncodeToString(sum[:8])
}

func appendEvidence(ids []string, id string) []string {
	if id == "" {
		return ids
	}
	if len(ids) > 0 && ids[len(ids)-1] == id {
		return ids
	}
	return append(ids, id)
}

func cloneIncident(incident Incident) Incident {
	incident.EvidenceIDs = append([]string(nil), incident.EvidenceIDs...)
	incident.EvidenceRefs = append([]string(nil), incident.EvidenceRefs...)
	if incident.ActivatedAt != nil {
		value := *incident.ActivatedAt
		incident.ActivatedAt = &value
	}
	if incident.RecoveryFirstAt != nil {
		value := *incident.RecoveryFirstAt
		incident.RecoveryFirstAt = &value
	}
	if incident.ResolvedAt != nil {
		value := *incident.ResolvedAt
		incident.ResolvedAt = &value
	}
	return incident
}

func cloneSnapshot(state Snapshot) Snapshot {
	clone := Snapshot{
		Version:          state.Version,
		Bootstrapped:     state.Bootstrapped,
		LastCheckpointAt: state.LastCheckpointAt,
		Trackers:         make(map[string]tracker, len(state.Trackers)),
		Incidents:        make(map[string]Incident, len(state.Incidents)),
		Jobs:             make(map[string]NotificationJob, len(state.Jobs)),
		Cursors:          make(map[string]storage.Cursor, len(state.Cursors)),
	}
	for key, value := range state.Trackers {
		clone.Trackers[key] = value
	}
	for key, value := range state.Incidents {
		clone.Incidents[key] = cloneIncident(value)
	}
	for key, value := range state.Jobs {
		clone.Jobs[key] = value
	}
	for key, value := range state.Cursors {
		clone.Cursors[key] = value
	}
	return clone
}

func initializeSnapshot(state *Snapshot) {
	if state.Version == 0 {
		state.Version = 1
	}
	if state.Trackers == nil {
		state.Trackers = make(map[string]tracker)
	}
	if state.Incidents == nil {
		state.Incidents = make(map[string]Incident)
	}
	if state.Jobs == nil {
		state.Jobs = make(map[string]NotificationJob)
	}
	if state.Cursors == nil {
		state.Cursors = make(map[string]storage.Cursor)
	}
}

func cloneCursors(cursors map[string]storage.Cursor) map[string]storage.Cursor {
	clone := make(map[string]storage.Cursor, len(cursors))
	for path, cursor := range cursors {
		clone[path] = cursor
	}
	return clone
}
