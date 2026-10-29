package humanirvana_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alswl/humanirvana"
	"github.com/caicloud/nirvana/definition"
	"github.com/danielgtaylor/huma/v2"
)

func TestHTTPMethodContract(t *testing.T) {
	h := newAPI()
	for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"} {
		h.adapter.Handle(&huma.Operation{Method: method, Path: "/methods/{id}"}, func(ctx huma.Context) {
			ctx.SetHeader("X-Method", ctx.Method())
			ctx.SetHeader("X-ID", ctx.Param("id"))
			ctx.SetStatus(204)
		})
	}
	h.finish(t)
	for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE", "CONNECT", "CUSTOM"} {
		t.Run(method, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.handler.ServeHTTP(w, httptest.NewRequest(method, "/methods/42", nil))
			supported := strings.Contains(" GET HEAD POST PUT PATCH DELETE ", " "+method+" ")
			if supported {
				if w.Code != 204 || w.Header().Get("X-Method") != method || w.Header().Get("X-ID") != "42" {
					t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
				}
			} else if w.Code != 405 || w.Header().Get("X-Method") != "" {
				t.Fatalf("unsupported request: status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	for _, method := range []string{"OPTIONS", "TRACE", "CONNECT", "CUSTOM", "get", ""} {
		t.Run("register-"+method, func(t *testing.T) {
			h := newAPI()
			before := len(h.builder.Definitions())
			defer func() {
				if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "unsupported Nirvana HTTP method") {
					t.Errorf("registration panic=%v", r)
				}
				if len(h.builder.Definitions()) != before {
					t.Errorf("unsupported method mutated definitions: %v", h.builder.Definitions())
				}
			}()
			h.adapter.Handle(&huma.Operation{Method: method, Path: "/bad"}, func(huma.Context) {})
		})
	}
}

func TestHeadMustBeRegisteredExplicitly(t *testing.T) {
	h := newAPI()
	h.adapter.Handle(&huma.Operation{Method: "GET", Path: "/get-only"}, func(ctx huma.Context) { ctx.SetStatus(204) })
	h.finish(t)
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, httptest.NewRequest("HEAD", "/get-only", nil))
	if w.Code != 405 {
		t.Fatalf("HEAD returned %d", w.Code)
	}
}

func TestPathMatchingAndInvalidSyntax(t *testing.T) {
	h := newAPI()
	for _, path := range []string{"/paths/{id}", "/paths/static"} {
		h.adapter.Handle(&huma.Operation{Method: "GET", Path: path}, func(ctx huma.Context) {
			ctx.SetHeader("X-Route", path)
			ctx.SetHeader("X-ID", ctx.Param("id"))
			ctx.SetStatus(204)
		})
	}
	h.finish(t)
	for _, tc := range []struct{ path, route, id string }{
		{"/paths/static", "/paths/static", ""},
		{"/paths/42", "/paths/{id}", "42"},
		{"/paths/a%2Fb", "/paths/{id}", "a/b"},
	} {
		w := httptest.NewRecorder()
		h.handler.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != 204 || w.Header().Get("X-Route") != tc.route || w.Header().Get("X-ID") != tc.id {
			t.Fatalf("%s: status=%d headers=%v", tc.path, w.Code, w.Header())
		}
	}
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "invalid route") {
			t.Errorf("invalid route panic=%v", r)
		}
	}()
	h.adapter.Handle(&huma.Operation{Method: "GET", Path: "/broken/{id"}, func(huma.Context) {})
}

func TestSharedRouteRenamedAndReorderedParameters(t *testing.T) {
	h := newAPI()
	type input struct {
		A string `path:"a"`
		B string `path:"b"`
	}
	for method, path := range map[string]string{"GET": "/pairs/{a}/{b}", "POST": "/pairs/{b}/{a}"} {
		huma.Register[input, reply](h.api, huma.Operation{Method: method, Path: path}, func(_ context.Context, in *input) (*reply, error) {
			return &reply{Body: in.A + ":" + in.B}, nil
		})
	}
	h.finish(t)
	for _, tc := range []struct{ method, want string }{{"GET", `"first:second"`}, {"POST", `"second:first"`}} {
		w := httptest.NewRecorder()
		h.handler.ServeHTTP(w, httptest.NewRequest(tc.method, "/pairs/first/second", nil))
		if w.Code != 200 || strings.TrimSpace(w.Body.String()) != tc.want {
			t.Fatalf("%s: status=%d body=%s", tc.method, w.Code, w.Body.String())
		}
	}
}

func legacyRoute(path string, method definition.Method) definition.Descriptor {
	return definition.Descriptor{Path: path, Consumes: []string{definition.MIMEAll}, Produces: []string{definition.MIMEJSON}, Definitions: []definition.Definition{{
		Method: method, Function: func() (string, error) { return "legacy", nil }, Results: definition.DataErrorResults("value"),
	}}}
}

func TestMixedRoutesEitherRegistrationOrder(t *testing.T) {
	for _, legacyFirst := range []bool{true, false} {
		t.Run(fmt.Sprint(legacyFirst), func(t *testing.T) {
			h := newAPI()
			addLegacy := func() {
				if err := humanirvana.RegisterLegacy(h.builder, []definition.Descriptor{legacyRoute("/mixed/{id}", definition.Get)}); err != nil {
					t.Fatal(err)
				}
			}
			if legacyFirst {
				addLegacy()
			}
			h.adapter.Handle(&huma.Operation{Method: "POST", Path: "/mixed/{id}"}, func(ctx huma.Context) {
				ctx.SetHeader("X-ID", ctx.Param("id"))
				ctx.SetStatus(202)
			})
			if !legacyFirst {
				addLegacy()
			}
			h.finish(t)
			for _, tc := range []struct {
				method, path string
				want         int
			}{{"GET", "/mixed/42", 200}, {"POST", "/mixed/42", 202}, {"DELETE", "/mixed/42", 405}, {"GET", "/missing", 404}} {
				w := httptest.NewRecorder()
				h.handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
				if w.Code != tc.want || (tc.method == "GET" && tc.want == 200 && !strings.Contains(w.Body.String(), "legacy")) || (tc.method == "POST" && w.Header().Get("X-ID") != "42") {
					t.Fatalf("%s %s: status=%d body=%s", tc.method, tc.path, w.Code, w.Body.String())
				}
			}
		})
	}
}

