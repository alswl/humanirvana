package humanirvana_test

import (
	"context"
	"fmt"
	"net/http/httptest"

	"github.com/alswl/humanirvana"
	"github.com/caicloud/nirvana/definition"
	"github.com/caicloud/nirvana/service"
	"github.com/caicloud/nirvana/service/rest"
	"github.com/danielgtaylor/huma/v2"
)

func Example() {
	builder := rest.NewBuilder()
	adapter := humanirvana.NewAdapter(builder)
	api := huma.NewAPI(huma.DefaultConfig("Mixed routes", "1.0.0"), adapter)

	err := humanirvana.RegisterLegacy(builder, []definition.Descriptor{{
		Path: "/items/{id}", Consumes: []string{definition.MIMEAll}, Produces: []string{definition.MIMEJSON},
		Definitions: []definition.Definition{{
			Method: definition.Get,
			Function: func(id string) (string, error) {
				return "legacy item " + id, nil
			},
			Parameters: []definition.Parameter{{Source: definition.Path, Name: "id"}},
			Results:    definition.DataErrorResults("item"),
		}},
	}})
	if err != nil {
		panic(err)
	}

	type requestKey struct{}
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		ctx = huma.WithValue(ctx, requestKey{}, "request-42")
		r, w := humanirvana.Unwrap(ctx)
		w.Header().Set("X-HTTP-Method", r.Method)
		// Huma overrides do not update request.Context().
		nirvana := service.HTTPContextFrom(ctx.Context())
		w.Header().Set("X-Route", nirvana.RoutePath())
		next(ctx)
		fmt.Println("Huma status:", ctx.Status())
	})
	type input struct {
		ID string `path:"id"`
	}
	type output struct{ Body string }
	huma.Register[input, output](api, huma.Operation{Method: "POST", Path: "/items/{id}"}, func(ctx context.Context, in *input) (*output, error) {
		return &output{Body: in.ID + ":" + ctx.Value(requestKey{}).(string)}, nil
	})

	handler, err := builder.Build()
	if err != nil {
		panic(err)
	}
	if err := adapter.Bind(handler); err != nil {
		panic(err)
	}
	// Recorders keep this example runnable without an HTTP server.
	for _, method := range []string{"GET", "POST"} {
		w := httptest.NewRecorder()
		adapter.ServeHTTP(w, httptest.NewRequest(method, "/items/42", nil))
		fmt.Println(method, w.Code)
	}
	// Output:
	// GET 200
	// Huma status: 200
	// POST 200
}
