package main

import (
	"context"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/timmersuk/logthing/internal/api"
	"github.com/timmersuk/logthing/internal/config"
	"github.com/timmersuk/logthing/internal/openapi"
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

	store, err := storage.NewFileStore(cfg.DataDir)
	if err != nil {
		return err
	}

	frontend, err := fs.Sub(web.Files, "dist")
	if err != nil {
		return err
	}
	swagger, err := fs.Sub(swaggerui.Files, "dist")
	if err != nil {
		return err
	}

	router, err := api.NewRouter(api.Config{
		Store:       store,
		Frontend:    frontend,
		SwaggerUI:   swagger,
		OpenAPISpec: openapi.Spec,
		TestEvent:   newTestEventSender(cfg),
		Credentials: api.Credentials{
			Username: cfg.Username,
			Password: cfg.Password,
		},
		BuildID: BuildID,
	})
	if err != nil {
		return err
	}

	listener, err := sysloglistener.NewListener(sysloglistener.Config{
		UDPAddr: cfg.SyslogUDPAddr,
		TCPAddr: cfg.SyslogTCPAddr,
		Format:  cfg.SyslogFormat,
	}, store)
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
