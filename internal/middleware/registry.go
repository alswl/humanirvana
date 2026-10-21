package middleware

import (
	"github.com/danielgtaylor/huma/v2"
)

// UseMiddleware applies a single middleware (function form) to the API.
func UseMiddleware(api huma.API, mw HumaMiddlewareFunc) {
	api.UseMiddleware(mw)
}

// Chain creates a middleware chain from Huma middleware functions (ctx, next).
func Chain(middlewares ...HumaMiddlewareFunc) func(huma.Context) {
	if len(middlewares) == 0 {
		return func(ctx huma.Context) {}
	}

	handler := func(ctx huma.Context) {}
	for i := len(middlewares) - 1; i >= 0; i-- {
		mw := middlewares[i]
		next := handler
		handler = func(ctx huma.Context) {
			mw(ctx, next)
		}
	}
	return handler
}

// Compose composes multiple middleware functions (each runs in order).
func Compose(middlewares ...func(huma.Context)) func(huma.Context) {
	if len(middlewares) == 0 {
		return func(ctx huma.Context) {}
	}

	return func(ctx huma.Context) {
		for _, mw := range middlewares {
			mw(ctx)
		}
	}
}
