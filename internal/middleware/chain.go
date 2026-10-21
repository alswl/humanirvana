package middleware

import (
	"context"

	"github.com/caicloud/nirvana/definition"
	"github.com/danielgtaylor/huma/v2"
)

// ChainAdapter adapts a Huma middleware chain to accept Nirvana Chain.
type ChainAdapter struct {
	ctx  context.Context
	next func(context.Context)
	huma huma.Context
}

// NewChainAdapter creates a new chain adapter.
func NewChainAdapter(ctx context.Context, next func(context.Context)) *ChainAdapter {
	return &ChainAdapter{
		ctx:  ctx,
		next: next,
	}
}

// Continue implements definition.Chain.
func (c *ChainAdapter) Continue(ctx context.Context) error {
	c.next(ctx)
	return nil
}

// Context implements definition.Chain.
func (c *ChainAdapter) Context() context.Context {
	return c.ctx
}

// ToHuma converts a Nirvana Chain to a Huma next function.
func (c *ChainAdapter) ToHuma() func(huma.Context) {
	return func(humaCtx huma.Context) {
		c.next(humaCtx.Context())
	}
}

// NirvanaChainBridge bridges Nirvana and Huma middleware chains.
type NirvanaChainBridge struct {
	// handlers holds the Huma middleware chain
	handlers []func(huma.Context)

	// index tracks current position in chain
	index int
}

// NewNirvanaChainBridge creates a new bridge.
func NewNirvanaChainBridge() *NirvanaChainBridge {
	return &NirvanaChainBridge{
		handlers: make([]func(huma.Context), 0),
		index:    -1,
	}
}

// AddHandler adds a Huma handler to the chain.
func (b *NirvanaChainBridge) AddHandler(handler func(huma.Context)) {
	b.handlers = append(b.handlers, handler)
}

// Build builds the final handler that executes all handlers in order.
func (b *NirvanaChainBridge) Build() func(huma.Context) {
	return func(ctx huma.Context) {
		for _, h := range b.handlers {
			h(ctx)
		}
	}
}

// executeNext executes the next handler in the chain (used when handlers call next).
func (b *NirvanaChainBridge) executeNext(ctx huma.Context) {
	b.index++
	if b.index < len(b.handlers) {
		b.handlers[b.index](ctx)
	}
}

// NirvanaToHumaChain converts a Nirvana middleware chain to Huma format.
func NirvanaToHumaChain(nirvanaChain definition.Chain, humaCtx huma.Context) func(context.Context) {
	return func(ctx context.Context) {
		nirvanaChain.Continue(ctx)
	}
}

// ExecuteMiddleware executes a Nirvana middleware with the given chain.
func ExecuteMiddleware(
	middleware func(ctx context.Context, chain definition.Chain) error,
	ctx context.Context,
	chain definition.Chain,
) error {
	return middleware(ctx, chain)
}

// CreateNirvanaChain creates a Chain that wraps Huma's next function.
// The chain holds a default context (Background) and the given next handler.
func CreateNirvanaChain(humaNext func(context.Context)) definition.Chain {
	return &simpleChain{
		ctx:  context.Background(),
		next: humaNext,
	}
}

// simpleChain is a basic Chain implementation.
type simpleChain struct {
	ctx  context.Context
	next func(context.Context)
}

// Continue continues execution.
func (c *simpleChain) Continue(ctx context.Context) error {
	c.next(ctx)
	return nil
}

// Context returns the context.
func (c *simpleChain) Context() context.Context {
	return c.ctx
}

// WithContext creates a new chain with a different context.
func (c *simpleChain) WithContext(ctx context.Context) definition.Chain {
	return &simpleChain{
		ctx:  ctx,
		next: c.next,
	}
}
