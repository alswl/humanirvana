// Package humanirvana connects a standard Huma API to an existing Nirvana builder.
package humanirvana

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/caicloud/nirvana/definition"
	"github.com/caicloud/nirvana/service"
	"github.com/caicloud/nirvana/service/rest/router"
	"github.com/danielgtaylor/huma/v2"
)

// Adapter registers Huma operations on an application-owned Nirvana builder.
// Build the builder after all Huma registrations, then Bind its service before
// serving requests through the API's Adapter.
type Adapter struct {
	builder service.Builder
	mu      sync.RWMutex
	handler http.Handler
}

var _ huma.Adapter = (*Adapter)(nil)

// NewAdapter attaches Huma registrations to the existing Nirvana builder.
func NewAdapter(builder service.Builder) *Adapter {
	if builder == nil {
		panic("runtime: nil Nirvana builder")
	}
	return &Adapter{builder: builder}
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

// Handle registers a Huma callback as a context-aware Nirvana definition.
// Nirvana owns route matching, media admission, filters and node middleware.
// Huma owns the callback's binding, validation and response serialization.
func (a *Adapter) Handle(op *huma.Operation, handler func(huma.Context)) {
	if op == nil || handler == nil {
		panic("runtime: cannot register a nil Huma operation or handler")
	}
	path := op.Path
	if path == "" {
		path = "/"
	}
	method, ok := map[string]definition.Method{
		http.MethodGet: definition.Get, http.MethodHead: definition.Head,
		http.MethodPost: definition.Create, http.MethodPut: definition.Update,
		http.MethodPatch: definition.Patch, http.MethodDelete: definition.Delete,
	}[op.Method]
	if !ok {
		panic(fmt.Errorf("humanirvana: unsupported Nirvana HTTP method %q", op.Method))
	}
	if _, _, err := router.Parse(path); err != nil {
		panic(fmt.Errorf("humanirvana: invalid route %q: %w", path, err))
	}
	// Reuse the existing template's parameter names when methods share a route.
	for existingPath, definitions := range a.builder.Definitions() {
		if routeShape(existingPath) != routeShape(path) {
			continue
		}
		for _, d := range definitions {
			if d.Method == definition.Any || service.HTTPMethodFor(d.Method) == op.Method {
				panic(fmt.Errorf("humanirvana: duplicate route %s %s", op.Method, path))
			}
		}
		path = existingPath
	}
	d := definition.Definition{
		Method:        method,
		Consumes:      []string{definition.MIMEAll},
		Produces:      []string{definition.MIMEAll},
		ErrorProduces: []string{definition.MIMEAll},
		Parameters:    []definition.Parameter{{Name: "context", Source: definition.Prefab}},
		Function: func(ctx context.Context) {
			httpContext := service.HTTPContextFrom(ctx)
			handler(newContext(op, httpContext.RoutePath(), ctx, httpContext.Request(), httpContext.ResponseWriter(), httpContext.ValueContainer()))
		},
	}
	if err := a.builder.AddDescriptor(definition.Descriptor{Path: path, Definitions: []definition.Definition{d}}); err != nil {
		panic(fmt.Errorf("humanirvana: register %s %s: %w", op.Method, path, err))
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

func routeShape(path string) string {
	parts := strings.Split(strings.TrimRight(path, "/"), "/")
	for i, part := range parts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") && !strings.Contains(part, ":") {
			parts[i] = "{}"
		}
	}
	return strings.Join(parts, "/")
}
