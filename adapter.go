// Package humanirvana connects a standard Huma API to an existing Nirvana builder.
package humanirvana

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/caicloud/nirvana/service"
	"github.com/danielgtaylor/huma/v2"
)

// Adapter registers Huma operations on an application-owned Nirvana builder.
// Build the builder after all Huma registrations, then Bind its service before
// serving requests through the API's Adapter.
type Adapter struct {
	builder service.Builder
	raw     service.RawHandlerRegistrar
	mu      sync.RWMutex
	handler http.Handler
}

var _ huma.Adapter = (*Adapter)(nil)

// NewAdapter attaches Huma registrations to the existing Nirvana builder.
func NewAdapter(builder service.Builder) *Adapter {
	if builder == nil {
		panic("runtime: nil Nirvana builder")
	}
	raw, ok := builder.(service.RawHandlerRegistrar)
	if !ok {
		panic("runtime: Nirvana builder does not support raw HTTP handlers; upgrade the REST builder")
	}
	return &Adapter{builder: builder, raw: raw}
}

// Bind makes the application-built Nirvana service handle Huma Adapter requests.
// It must be called once, after builder.Build succeeds and before serving.
func (a *Adapter) Bind(handler http.Handler) error {
	if handler == nil {
		return fmt.Errorf("runtime: cannot bind a nil Nirvana service")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.handler != nil {
		return fmt.Errorf("runtime: Nirvana service is already bound")
	}
	a.handler = handler
	return nil
}

// Handle adds a Huma operation to the same Nirvana builder used for existing
// descriptors. Nirvana owns matching and middleware execution; Huma's callback
// already contains its API and operation middleware.
func (a *Adapter) Handle(op *huma.Operation, handler func(huma.Context)) {
	if op == nil || handler == nil {
		panic("runtime: cannot register a nil Huma operation or handler")
	}
	path := op.Path
	if path == "" {
		path = "/"
	}
	if err := a.raw.AddRawHandler(op.Method, path, func(ctx context.Context) error {
		httpContext := service.HTTPContextFrom(ctx)
		if httpContext == nil {
			return fmt.Errorf("runtime: Nirvana request context is unavailable")
		}
		handler(newContext(op, httpContext.RoutePath(), ctx, httpContext.Request(), httpContext.ResponseWriter(), httpContext.ValueContainer()))
		return nil
	}); err != nil {
		panic(fmt.Errorf("runtime: register %s %s: %w", op.Method, path, err))
	}
}

// ServeHTTP delegates to the service built by the application. It never builds
// the router implicitly.
func (a *Adapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	handler := a.handler
	a.mu.RUnlock()
	if handler == nil {
		http.Error(w, "Nirvana service is not bound", http.StatusServiceUnavailable)
		return
	}
	handler.ServeHTTP(w, r)
}
