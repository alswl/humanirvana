// Package demo is a test-first migration application using shared Nirvana handlers.
package demo

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/alswl/humanirvana/internal/pilot"
	"github.com/caicloud/nirvana/definition"
	"github.com/caicloud/nirvana/service"
	nrest "github.com/caicloud/nirvana/service/rest"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

// Order is an application record with deterministic IDs for isolated comparisons.
type Order struct {
	ID       int    `json:"id"`
	Item     string `json:"item"`
	Quantity int    `json:"quantity"`
}

// CreateOrder is the original Nirvana handler's input.
type CreateOrder struct {
	Item     string `json:"item"`
	Quantity int    `json:"quantity"`
}

type repository struct {
	mu          sync.Mutex
	orders      map[int]Order
	writes      int
	createCalls int
	trace       []string
}

func newRepository() *repository {
	return &repository{orders: map[int]Order{1: {ID: 1, Item: "seed", Quantity: 1}}}
}

type traceKey struct{}
type trace struct{ events []string }

func record(ctx context.Context, event string) {
	if t, ok := ctx.Value(traceKey{}).(*trace); ok {
		t.events = append(t.events, event)
	}
}

type appError struct {
	status  int
	message string
}

func (e appError) Error() string        { return e.message }
func (e appError) Code() int            { return e.status }
func (e appError) Message() interface{} { return map[string]string{"message": e.message} }

type validateOrder struct{}

func (validateOrder) Kind() string      { return "validator" }
func (validateOrder) In() reflect.Type  { return reflect.TypeOf(CreateOrder{}) }
func (validateOrder) Out() reflect.Type { return reflect.TypeOf(CreateOrder{}) }
func (validateOrder) Operate(ctx context.Context, _ string, value interface{}) (interface{}, error) {
	record(ctx, "validate")
	input := value.(CreateOrder)
	if strings.TrimSpace(input.Item) == "" || input.Quantity < 1 {
		return nil, appError{400, "item and positive quantity required"}
	}
	return input, nil
}

func (r *repository) create(ctx context.Context, input CreateOrder) (*Order, error) {
	record(ctx, "handler")
	r.mu.Lock()
	r.createCalls++
	r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, appError{408, "request cancelled"}
	}
	if input.Item == "sold-out" {
		return nil, appError{409, "item sold out"}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	order := Order{ID: len(r.orders) + 1, Item: input.Item, Quantity: input.Quantity}
	r.orders[order.ID] = order
	r.writes++
	return &order, nil
}
func (r *repository) get(ctx context.Context, id int) (*Order, error) {
	record(ctx, "handler")
	r.mu.Lock()
	defer r.mu.Unlock()
	order, ok := r.orders[id]
	if !ok {
		return nil, appError{404, "order not found"}
	}
	return &order, nil
}
func (r *repository) audit(ctx context.Context, next definition.Chain) error {
	t := &trace{}
	ctx = context.WithValue(ctx, traceKey{}, t)
	record(ctx, "audit.before")
	req := service.HTTPContextFrom(ctx).Request()
	service.HTTPContextFrom(ctx).ResponseWriter().Header().Set("X-Request-ID", req.Header.Get("X-Request-ID"))
	defer func() {
		record(ctx, "audit.after")
		r.mu.Lock()
		r.trace = append([]string(nil), t.events...)
		r.mu.Unlock()
	}()
	return next.Continue(ctx)
}
func authorize(ctx context.Context, next definition.Chain) error {
	record(ctx, "auth")
	if service.HTTPContextFrom(ctx).Request().Header.Get("Authorization") != "Bearer demo-token" {
		return appError{401, "unauthorized"}
	}
	return next.Continue(ctx)
}

// App contains two independent stores and handlers using the same business code.
type App struct {
	Original, Migrated http.Handler
	original, migrated *repository
}

