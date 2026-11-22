// Package bootstrap wires configuration, storage and HTTP without domain logic.
package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/alibaba/open-guardrail/extension/resource/httpclient"
	"github.com/alibaba/open-guardrail/internal/adapters/httpapi"
	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	"github.com/alibaba/open-guardrail/internal/application/audit"
	"github.com/alibaba/open-guardrail/internal/application/capability"
	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/internal/config"
)

// WrapHTTP allows an enterprise binary to mount transport adapters around the core API.
var WrapHTTP func(http.Handler, httpapi.Options) http.Handler

func Serve(ctx context.Context, c config.Config, logger *slog.Logger) error {
	startup, cancel := context.WithTimeout(ctx, config.StartupTimeout)
	defer cancel()
	options := httpapi.Options{Logger: logger, TrustedOperatorNamespace: c.TrustedOperatorNamespace}
	store, err := storage.Open(startup, c)
	if err != nil {
		return err
	}
	defer store.Close()
	listener, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return errors.New("cannot listen on configured address; check host and port availability")
	}
	defer listener.Close()
	origin := c.PublicURL
	if origin == "" {
		origin = "http://" + listener.Addr().String()
	}
	options.Origin = origin
	if options.TrustedOperatorNamespace == "" {
		options.Audits = audit.New(store.Audits())
	}
	options.ModelSpecs, options.ModelRuns = spec.New(store.Specs()), runapp.New(store.Runs()).WithScenePlugins(scenePlugins(store.Specs()))
	handler := httpapi.New(store, options)
	if WrapHTTP != nil {
		handler = WrapHTTP(handler, options)
	}
	server := &http.Server{
		Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	logger.Info("server ready", "url", "http://"+listener.Addr().String(), "database", c.Database.Driver)
	if options.TrustedOperatorNamespace != "" {
		logger.Info("API-only access enabled; use the HTTP API")
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

func scenePlugins(specs *storage.Specs) func(context.Context, runapp.Scope, string) (runapp.ScenePlugins, error) {
	return func(ctx context.Context, scope runapp.Scope, key string) (runapp.ScenePlugins, error) {
		scene, err := specs.GetSceneModel(ctx, spec.Scope{WorkspaceID: scope.WorkspaceID, ProjectID: scope.ProjectID}, key)
		var result runapp.ScenePlugins
		if errors.Is(err, spec.ErrNotFound) {
			return result, runapp.ErrNotFound
		}
		if err != nil || scene.Plugins == nil {
			return result, err
		}
		if e := scene.Plugins.ChangeSet; e != nil {
			result.Enricher, err = httpclient.New(e.URL)
			if err != nil {
				return result, err
			}
		}
		if e := scene.Plugins.BindingCandidates; e != nil {
			result.Resolver, err = httpclient.New(e.URL)
		}
		return result, err
	}
}
