// Command middleware shows authentication, request-ID logging and an
// application-owned dependency shared by a legacy Nirvana descriptor and a
// Huma operation on one Nirvana router.
//
//	go run ./examples/middleware -listen 127.0.0.1:18080
//	curl -i -H 'Authorization: Bearer demo' -H 'X-Request-ID: r1' http://127.0.0.1:18080/legacy/greeting
//	curl -i -H 'Authorization: Bearer demo' -H 'X-Request-ID: r2' http://127.0.0.1:18080/greetings/ada
//	curl -i http://127.0.0.1:18080/greetings/ada   # 401, the handler is not called
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/alswl/humanirvana"
	"github.com/caicloud/nirvana/definition"
	"github.com/caicloud/nirvana/errors"
	"github.com/caicloud/nirvana/service"
	"github.com/caicloud/nirvana/service/rest"
	"github.com/danielgtaylor/huma/v2"
)

// Greeter is an application-owned dependency. It is created once in main and
// shared by both handlers; the library never constructs it.
type Greeter struct{ calls atomic.Int64 }

func (g *Greeter) Greet(ctx context.Context, name string) string {
	g.calls.Add(1)
	return fmt.Sprintf("hello %s (request %s, call %d)", name, requestID(ctx), g.calls.Load())
}

type requestIDKey struct{}

func requestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

var errUnauthorized = errors.Unauthorized.Build("Example:Unauthorized", "authorization required")

// accessLog is a Nirvana node middleware: it derives the request context with
// a request ID, enforces bearer auth, and logs the final status. It wraps both
// legacy descriptors and Huma operations registered below it.
func accessLog(ctx context.Context, next definition.Chain) error {
	hc := service.HTTPContextFrom(ctx)
	r := hc.Request()
	id := r.Header.Get("X-Request-ID")
	if id == "" {
		id = fmt.Sprintf("gen-%d", time.Now().UnixNano())
	}
	start := time.Now()
	var err error
	if r.Header.Get("Authorization") != "Bearer demo" {
		err = errUnauthorized.Error()
	} else {
		err = next.Continue(context.WithValue(ctx, requestIDKey{}, id))
	}
	if err != nil {
		// Nirvana writes the error response after the middleware returns, so
		// the final status is not known here yet.
		log.Printf("request=%s %s %s route=%s error=%v in %s", id, r.Method, r.URL.Path, hc.RoutePath(), err, time.Since(start))
		return err
	}
	log.Printf("request=%s %s %s route=%s status=%d in %s", id, r.Method, r.URL.Path, hc.RoutePath(), hc.ResponseWriter().StatusCode(), time.Since(start))
	return nil
}

type greetingInput struct {
	Name string `path:"name" minLength:"1"`
}

type greetingOutput struct {
	RequestID string `header:"X-Request-ID"`
	Body      struct {
		Message string `json:"message"`
	}
}

func newHandler(greeter *Greeter) (http.Handler, error) {
	builder := rest.NewBuilder()
	builder.SetModifier(service.ConsumeNoneForHTTPGet())
	// The root descriptor's middleware applies to every route below it,
	// including the Huma operations registered through the adapter.
	if err := builder.AddDescriptor(definition.Descriptor{Path: "/", Middlewares: []definition.Middleware{accessLog}}); err != nil {
		return nil, err
	}

	// Existing Nirvana descriptor and handler, unchanged.
	legacy := definition.Descriptor{Path: "/legacy/greeting", Definitions: []definition.Definition{{
		Method:     definition.Get,
		Parameters: []definition.Parameter{{Source: definition.Prefab, Name: "context"}},
		Results:    definition.DataErrorResults("greeting"),
		Function: func(ctx context.Context) (map[string]string, error) {
			return map[string]string{"message": greeter.Greet(ctx, "legacy")}, nil
		},
		Consumes: []string{definition.MIMENone}, Produces: []string{definition.MIMEJSON}, ErrorProduces: []string{definition.MIMEJSON},
	}}}
	if err := humanirvana.RegisterLegacy(builder, []definition.Descriptor{legacy}); err != nil {
		return nil, err
	}

	// New Huma operation using the same dependency and request context.
	adapter := humanirvana.NewAdapter(builder)
	api := huma.NewAPI(huma.DefaultConfig("middleware example", "1.0.0"), adapter)
	huma.Register(api, huma.Operation{OperationID: "greet", Method: http.MethodGet, Path: "/greetings/{name}"},
		func(ctx context.Context, in *greetingInput) (*greetingOutput, error) {
			out := &greetingOutput{RequestID: requestID(ctx)}
			out.Body.Message = greeter.Greet(ctx, in.Name)
			return out, nil
		})

	svc, err := builder.Build()
	if err != nil {
		return nil, err
	}
	if err := adapter.Bind(svc); err != nil {
		return nil, err
	}
	return adapter, nil
}

func main() {
	addr := flag.String("listen", "127.0.0.1:18080", "HTTP listen address")
	flag.Parse()
	h, err := newHandler(&Greeter{})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("listening on http://%s", *addr)
	srv := &http.Server{Addr: *addr, Handler: h, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