// New builds the original Nirvana application and the experimental Huma bridge.
func New() (*App, error) {
	a := &App{original: newRepository(), migrated: newRepository()}
	originalOperations := operations(a.original)
	builder := nrest.NewBuilder()
	builder.SetModifier(service.DefinitionModifiers{service.FirstContextParameter(), service.ConsumeNoneForHTTPGet()}.Combine())
	// The original tree inherits middleware once from /orders. The pilot receives
	// the equivalent explicit list per operation, rather than nesting it twice.
	descriptor := definition.Descriptor{
		Path:        "/orders",
		Middlewares: originalOperations[0].Middlewares,
		Definitions: []definition.Definition{originalOperations[0].Definition},
		Children:    []definition.Descriptor{{Path: "/{id}", Definitions: []definition.Definition{originalOperations[1].Definition}}},
	}
	if err := builder.AddDescriptor(descriptor); err != nil {
		return nil, err
	}
	legacy := definition.Descriptor{Path: "/legacy", Definitions: []definition.Definition{{
		Method: definition.Get,
		Function: func(context.Context) (map[string]string, error) {
			return map[string]string{"status": "unmigrated"}, nil
		},
		Results:  definition.DataErrorResults("LegacyStatus"),
		Consumes: []string{definition.MIMEJSON}, Produces: []string{definition.MIMEJSON}, ErrorProduces: []string{definition.MIMEJSON},
	}}}
	if err := builder.AddDescriptor(legacy); err != nil {
		return nil, err
	}
	original, err := builder.Build()
	if err != nil {
		return nil, err
	}
	router := http.NewServeMux()
	api := humago.New(router, huma.DefaultConfig("TDD migration demo", "0.1.0"))
	if err := pilot.Register(api, operations(a.migrated)...); err != nil {
		return nil, err
	}
	a.Original = pilot.Marker("original", original)
	a.Migrated = pilot.Marker("migrated", router)
	return a, nil
}

func operations(r *repository) []pilot.Operation {
	common := func(method definition.Method, fn interface{}, parameters []definition.Parameter) definition.Definition {
		return definition.Definition{Method: method, Function: fn, Parameters: parameters, Results: definition.DataErrorResults("Order"), Consumes: []string{definition.MIMEJSON}, Produces: []string{definition.MIMEJSON}, ErrorProduces: []string{definition.MIMEJSON}}
	}
	return []pilot.Operation{
		{Path: "/orders", Definition: common(definition.Create, r.create, []definition.Parameter{{Source: definition.Body, Operators: []definition.Operator{validateOrder{}}}}), Middlewares: []definition.Middleware{r.audit, authorize}},
		{Path: "/orders/{id}", Definition: common(definition.Get, r.get, []definition.Parameter{{Source: definition.Path, Name: "id"}}), Middlewares: []definition.Middleware{r.audit, authorize}},
	}
}

// Entry builds immutable startup ownership. Rollback selects the original backend.
// There is no automatic fallback or write replay.
func (a *App) Entry(mode string) (http.Handler, error) {
	var target http.Handler
	switch mode {
	case "original", "rollback":
		target = a.Original
	case "migrated":
		target = a.Migrated
	default:
		return nil, fmt.Errorf("unknown ownership mode %q", mode)
	}
	router := http.NewServeMux()
	router.Handle("POST /orders", target)
	router.Handle("GET /orders/{id}", target)
	router.Handle("GET /legacy", a.Original)
	return router, nil
}

// Writes reports successful writes to the selected isolated store.
func (a *App) Writes(backend string) int {
	r := a.repo(backend)
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.writes
}

// Trace returns the last completed operation's execution events for this demo.
func (a *App) Trace(backend string) []string {
	r := a.repo(backend)
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.trace...)
}
func (a *App) repo(backend string) *repository {
	if backend == "original" {
		return a.original
	}
	return a.migrated
}

// CreateCalls reports original business-handler invocations, including rejected writes.
func (a *App) CreateCalls(backend string) int {
	r := a.repo(backend)
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.createCalls
}
