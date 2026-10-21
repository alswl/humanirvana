// Package middleware provides middleware bridging from Nirvana to Huma.
package middleware

import (
	"context"
	"time"

	"github.com/caicloud/nirvana/definition"
	"github.com/danielgtaylor/huma/v2"
)

// NirvanaMiddleware represents a Nirvana middleware function.
type NirvanaMiddleware func(ctx context.Context, chain definition.Chain) error

// NirvanaToHumaConfig holds configuration for middleware conversion.
type NirvanaToHumaConfig struct {
	// Priority determines the execution order (higher = executed first)
	Priority int

	// Name is the middleware identifier
	Name string

	// SkipWithoutContext skips middleware if nirvana context is not available
	SkipWithoutContext bool
}

// MiddlewareOption is a function that configures middleware conversion.
type MiddlewareOption func(*NirvanaToHumaConfig)

// WithPriority sets the priority for middleware execution.
func WithPriority(priority int) MiddlewareOption {
	return func(c *NirvanaToHumaConfig) {
		c.Priority = priority
	}
}

// WithName sets the name for middleware identification.
func WithName(name string) MiddlewareOption {
	return func(c *NirvanaToHumaConfig) {
		c.Name = name
	}
}

// WithSkipWithoutContext enables skipping middleware when nirvana context is unavailable.
func WithSkipWithoutContext() MiddlewareOption {
	return func(c *NirvanaToHumaConfig) {
		c.SkipWithoutContext = true
	}
}

// nirvanaChainAdapter adapts Nirvana's Chain interface to Huma's next function.
type nirvanaChainAdapter struct {
	ctx  context.Context
	next func(context.Context)
}

// Continue implements the Nirvana Chain interface.
func (c *nirvanaChainAdapter) Continue(ctx context.Context) error {
	c.next(ctx)
	return nil
}

// HumaMiddlewareFunc is the Huma v2 middleware signature: func(ctx, next).
// Huma does not export a Middleware type; UseMiddleware accepts this function form.
type HumaMiddlewareFunc func(ctx huma.Context, next func(huma.Context))

// HumaMiddlewareWrapper wraps a Nirvana middleware for use with Huma.
type HumaMiddlewareWrapper struct {
	// Middleware is the original Nirvana middleware
	Middleware NirvanaMiddleware

	// Config holds conversion configuration
	Config NirvanaToHumaConfig
}

// ConvertToHuma converts a Nirvana middleware to a Huma middleware (function form).
func ConvertToHuma(mw NirvanaMiddleware, opts ...MiddlewareOption) HumaMiddlewareFunc {
	config := NirvanaToHumaConfig{
		Priority:           0,
		SkipWithoutContext: false,
	}
	for _, opt := range opts {
		opt(&config)
	}

	wrapper := &HumaMiddlewareWrapper{
		Middleware: mw,
		Config:     config,
	}

	return func(ctx huma.Context, next func(huma.Context)) {
		wrapper.Wrap(next)(ctx)
	}
}

// Wrap implements the Huma middleware interface.
func (w *HumaMiddlewareWrapper) Wrap(next func(huma.Context)) func(huma.Context) {
	return func(ctx huma.Context) {
		// Wrap the Huma context with nirvana context
		nirvanaCtx := wrapToNirvanaContext(ctx)

		// Create chain adapter
		chain := &nirvanaChainAdapter{
			ctx: ctx.Context(),
			next: func(c context.Context) {
				next(ctx)
			},
		}

		// Call the Nirvana middleware
		if err := w.Middleware(nirvanaCtx, chain); err != nil {
			// Stop chain on error; proper error response would need api (huma.WriteErr(api, ctx, ...))
			return
		}
		next(ctx)
	}
}

// wrapToNirvanaContext wraps a Huma context to provide Nirvana-compatible access.
func wrapToNirvanaContext(ctx huma.Context) context.Context {
	// Create a context that can be used by Nirvana middleware
	// The actual implementation would need to extract nirvana-specific data
	return &nirvanaContextAdapter{ctx: ctx}
}

// nirvanaContextAdapter adapts Huma context to context.Context with Nirvana helpers.
type nirvanaContextAdapter struct {
	ctx huma.Context
}

func (c *nirvanaContextAdapter) Value(key interface{}) interface{} {
	if key == "huma.context" {
		return c.ctx
	}
	return nil
}

func (c *nirvanaContextAdapter) Deadline() (deadline time.Time, ok bool) {
	return time.Time{}, false
}

func (c *nirvanaContextAdapter) Done() <-chan struct{} {
	return nil
}

func (c *nirvanaContextAdapter) Err() error {
	return nil
}

// MiddlewareRegistry manages middleware registration and ordering.
type MiddlewareRegistry struct {
	// middlewares holds registered middlewares
	middlewares []*HumaMiddlewareWrapper

	// priorityMap maps middleware names to priorities
	priorityMap map[string]int
}

// NewMiddlewareRegistry creates a new middleware registry.
func NewMiddlewareRegistry() *MiddlewareRegistry {
	return &MiddlewareRegistry{
		middlewares: make([]*HumaMiddlewareWrapper, 0),
		priorityMap: make(map[string]int),
	}
}

// Register registers a Nirvana middleware.
func (r *MiddlewareRegistry) Register(mw NirvanaMiddleware, opts ...MiddlewareOption) {
	config := NirvanaToHumaConfig{}
	for _, opt := range opts {
		opt(&config)
	}

	wrapper := &HumaMiddlewareWrapper{
		Middleware: mw,
		Config:     config,
	}

	r.middlewares = append(r.middlewares, wrapper)
	if config.Name != "" {
		r.priorityMap[config.Name] = config.Priority
	}

	// Sort by priority (higher first)
	r.sortByPriority()
}

// sortByPriority sorts middlewares by priority.
func (r *MiddlewareRegistry) sortByPriority() {
	// Simple insertion sort by priority
	for i := 1; i < len(r.middlewares); i++ {
		current := r.middlewares[i]
		j := i - 1
		for j >= 0 && r.middlewares[j].Config.Priority < current.Config.Priority {
			r.middlewares[j+1] = r.middlewares[j]
			j--
		}
		r.middlewares[j+1] = current
	}
}

// Build builds the middleware chain in the correct order.
func (r *MiddlewareRegistry) Build(finalHandler func(huma.Context)) func(huma.Context) {
	handler := finalHandler

	// Apply middlewares in reverse order (so first registered is outermost)
	for i := len(r.middlewares) - 1; i >= 0; i-- {
		mw := r.middlewares[i]
		nextHandler := handler
		handler = mw.Wrap(nextHandler)
	}

	return handler
}

// Middlewares returns all registered middlewares.
func (r *MiddlewareRegistry) Middlewares() []*HumaMiddlewareWrapper {
	return r.middlewares
}

// Use applies all registered middlewares to the Huma API.
func (r *MiddlewareRegistry) Use(api huma.API) {
	for _, w := range r.middlewares {
		api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
			w.Wrap(next)(ctx)
		})
	}
}
