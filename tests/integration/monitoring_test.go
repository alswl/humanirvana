package demo_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alswl/humanirvana"
	"github.com/caicloud/nirvana/definition"
	"github.com/caicloud/nirvana/service"
	"github.com/caicloud/nirvana/service/rest"
	"github.com/danielgtaylor/huma/v2"
)

type monitorReply struct {
	Body string `json:"body"`
}

type monitor struct {
	routes   []string
	statuses []int
	elapsed  []time.Duration
}

// newMonitoredAPI wires an existing-style Nirvana monitoring middleware
// (route, status, latency, request deadline) in front of a Huma API.
func newMonitoredAPI(t *testing.T, register func(huma.API)) (http.Handler, *monitor) {
	t.Helper()
	m := &monitor{}
	builder := rest.NewBuilder()
	if err := builder.AddDescriptor(definition.Descriptor{Path: "/", Middlewares: []definition.Middleware{
		func(ctx context.Context, next definition.Chain) error {
			start := time.Now()
			ctx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			err := next.Continue(ctx)
			hc := service.HTTPContextFrom(ctx)
			m.routes = append(m.routes, hc.RoutePath())
			m.statuses = append(m.statuses, hc.ResponseWriter().StatusCode())
			m.elapsed = append(m.elapsed, time.Since(start))
			return err
		},
	}}); err != nil {
		t.Fatal(err)
	}
	adapter := humanirvana.NewAdapter(builder)
	config := huma.DefaultConfig("monitoring", "1.0.0")
	config.Transformers = append(config.Transformers, func(ctx huma.Context, status string, v any) (any, error) {
		ctx.SetHeader("X-Transformed", status)
		return v, nil
	})
	register(huma.NewAPI(config, adapter))
	svc, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Bind(svc); err != nil {
		t.Fatal(err)
	}
	return adapter, m
}

// T034: monitoring data owned by Nirvana middleware (route template, final
// status, latency) and request deadlines reach and observe Huma operations;
// Huma response transformers still run.
func TestMonitoringMiddlewareObservesHumaOperations(t *testing.T) {
	var deadlineSeen bool
	handler, m := newMonitoredAPI(t, func(api huma.API) {
		huma.Register(api, huma.Operation{Method: http.MethodGet, Path: "/items/{id}", OperationID: "item"}, func(ctx context.Context, in *struct {
			ID string `path:"id"`
		}) (*monitorReply, error) {
			_, deadlineSeen = ctx.Deadline()
			if in.ID == "missing" {
				return nil, huma.Error404NotFound("missing")
			}
			return &monitorReply{in.ID}, nil
		})
	})
	for _, target := range []string{"/items/1", "/items/missing"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
		if got := w.Header().Get("X-Transformed"); got == "" {
			t.Fatalf("%s: Huma transformer did not run", target)
		}
	}
	if !deadlineSeen {
		t.Fatal("deadline set by Nirvana middleware is not visible to the Huma handler")
	}
	if len(m.statuses) != 2 || m.statuses[0] != http.StatusOK || m.statuses[1] != http.StatusNotFound {
		t.Fatalf("monitored statuses = %v, want [200 404]", m.statuses)
	}
	for _, route := range m.routes {
		if route != "/items/{id}" {
			t.Fatalf("monitored route = %q, want the route template", route)
		}
	}
}

// T034: client cancellation propagates through Nirvana into the Huma handler.
func TestClientCancellationReachesHumaHandler(t *testing.T) {
	var handlerErr error
	handler, m := newMonitoredAPI(t, func(api huma.API) {
		huma.Register(api, huma.Operation{Method: http.MethodGet, Path: "/slow", OperationID: "slow"}, func(ctx context.Context, _ *struct{}) (*monitorReply, error) {
			<-ctx.Done()
			handlerErr = ctx.Err()
			return nil, huma.Error503ServiceUnavailable("canceled")
		})
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/slow", nil).WithContext(ctx))
	if !errors.Is(handlerErr, context.Canceled) {
		t.Fatalf("handler context error = %v, want context.Canceled", handlerErr)
	}
	if len(m.statuses) != 1 || m.statuses[0] != http.StatusServiceUnavailable {
		t.Fatalf("monitored statuses = %v, want [503]", m.statuses)
	}
}
