package middleware

import (
	"context"

	"github.com/caicloud/nirvana/definition"
	"github.com/danielgtaylor/huma/v2"
)

// NirvanaMiddlewareFunc is the signature for Nirvana middleware.
type NirvanaMiddlewareFunc func(ctx context.Context, chain definition.Chain) error

// ConvertMiddleware converts a Nirvana middleware to a Huma middleware (function form).
func ConvertMiddleware(mw NirvanaMiddlewareFunc, opts ...MiddlewareOption) HumaMiddlewareFunc {
	config := NirvanaToHumaConfig{
		Priority:           0,
		SkipWithoutContext: false,
	}
	for _, opt := range opts {
		opt(&config)
	}

	wrapper := &nirvanaMiddlewareWrapper{
		middleware: mw,
		config:     config,
	}

	return func(ctx huma.Context, next func(huma.Context)) {
		wrapper.wrapNext(next)(ctx)
	}
}

// nirvanaMiddlewareWrapper wraps a Nirvana middleware function.
type nirvanaMiddlewareWrapper struct {
	middleware NirvanaMiddlewareFunc
	config     NirvanaToHumaConfig
}

// wrapNext returns the handler that runs this middleware then next.
func (w *nirvanaMiddlewareWrapper) wrapNext(next func(huma.Context)) func(huma.Context) {
	return func(ctx huma.Context) {
		if w.config.SkipWithoutContext {
			next(ctx)
			return
		}

		chain := &nirvanaChain{
			ctx:     ctx.Context(),
			next:    next,
			humaCtx: ctx,
		}

		err := w.middleware(chain.Context(), chain)
		if err != nil {
			return // stop chain; proper error response would need api
		}
		next(ctx)
	}
}

// nirvanaChain implements Nirvana's Chain interface.
type nirvanaChain struct {
	ctx     context.Context
	next    func(huma.Context)
	humaCtx huma.Context
}

// Context returns the underlying context.
func (c *nirvanaChain) Context() context.Context {
	return c.ctx
}

// Continue continues to the next handler in the chain.
func (c *nirvanaChain) Continue(ctx context.Context) error {
	// Call the next handler
	c.next(c.humaCtx)
	return nil
}

// WithContext returns a new chain with the given context.
func (c *nirvanaChain) WithContext(ctx context.Context) definition.Chain {
	return &nirvanaChain{
		ctx:     ctx,
		next:    c.next,
		humaCtx: c.humaCtx,
	}
}

// CommonMiddlewareConverters provides common middleware conversion patterns.
type CommonMiddlewareConverters struct{}

// LoggingConverter converts a Nirvana logging middleware to Huma.
func (c *CommonMiddlewareConverters) LoggingConverter(nirvanaMw func(ctx context.Context, chain definition.Chain) error) HumaMiddlewareFunc {
	return ConvertMiddleware(func(ctx context.Context, chain definition.Chain) error {
		// Add logging logic here
		return nirvanaMw(ctx, chain)
	}, WithName("logging"))
}

// AuthConverter converts a Nirvana authentication middleware to Huma.
func (c *CommonMiddlewareConverters) AuthConverter(nirvanaMw func(ctx context.Context, chain definition.Chain) error) HumaMiddlewareFunc {
	return ConvertMiddleware(func(ctx context.Context, chain definition.Chain) error {
		// Add auth logic here
		return nirvanaMw(ctx, chain)
	}, WithName("auth"), WithPriority(100))
}

// RecoveryConverter converts a Nirvana recovery middleware to Huma.
func (c *CommonMiddlewareConverters) RecoveryConverter(nirvanaMw func(ctx context.Context, chain definition.Chain) error) HumaMiddlewareFunc {
	return ConvertMiddleware(func(ctx context.Context, chain definition.Chain) error {
		// Add recovery logic here
		return nirvanaMw(ctx, chain)
	}, WithName("recovery"), WithPriority(1000))
}

// CORSConverter converts a Nirvana CORS middleware to Huma.
func (c *CommonMiddlewareConverters) CORSConverter(nirvanaMw func(ctx context.Context, chain definition.Chain) error) HumaMiddlewareFunc {
	return ConvertMiddleware(func(ctx context.Context, chain definition.Chain) error {
		// Add CORS logic here
		return nirvanaMw(ctx, chain)
	}, WithName("cors"), WithPriority(500))
}

// NewCommonConverters creates a new CommonMiddlewareConverters.
func NewCommonConverters() *CommonMiddlewareConverters {
	return &CommonMiddlewareConverters{}
}