func TestRouteConflicts(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		kind definition.Method
	}{{"same-method", "/conflict/{id}", definition.Get}, {"renamed-parameter", "/conflict/{name}", definition.Get}, {"legacy-any", "/conflict/{id}", definition.Any}, {"system-route", "/openapi.json", definition.Get}} {
		t.Run(tc.name, func(t *testing.T) {
			h := newAPI()
			if tc.name != "system-route" {
				if err := h.builder.AddDescriptor(legacyRoute(tc.path, tc.kind)); err != nil {
					t.Fatal(err)
				}
			}
			path := "/conflict/{id}"
			if tc.name == "system-route" {
				path = tc.path
			}
			defer func() {
				if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "duplicate route") {
					t.Errorf("duplicate panic=%v", r)
				}
			}()
			h.adapter.Handle(&huma.Operation{Method: "GET", Path: path}, func(huma.Context) {})
		})
	}
	t.Run("legacy-added-after-huma", func(t *testing.T) {
		h := newAPI()
		h.adapter.Handle(&huma.Operation{Method: "GET", Path: "/conflict"}, func(ctx huma.Context) { ctx.SetStatus(204) })
		if err := h.builder.AddDescriptor(legacyRoute("/conflict", definition.Get)); err != nil {
			t.Fatal(err)
		}
		if _, err := h.builder.Build(); err == nil {
			t.Fatal("Build accepted conflicting legacy route")
		}
		w := httptest.NewRecorder()
		h.adapter.ServeHTTP(w, httptest.NewRequest("GET", "/conflict", nil))
		if w.Code != 503 {
			t.Fatalf("failed build bound a service: %d", w.Code)
		}
	})
}

func TestBuildBindSnapshotContract(t *testing.T) {
	h := newAPI()
	first, err := h.builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	h.adapter.Handle(&huma.Operation{Method: "GET", Path: "/after-build"}, func(ctx huma.Context) { ctx.SetStatus(204) })
	w := httptest.NewRecorder()
	first.ServeHTTP(w, httptest.NewRequest("GET", "/after-build", nil))
	if w.Code != 404 {
		t.Fatalf("Build was not a snapshot: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.adapter.ServeHTTP(w, httptest.NewRequest("GET", "/after-build", nil))
	if w.Code != 503 {
		t.Fatalf("Build implicitly bound adapter: %d", w.Code)
	}
	if err := h.adapter.Bind(nil); err == nil {
		t.Fatal("Bind(nil) succeeded")
	}
	// Rebuild to include the route added after the first snapshot.
	h.finish(t)
	w = httptest.NewRecorder()
	h.adapter.ServeHTTP(w, httptest.NewRequest("GET", "/after-build", nil))
	if w.Code != 204 {
		t.Fatalf("rebuilt service omitted route: %d", w.Code)
	}
	if err := h.adapter.Bind(first); err == nil {
		t.Fatal("duplicate Bind succeeded")
	}
	w = httptest.NewRecorder()
	h.adapter.ServeHTTP(w, httptest.NewRequest("GET", "/after-build", nil))
	if w.Code != 204 {
		t.Fatalf("failed Bind replaced service: %d", w.Code)
	}
}

func TestSharedPrefixAndLateLegacyParameterNames(t *testing.T) {
	for _, lateLegacy := range []bool{false, true} {
		t.Run(fmt.Sprint(lateLegacy), func(t *testing.T) {
			h := newAPI()
			h.adapter.Handle(&huma.Operation{Method: "GET", Path: "/prefix/{id}"}, func(ctx huma.Context) {
				ctx.SetHeader("X-Value", ctx.Param("id"))
				ctx.SetStatus(204)
			})
			postPath, postStatus := "/prefix/42/child", 204
			if lateLegacy {
				// Direct descriptors bypass the adapter's parameter-name reuse.
				d := legacyRoute("/prefix/{name}", definition.Create)
				d.Definitions[0].Function = func(name string) (string, error) { return name, nil }
				d.Definitions[0].Parameters = []definition.Parameter{{Source: definition.Path, Name: "name"}}
				if err := h.builder.AddDescriptor(d); err != nil {
					t.Fatal(err)
				}
				postPath, postStatus = "/prefix/42", 201
			} else {
				h.adapter.Handle(&huma.Operation{Method: "POST", Path: "/prefix/{name}/child"}, func(ctx huma.Context) {
					ctx.SetHeader("X-Value", ctx.Param("name"))
					ctx.SetStatus(204)
				})
			}
			h.finish(t)
			for _, tc := range []struct {
				method, path string
				status       int
			}{{"GET", "/prefix/42", 204}, {"POST", postPath, postStatus}} {
				w := httptest.NewRecorder()
				h.handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
				if w.Code != tc.status || (tc.status == 204 && w.Header().Get("X-Value") != "42") || (tc.status == 201 && !strings.Contains(w.Body.String(), "42")) {
					t.Fatalf("%s %s: status=%d headers=%v body=%s", tc.method, tc.path, w.Code, w.Header(), w.Body.String())
				}
			}
		})
	}
}
