package pilot_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alswl/humanirvana/internal/pilot"
	"github.com/caicloud/nirvana/definition"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type output struct {
	Value string `json:"value"`
}

func valid() pilot.Operation {
	return pilot.Operation{Path: "/valid", Definition: definition.Definition{Method: definition.Get, Function: func(context.Context) (output, error) { return output{Value: "ok"}, nil }, Results: definition.DataErrorResults("output")}}
}

func TestRegistrationRejectsBatchBeforePublishing(t *testing.T) {
	cases := map[string]func() []pilot.Operation{
		"typed_nil_handler": func() []pilot.Operation {
			op := valid()
			op.Path = "/invalid"
			var fn func(context.Context) (output, error)
			op.Definition.Function = fn
			return []pilot.Operation{valid(), op}
		},
		"nil_middleware": func() []pilot.Operation {
			op := valid()
			op.Path = "/invalid"
			op.Middlewares = []definition.Middleware{nil}
			return []pilot.Operation{valid(), op}
		},

		"duplicate": func() []pilot.Operation { return []pilot.Operation{valid(), valid()} },
		"signature": func() []pilot.Operation {
			op := valid()
			op.Path = "/invalid"
			op.Definition.Function = func(int) {}
			return []pilot.Operation{valid(), op}
		},
		"unsupported_source": func() []pilot.Operation {
			op := valid()
			op.Path = "/invalid"
			op.Definition.Parameters = []definition.Parameter{{Source: definition.Form, Name: "field"}}
			return []pilot.Operation{valid(), op}
		},
		"invalid_path": func() []pilot.Operation { op := valid(); op.Path = "/bad/{}"; return []pilot.Operation{valid(), op} },
		"renamed_duplicate": func() []pilot.Operation {
			a, b := valid(), valid()
			a.Path = "/valid/{one}"
			b.Path = "/valid/{two}"
			return []pilot.Operation{a, b}
		},
	}
	for name, ops := range cases {
		t.Run(name, func(t *testing.T) {
			router := http.NewServeMux()
			api := humago.New(router, huma.DefaultConfig("test", "1"))
			if err := pilot.Register(api, ops()...); err == nil {
				t.Fatal("invalid batch accepted")
			}
			if api.OpenAPI().Paths["/valid"] != nil {
				t.Fatal("part of invalid batch published")
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", "/valid", nil))
			if w.Code != 404 {
				t.Fatalf("partial route registered: %d", w.Code)
			}
		})
	}
}

func TestHumaMiddlewareAndSchema(t *testing.T) {
	router := http.NewServeMux()
	api := humago.New(router, huma.DefaultConfig("test", "1"))
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) { ctx.SetHeader("X-Huma-Middleware", "ran"); next(ctx) })
	if err := pilot.Register(api, valid()); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/valid", nil))
	if w.Code != 200 || w.Result().Header.Get("X-Huma-Middleware") != "ran" {
		t.Fatalf("status=%d headers=%v", w.Code, w.Result().Header)
	}
	op := api.OpenAPI().Paths["/valid"].Get
	if op == nil || op.Responses["200"].Content["application/json"].Schema == nil {
		t.Fatal("response schema missing")
	}
	if _, err := api.OpenAPI().MarshalJSON(); err != nil {
		t.Fatal(err)
	}
}
