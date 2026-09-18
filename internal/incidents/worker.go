package incidents

import (
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/timmersuk/logthing/internal/model"
	"github.com/timmersuk/logthing/internal/notification"
	"github.com/timmersuk/logthing/internal/storage"
)

type RecordReader interface {
	VisitAfter(context.Context, map[string]int64, func(storage.StoredRecord) error) (map[string]int64, error)
}

type WorkerHealth struct {
	Running          bool       `json:"running"`
	Stale            bool       `json:"stale"`
	LastReconciledAt *time.Time `json:"last_reconciled_at,omitempty"`
	LastProcessedAt  *time.Time `json:"last_processed_at,omitempty"`
	LastError        string     `json:"last_error,omitempty"`
	PendingJobs      int        `json:"pending_jobs"`
}

type Worker struct {
	engine    *Service
	reader    RecordReader
	notifier  notification.Notifier
	startedAt time.Time
	publicURL string
	wake      chan struct{}
	mu        sync.RWMutex
	runMu     sync.Mutex
	health    WorkerHealth
}

func NewWorker(engine *Service, reader RecordReader, notifier notification.Notifier, startedAt time.Time, publicURL ...string) *Worker {
	if notifier == nil {
		notifier = notification.DiscardNotifier{}
	}
	worker := &Worker{
		engine: engine, reader: reader, notifier: notifier,
		startedAt: startedAt.UTC(), wake: make(chan struct{}, 1),
	}
	if len(publicURL) > 0 {
		worker.publicURL = publicURL[0]
	}
	return worker
}

func (w *Worker) Publish(model.Message) {
	w.Wake()
}

func (w *Worker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *Worker) Run(ctx context.Context) {
	w.setRunning(true)
	defer w.setRunning(false)
	if err := w.reconcile(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
		w.recordError(err)
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	reconcile := time.NewTicker(30 * time.Second)
	defer reconcile.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			if err := w.tick(ctx, now.UTC()); err != nil {
				w.recordError(err)
			}
		case now := <-reconcile.C:
			if err := w.reconcile(ctx, now.UTC()); err != nil {
				w.recordError(err)
			}
		case <-w.wake:
			if err := w.reconcile(ctx, time.Now().UTC()); err != nil {
				w.recordError(err)
			}
		}
	}
}

func (w *Worker) reconcile(ctx context.Context, now time.Time) error {
	w.runMu.Lock()
	defer w.runMu.Unlock()
	cursors := w.engine.Cursors()
	var lastProcessed *time.Time
	var records []storage.StoredRecord
	next, err := w.reader.VisitAfter(ctx, cursors, func(record storage.StoredRecord) error {
		if _, supported := classify(record.Message, w.engine.cfg.Interface); supported {
			records = append(records, record)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("reconcile incidents: %w", err)
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].Message.ReceivedAt.Equal(records[j].Message.ReceivedAt) {
			if records[i].File == records[j].File {
				return records[i].Offset < records[j].Offset
			}
			return records[i].File < records[j].File
		}
		return records[i].Message.ReceivedAt.Before(records[j].Message.ReceivedAt)
	})
	observations := make([]Observation, 0, len(records))
	for _, record := range records {
		message := record.Message
		observations = append(observations, Observation{
			Message: message, NotifyEligible: message.Transport != "import" && message.ReceivedAt.After(w.startedAt),
			SourceRef: fmt.Sprintf("%s:%d", record.File, record.Offset),
		})
		processed := message.ReceivedAt.UTC()
		if lastProcessed == nil || processed.After(*lastProcessed) {
			lastProcessed = &processed
		}
	}
	if err := w.engine.Apply(ctx, observations, next); err != nil {
		return fmt.Errorf("apply incident evidence: %w", err)
	}
	if err := w.engine.Tick(ctx, now); err != nil {
		return fmt.Errorf("advance incidents: %w", err)
	}
	if err := w.dispatch(ctx, now); err != nil {
		return err
	}
	w.mu.Lock()
	reconciled := now.UTC()
	w.health.LastReconciledAt = &reconciled
	if lastProcessed != nil {
		w.health.LastProcessedAt = lastProcessed
	}
	w.health.LastError = ""
	w.health.PendingJobs = len(w.engine.PendingNotifications())
	w.mu.Unlock()
	return nil
}

