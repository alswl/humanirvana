package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alswl/humanirvana"
	"github.com/caicloud/nirvana"
	"github.com/caicloud/nirvana/plugins/healthcheck"
	"github.com/caicloud/nirvana/service"
	"github.com/caicloud/nirvana/service/rest"
	"github.com/danielgtaylor/huma/v2"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// Keep os.Exit outside run so deferred plugin cleanup cannot be skipped.
	err := run(ctx)
	stop()
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context) (err error) {
	builder := rest.NewBuilder()
	// Nirvana healthcheck expects an injected context parameter.
	builder.SetModifier(service.FirstContextParameter())
	config := nirvana.NewConfig().Configure(healthcheck.Path("/healthz"))
	cleanup, err := humanirvana.InstallPlugins(builder, config, []string{healthcheck.ExternalConfigName})
	if err != nil {
		// InstallPlugins already includes rollback errors.
		return err
	}
	// Cleanup errors must not hide an initialization failure.
	defer func() { err = errors.Join(err, cleanup()) }()

	adapter := humanirvana.NewAdapter(builder)
	api := huma.NewAPI(huma.DefaultConfig("Plugin example", "1.0.0"), adapter)
	huma.Register(api, huma.Operation{OperationID: "hello", Method: http.MethodGet, Path: "/hello"},
		func(context.Context, *struct{}) (*struct{ Body string }, error) {
			return &struct{ Body string }{Body: "hello"}, nil
		})
	handler, err := builder.Build()
	if err != nil {
		return err
	}
	if err := adapter.Bind(handler); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:18080")
	if err != nil {
		return err
	}
	server := &http.Server{Handler: adapter, ReadHeaderTimeout: 5 * time.Second}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	var serveErr error
	finished := false
	select {
	case <-ctx.Done():
	case serveErr = <-served:
		finished = true
	}
	// Drain requests before releasing resources used by their handlers.
	// Applications choosing a shutdown deadline must handle still-running requests.
	shutdownErr := server.Shutdown(context.Background())
	if !finished {
		serveErr = <-served
	}
	// Serve reports ErrServerClosed even after a successful graceful shutdown.
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(serveErr, shutdownErr)
}
