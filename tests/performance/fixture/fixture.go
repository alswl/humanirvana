// Package fixture defines the PERF-01 technical fixture: the same five Huma
// operations served by a standard Huma arm (humago on net/http) and by the
// adapter arm (humanirvana on a Nirvana router). Both arms share one Huma
// configuration, so they do equivalent work.
package fixture

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/alswl/humanirvana"
	"github.com/caicloud/nirvana/service/rest"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

// ProtocolRevision identifies the frozen fixture. Changing operations,
// payloads or expectations requires a new revision and a full re-run.
const ProtocolRevision = 1

// Arms under comparison.
const (
	Native  = "native"
	Adapter = "adapter"
)

// Config is the Huma configuration both arms use. Schema link hooks are
// removed so response bodies do not depend on the listening address.
func Config() huma.Config {
	c := huma.DefaultConfig("perf01", "1.0.0")
	c.CreateHooks = nil
	return c
}

// NewHandler builds one arm.
func NewHandler(arm string) (http.Handler, error) {
	switch arm {
	case Native:
		mux := http.NewServeMux()
		Register(humago.New(mux, Config()))
		return mux, nil
	case Adapter:
		builder := rest.NewBuilder()
		adapter := humanirvana.NewAdapter(builder)
		Register(huma.NewAPI(Config(), adapter))
		svc, err := builder.Build()
		if err != nil {
			return nil, err
		}
		if err := adapter.Bind(svc); err != nil {
			return nil, err
		}
		return adapter, nil
	}
	return nil, fmt.Errorf("unknown arm %q", arm)
}

// Sink is an in-memory log written by the P05 middleware; it does no I/O.
type Sink struct {
	entries [1024]atomic.Pointer[string]
	next    atomic.Uint64
}

func (s *Sink) write(entry string) {
	i := s.next.Add(1) % uint64(len(s.entries))
	s.entries[i].Store(&entry)
}

// Writes returns the number of log entries written.
func (s *Sink) Writes() uint64 { return s.next.Load() }

type ctxKey string

// ParamsInput is P01.
type ParamsInput struct {
	ID      int    `path:"id"`
	Count   int    `query:"count"`
	Enabled bool   `query:"enabled"`
	Tenant  string `header:"X-Tenant"`
}

// ParamsOutput echoes P01's parsed values.
type ParamsOutput struct {
	Body struct {
		ID      int    `json:"id"`
		Count   int    `json:"count"`
		Enabled bool   `json:"enabled"`
		Tenant  string `json:"tenant"`
	}
}

// SmallBody is the P02/P04 request body.
type SmallBody struct {
	Quantity int    `json:"quantity" minimum:"1"`
	Padding  string `json:"padding"`
}

// SmallInput is P02 and P04.
type SmallInput struct {
	Body SmallBody
}

// SmallOutput confirms P02.
type SmallOutput struct {
	Body struct {
		Accepted bool `json:"accepted"`
		Quantity int  `json:"quantity"`
	}
}

// Item is one P03 entry.
type Item struct {
	ID    int    `json:"id" minimum:"0"`
	Name  string `json:"name" minLength:"1"`
	Attrs struct {
		Color string `json:"color" enum:"red,green,blue"`
		Size  int    `json:"size" minimum:"0"`
	} `json:"attrs"`
}

// NestedInput is P03.
type NestedInput struct {
	Body struct {
		Items   []Item `json:"items" minItems:"1" maxItems:"64"`
		Padding string `json:"padding"`
	}
}

// NestedOutput returns P03's item count.
type NestedOutput struct {
	Body struct {
		Count int `json:"count"`
	}
}

// MiddlewareInput is P05.
type MiddlewareInput struct {
	RequestID string `header:"X-Request-ID"`
}

// MiddlewareOutput returns P05's middleware order and context values.
type MiddlewareOutput struct {
	Body struct {
		Order     []string `json:"order"`
		Values    string   `json:"values"`
		RequestID string   `json:"request_id"`
	}
}

// Register adds P01–P05 to api and returns the P05 log sink.
func Register(api huma.API) *Sink {
	sink := &Sink{}
	huma.Register(api, huma.Operation{OperationID: "p01-params", Method: http.MethodGet, Path: "/perf/params/{id}"},
		func(_ context.Context, in *ParamsInput) (*ParamsOutput, error) {
			out := &ParamsOutput{}
			out.Body.ID, out.Body.Count, out.Body.Enabled, out.Body.Tenant = in.ID, in.Count, in.Enabled, in.Tenant
			return out, nil
		})
	huma.Register(api, huma.Operation{OperationID: "p02-json-small", Method: http.MethodPost, Path: "/perf/json-small", DefaultStatus: http.StatusCreated},
		func(_ context.Context, in *SmallInput) (*SmallOutput, error) {
			out := &SmallOutput{}
			out.Body.Accepted, out.Body.Quantity = true, in.Body.Quantity
			return out, nil
		})
	huma.Register(api, huma.Operation{OperationID: "p03-json-nested", Method: http.MethodPost, Path: "/perf/json-nested"},
		func(_ context.Context, in *NestedInput) (*NestedOutput, error) {
			out := &NestedOutput{}
			out.Body.Count = len(in.Body.Items)
			return out, nil
		})
	huma.Register(api, huma.Operation{OperationID: "p04-validate", Method: http.MethodPost, Path: "/perf/validate"},
		func(_ context.Context, in *SmallInput) (*SmallOutput, error) {
			out := &SmallOutput{}
			out.Body.Accepted, out.Body.Quantity = true, in.Body.Quantity
			return out, nil
		})

	step := func(name string, read, set ctxKey, value string) func(huma.Context, func(huma.Context)) {
		return func(ctx huma.Context, next func(huma.Context)) {
			order, _ := ctx.Context().Value(ctxKey("order")).([]string)
			prev, _ := ctx.Context().Value(read).(string)
			ctx = huma.WithValue(ctx, ctxKey("order"), append(append([]string(nil), order...), name))
			ctx = huma.WithValue(ctx, set, strings.TrimPrefix(prev+","+value, ","))
			sink.write(name + " " + ctx.Header("X-Request-ID"))
			next(ctx)
		}
	}
	api.UseMiddleware(step("api", "", "v1", "a"))
	huma.Register(api, huma.Operation{OperationID: "p05-middleware", Method: http.MethodGet, Path: "/perf/middleware",
		Middlewares: huma.Middlewares{step("op1", "v1", "v2", "b"), step("op2", "v2", "v3", "c")}},
		func(ctx context.Context, in *MiddlewareInput) (*MiddlewareOutput, error) {
			out := &MiddlewareOutput{}
			order, _ := ctx.Value(ctxKey("order")).([]string)
			out.Body.Order = order
			out.Body.Values, _ = ctx.Value(ctxKey("v3")).(string)
			out.Body.RequestID = in.RequestID
			return out, nil
		})
	return sink
}
