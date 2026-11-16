// Package bootstrap wires configuration, storage and HTTP without domain logic.
package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/alibaba/open-guardrail/internal/adapters/httpapi"
	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	"github.com/alibaba/open-guardrail/internal/application/audit"
	"github.com/alibaba/open-guardrail/internal/application/capability"
	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/internal/config"
)

// Never infer local setup from Host or forwarding headers. An explicit public
// deployment opts out even when its reverse proxy connects over loopback.
func localBootstrap(c config.Config, addr net.Addr) bool {
	tcp, ok := addr.(*net.TCPAddr)
	return c.PublicURL == "" && ok && tcp.IP.IsLoopback()
}

func Serve(ctx context.Context, c config.Config, logger *slog.Logger) error {
	startup, cancel := context.WithTimeout(ctx, config.StartupTimeout)
	store, err := storage.Open(startup, c)
	defer cancel()
	if err != nil {
		return err
	}
	defer store.Close()
	identityService, err := prepareIdentity(startup, c, store)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return errors.New("cannot listen on configured address; check host and port availability")
	}
	defer listener.Close()
	origin := c.PublicURL
	if origin == "" {
		origin = "http://" + listener.Addr().String()
	}
	server := &http.Server{
		Handler: httpapi.New(store, httpapi.Options{
			Identity:   identityService,
			Audits:     audit.New(store.Audits()),
			Logger:     logger,
			ModelSpecs: spec.New(store.Specs()), ModelRuns: runapp.New(store.Runs()),
			Origin: origin, LocalBootstrap: localBootstrap(c, listener.Addr()),
		}), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	logger.Info("server ready", "url", "http://"+listener.Addr().String(), "database", c.Database.Driver)
	if initialized, _ := identityService.Status(ctx); !initialized {
		if localBootstrap(c, listener.Addr()) {
			logger.Info("create your administrator in the browser", "url", origin)
		} else {
			logger.Info("administrator setup required; read the private token file locally", "token_file", bootstrapPath(c))
		}
	}
	if store.StorageCapability().SupportLevel == capability.SupportOfficialLocal {
		logger.Info("local storage", "data_dir", c.DataDir)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return errors.New("HTTP server stopped unexpectedly")
	case <-ctx.Done():
		logger.Info("server shutting down")
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return errors.New("HTTP shutdown timed out")
		}
		return nil
	}
}