func (w *Worker) tick(ctx context.Context, now time.Time) error {
	w.runMu.Lock()
	defer w.runMu.Unlock()
	if err := w.engine.Tick(ctx, now); err != nil {
		return fmt.Errorf("advance incidents: %w", err)
	}
	return w.dispatch(ctx, now)
}

func (w *Worker) dispatch(ctx context.Context, now time.Time) error {
	for _, job := range w.engine.DueNotifications(now) {
		incident, exists := w.engine.Get(job.IncidentID)
		if !exists {
			if err := w.engine.MarkNotificationFailed(ctx, job.ID, "incident not found", now, true); err != nil {
				return err
			}
			continue
		}
		message := notificationFor(job, incident)
		if w.publicURL != "" {
			message.Link = fmt.Sprintf("%s/?incident=%s", w.publicURL, incident.ID)
		}
		receipt, err := w.notifier.Send(ctx, message)
		if err == nil {
			if err := w.engine.MarkNotificationSent(ctx, job.ID, receipt.Adapter, receipt.ExternalID, now); err != nil {
				return err
			}
			continue
		}
		permanent := false
		next := now.Add(retryDelay(job.ID, job.Attempts+1))
		var sendErr *notification.SendError
		if notification.AsSendError(err, &sendErr) {
			permanent = sendErr.Permanent
			if sendErr.RetryAfter > 0 {
				next = now.Add(sendErr.RetryAfter)
			}
		}
		if markErr := w.engine.MarkNotificationFailed(ctx, job.ID, err.Error(), next, permanent); markErr != nil {
			return markErr
		}
		if !permanent {
			log.Printf("incident notification %s failed; retry scheduled: %v", job.ID, err)
		}
	}
	return nil
}

func (w *Worker) SendTest(ctx context.Context) (notification.Receipt, error) {
	if _, disabled := w.notifier.(notification.DiscardNotifier); disabled {
		return notification.Receipt{}, notification.ErrNotConfigured
	}
	return w.notifier.Send(ctx, notification.Notification{
		EventID:    fmt.Sprintf("test-%d", time.Now().UTC().UnixNano()),
		Kind:       notification.KindTest,
		Severity:   "info",
		Title:      "Logthing test notification",
		Body:       "Discord notifications are configured correctly.",
		OccurredAt: time.Now().UTC(),
	})
}

func (w *Worker) Health() WorkerHealth {
	w.mu.RLock()
	health := w.health
	w.mu.RUnlock()
	health.PendingJobs = len(w.engine.PendingNotifications())
	health.Stale = health.Running && (health.LastReconciledAt == nil || time.Since(*health.LastReconciledAt) > time.Minute)
	return health
}

func (w *Worker) setRunning(running bool) {
	w.mu.Lock()
	w.health.Running = running
	w.mu.Unlock()
}

func (w *Worker) recordError(err error) {
	w.mu.Lock()
	w.health.LastError = err.Error()
	w.health.PendingJobs = len(w.engine.PendingNotifications())
	w.mu.Unlock()
	log.Printf("incident worker: %v", err)
}

func notificationFor(job NotificationJob, incident Incident) notification.Notification {
	message := notification.Notification{
		EventID:    job.ID,
		OccurredAt: job.CreatedAt,
		IncidentID: incident.ID,
	}
	switch job.Kind {
	case NotificationResolved:
		message.Kind = notification.KindIncidentResolved
		message.Severity = "info"
		message.Title = "🟢 Primary WAN recovered"
		duration := job.CreatedAt.Sub(incident.StartedAt).Round(time.Second)
		message.Body = fmt.Sprintf("%s %s was healthy continuously for %s. Incident duration: %s.", incident.Hostname, incident.Interface, incident.RecoveredAfter, duration)
	default:
		message.Kind = notification.KindIncidentOpened
		message.Severity = "critical"
		message.Title = "🔴 Primary WAN down"
		message.Body = fmt.Sprintf("%s %s has been offline for %s. Backup connectivity is unknown.", incident.Hostname, incident.Interface, incident.DownAfter)
	}
	return message
}

func retryDelay(jobID string, attempt int) time.Duration {
	minutes := math.Pow(2, float64(attempt-1))
	if minutes > 60 {
		minutes = 60
	}
	base := time.Duration(minutes * float64(time.Minute))
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(jobID))
	jitter := 0.9 + float64(hash.Sum32()%2001)/10000
	return time.Duration(float64(base) * jitter)
}
