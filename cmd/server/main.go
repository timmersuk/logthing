package main

import (
	"context"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/timmersuk/logthing/internal/api"
	"github.com/timmersuk/logthing/internal/config"
	"github.com/timmersuk/logthing/internal/incidents"
	"github.com/timmersuk/logthing/internal/model"
	"github.com/timmersuk/logthing/internal/notification"
	"github.com/timmersuk/logthing/internal/openapi"
	"github.com/timmersuk/logthing/internal/realtime"
	"github.com/timmersuk/logthing/internal/storage"
	"github.com/timmersuk/logthing/internal/swaggerui"
	sysloglistener "github.com/timmersuk/logthing/internal/syslog"
	"github.com/timmersuk/logthing/internal/web"
)

var BuildID string

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := storage.NewFileStore(cfg.DataDir)
	if err != nil {
		return err
	}
	incidentEngine, err := incidents.New(incidents.Config{
		Interface:      cfg.WANInterface,
		DownAfter:      cfg.WANDownAfter,
		RecoveredAfter: cfg.WANRecoveredAfter,
	}, incidents.NewFilePersistence(filepath.Join(cfg.StateDir, "incidents.json")))
	if err != nil {
		return err
	}
	if err := incidentEngine.Load(ctx); err != nil {
		return err
	}
	notifier, err := newNotifier(cfg)
	if err != nil {
		return err
	}
	incidentWorker := incidents.NewWorker(incidentEngine, store, notifier, time.Now().UTC(), cfg.PublicURL)
	go incidentWorker.Run(ctx)

	events := realtime.NewHub()
	publishingStore := realtime.NewPublishingStore(store, multiPublisher{events, incidentWorker})

	frontend, err := fs.Sub(web.Files, "dist")
	if err != nil {
		return err
	}
	swagger, err := fs.Sub(swaggerui.Files, "dist")
	if err != nil {
		return err
	}

	router, err := api.NewRouter(api.Config{
		Store:       publishingStore,
		Events:      events,
		Frontend:    frontend,
		SwaggerUI:   swagger,
		OpenAPISpec: openapi.Spec,
		TestEvent:   newTestEventSender(cfg),
		Credentials: api.Credentials{
			Username: cfg.Username,
			Password: cfg.Password,
		},
		BuildID:        BuildID,
		Incidents:      incidentEngine,
		IncidentWorker: incidentWorker,
	})
	if err != nil {
		return err
	}

	listener, err := sysloglistener.NewListener(sysloglistener.Config{
		UDPAddr: cfg.SyslogUDPAddr,
		TCPAddr: cfg.SyslogTCPAddr,
		Format:  cfg.SyslogFormat,
	}, publishingStore)
	if err != nil {
		return err
	}
	if err := listener.Start(); err != nil {
		return err
	}
	defer listener.Shutdown()

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("http listening on %s", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
		close(serverErr)
	}()

	log.Printf("syslog listening udp=%q tcp=%q format=%s", cfg.SyslogUDPAddr, cfg.SyslogTCPAddr, cfg.SyslogFormat)

	select {
	case <-ctx.Done():
	case err := <-serverErr:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}

	if err := <-serverErr; err != nil {
		return err
	}
	return nil
}

type multiPublisher []interface{ Publish(model.Message) }

func (publishers multiPublisher) Publish(message model.Message) {
	for _, publisher := range publishers {
		if publisher != nil {
			publisher.Publish(message)
		}
	}
}

func newNotifier(cfg config.Config) (notification.Notifier, error) {
	switch cfg.Notifier {
	case config.NotifierDiscord:
		return notification.NewDiscord(cfg.DiscordWebhookURL, 5*time.Second)
	default:
		return notification.DiscardNotifier{}, nil
	}
}

func newTestEventSender(cfg config.Config) api.TestEventSender {
	sender, err := sysloglistener.NewSender(cfg.TestEventNetwork, cfg.TestEventTarget, 5*time.Second)
	if err != nil {
		return func(context.Context, string) (api.TestEventResult, error) {
			return api.TestEventResult{}, err
		}
	}

	return func(ctx context.Context, message string) (api.TestEventResult, error) {
		result, err := sender.Send(ctx, sysloglistener.NewTestEvent(message))
		if err != nil {
			return api.TestEventResult{}, err
		}
		return api.TestEventResult{
			Network: result.Network,
			Address: result.Address,
			Payload: result.Payload,
		}, nil
	}
}
