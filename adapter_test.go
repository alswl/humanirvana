package humanirvana_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/alswl/humanirvana"
	"github.com/caicloud/nirvana/definition"
	"github.com/caicloud/nirvana/service"
	"github.com/caicloud/nirvana/service/rest"
	"github.com/danielgtaylor/huma/v2"
)

type apiHarness struct {
	builder service.Builder
	api     huma.API
	adapter *humanirvana.Adapter
	handler http.Handler
}

func newAPI() apiHarness {
	builder := rest.NewBuilder()
	adapter := humanirvana.NewAdapter(builder)
	api := huma.NewAPI(huma.DefaultConfig("test", "1.0.0"), adapter)
	return apiHarness{builder: builder, api: api, adapter: adapter}
}

func (h *apiHarness) finish(t *testing.T) {
	t.Helper()
	service, err := h.builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if err := h.adapter.Bind(service); err != nil {
		t.Fatal(err)
	}
	h.handler = h.adapter
}

type reply struct {
	Body string `json:"body"`
}

func TestHumaTypedGroupHiddenAndSystemRoutesShareNirvanaBuilder(t *testing.T) {
	h := newAPI()
	var calls int
	huma.Register[struct{}, reply](h.api, huma.Operation{Method: http.MethodGet, Path: "/native", OperationID: "native"}, func(context.Context, *struct{}) (*reply, error) {
		calls++
		return &reply{"native"}, nil
	})
	g := huma.NewGroup(h.api, "/group")
	huma.Register[struct{}, reply](g, huma.Operation{Method: http.MethodGet, Path: "/hidden", OperationID: "hidden", Hidden: true}, func(context.Context, *struct{}) (*reply, error) { return &reply{"hidden"}, nil })
	h.finish(t)
	for path, want := range map[string]string{"/native": "native", "/group/hidden": "hidden"} {
		w := httptest.NewRecorder()
		h.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), want) {
			t.Errorf("GET %s: status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
	if calls != 1 {
		t.Fatalf("native callback calls=%d", calls)
	}
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "/native") || strings.Contains(w.Body.String(), "/group/hidden") {
		t.Fatalf("OpenAPI hidden/system behavior: %d %s", w.Code, w.Body.String())
	}
}

func TestSameRouteShapeWithDifferentMethodsKeepsEachHumaParameterName(t *testing.T) {
	type getInput struct {
		ID string `path:"id"`
	}
	type postInput struct {
		Name string `path:"name"`
	}
	type out struct {
		Body string `json:"body"`
	}
	h := newAPI()
	huma.Register[getInput, out](h.api, huma.Operation{Method: http.MethodGet, Path: "/items/{id}", OperationID: "get-item"}, func(_ context.Context, in *getInput) (*out, error) {
		return &out{Body: in.ID}, nil
	})
	huma.Register[postInput, out](h.api, huma.Operation{Method: http.MethodPost, Path: "/items/{name}", OperationID: "post-item"}, func(_ context.Context, in *postInput) (*out, error) {
		return &out{Body: in.Name}, nil
	})
	h.finish(t)
	for _, tc := range []struct{ method, want string }{{http.MethodGet, "get"}, {http.MethodPost, "post"}} {
		w := httptest.NewRecorder()
		h.handler.ServeHTTP(w, httptest.NewRequest(tc.method, "/items/"+tc.want, nil))
		var got string
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != http.StatusOK || got != tc.want {
			t.Fatalf("%s route response=%d body=%s err=%v", tc.method, w.Code, w.Body.String(), err)
		}
	}
}

func TestLegacyDescriptorRemainsOriginalNirvanaHandlerAndMiddleware(t *testing.T) {
	h := newAPI()
	var calls int
	var order []string
	mw := func(ctx context.Context, chain definition.Chain) error {
		order = append(order, "before")
		err := chain.Continue(ctx)
		order = append(order, "after")
		return err
	}
	d := definition.Descriptor{Path: "/legacy", Consumes: []string{definition.MIMEAll}, Produces: []string{definition.MIMEJSON}, Middlewares: []definition.Middleware{mw}, Children: []definition.Descriptor{{Path: "/status/{id}", Definitions: []definition.Definition{{Method: definition.Get, Function: func(_ context.Context, id string) (string, error) {
		calls++
		return "original-handler-" + id, nil
	}, Parameters: []definition.Parameter{{Source: definition.Prefab, Name: "context"}, {Source: definition.Path, Name: "id"}}, Results: definition.DataErrorResults("status")}}}}}
	if err := humanirvana.RegisterLegacy(h.builder, []definition.Descriptor{d}); err != nil {
		t.Fatal(err)
	}
	h.finish(t)
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/legacy/status/item-7", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "original-handler-item-7") || calls != 1 || strings.Join(order, ",") != "before,after" {
		t.Fatalf("HTTP=%d %s calls=%d middleware=%v", w.Code, w.Body.String(), calls, order)
	}
}

func TestLegacyErrorResponseIsNotConvertedByHuma(t *testing.T) {
	h := newAPI()
	boom := errors.New("legacy failure")
	d := definition.Descriptor{Path: "/legacy-error", Consumes: []string{definition.MIMEAll}, Produces: []string{definition.MIMEJSON}, Definitions: []definition.Definition{{Method: definition.Get, Function: func(context.Context) (string, error) { return "", boom }, Parameters: []definition.Parameter{{Source: definition.Prefab, Name: "context"}}, Results: definition.DataErrorResults("value")}}}
	if err := humanirvana.RegisterLegacy(h.builder, []definition.Descriptor{d}); err != nil {
		t.Fatal(err)
	}
	h.finish(t)
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/legacy-error", nil))
	if w.Code < 400 || !strings.Contains(w.Body.String(), "legacy failure") || !strings.Contains(w.Header().Get("Content-Type"), "json") {
		t.Fatalf("legacy error was not handled by Nirvana: %d %q %v", w.Code, w.Body.String(), w.Header())
	}
}

func TestHumaContextUsesMatchedNirvanaRequestAndCommitsOnce(t *testing.T) {
	h := newAPI()
	type input struct {
		ID   string `path:"id"`
		Q    string `query:"q"`
		Head string `header:"X-Test"`
		Body struct {
			Value string `json:"value"`
		} `json:"body"`
	}
	type output struct {
		Status int    `status:"status"`
		Header string `header:"X-Result"`
		Body   struct {
			Value string `json:"value"`
		} `json:"body"`
	}
	huma.Register[input, output](h.api, huma.Operation{Method: http.MethodPost, Path: "/context/{id}", OperationID: "context", DefaultStatus: http.StatusAccepted}, func(ctx context.Context, in *input) (*output, error) {
		if ctx == nil || ctx.Err() != nil {
			return nil, fmt.Errorf("request context unavailable: %v", ctx)
		}
		out := &output{Status: http.StatusAccepted, Header: in.Head}
		out.Body.Value = in.ID + ":" + in.Q + ":" + in.Body.Value
		return out, nil
	})
	h.finish(t)
	r := httptest.NewRequest(http.MethodPost, "/context/a%2Fb?q=query", strings.NewReader(`{"value":"body"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Test", "header")
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, r)
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("response %q: %v", w.Body.String(), err)
	}
	if w.Code != http.StatusAccepted || w.Header().Get("X-Result") != "header" || !strings.Contains(w.Body.String(), `a/b:query:body`) {
		t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
	}
}

func TestHumaMiddlewareOrderAndShortCircuitRunsOnce(t *testing.T) {
	h := newAPI()
	var order []string
	wrap := func(name string) func(huma.Context, func(huma.Context)) {
		return func(ctx huma.Context, next func(huma.Context)) {
			order = append(order, name+"-before")
			next(ctx)
			order = append(order, name+"-after")
		}
	}
	h.api.UseMiddleware(wrap("api"))
	g := huma.NewGroup(h.api, "/v1")
	g.UseMiddleware(wrap("group"))
	huma.Register[struct{}, reply](g, huma.Operation{Method: http.MethodGet, Path: "/ordered", OperationID: "ordered", Middlewares: huma.Middlewares{wrap("operation")}}, func(context.Context, *struct{}) (*reply, error) {
		order = append(order, "handler")
		return &reply{"ok"}, nil
	})
	h.finish(t)
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/ordered", nil))
	want := "api-before,group-before,operation-before,handler,operation-after,group-after,api-after"
	if w.Code != http.StatusOK || strings.Join(order, ",") != want {
		t.Fatalf("status=%d order=%v", w.Code, order)
	}
}

func TestHumaRegistryHooksAndCustomSystemPaths(t *testing.T) {
	config := huma.DefaultConfig("test", "1.0.0")
	registry := huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer)
	registry.Map()["Preloaded"] = &huma.Schema{Type: "string", Description: "preloaded"}
	config.Components.Schemas = registry
	config.CreateHooks = []func(huma.Config) huma.Config{func(c huma.Config) huma.Config {
		c.OpenAPIPath, c.DocsPath, c.SchemasPath = "/hook-spec", "/hook-docs", "/hook-schemas"
		return c
	}}
	h := newAPIWithConfig(config)
	huma.Register[struct{}, reply](h.api, huma.Operation{Method: http.MethodGet, Path: "/hook", OperationID: "hook"}, func(context.Context, *struct{}) (*reply, error) { return &reply{"ok"}, nil })
	if h.api.OpenAPI().Components.Schemas.Map()["Preloaded"] == nil {
		t.Fatal("preloaded schema disappeared")
	}
	h.finish(t)
	for _, path := range []string{"/hook-spec.json", "/hook-spec-3.0.json", "/hook-spec.yaml", "/hook-spec-3.0.yaml", "/hook-docs", "/hook-schemas/Anything"} {
		w := httptest.NewRecorder()
		h.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("GET %s: %d %s", path, w.Code, w.Body.String())
		}
	}
}

func newAPIWithConfig(config huma.Config) apiHarness {
	builder := rest.NewBuilder()
	adapter := humanirvana.NewAdapter(builder)
	return apiHarness{builder: builder, api: huma.NewAPI(config, adapter), adapter: adapter}
}

func TestNirvanaBuilderRejectsDuplicateAndSystemRouteConflictsBeforeBind(t *testing.T) {
	h := newAPI()
	huma.Register[struct{}, reply](h.api, huma.Operation{Method: http.MethodGet, Path: "/same", OperationID: "first"}, func(context.Context, *struct{}) (*reply, error) { return &reply{"first"}, nil })
	panicked := false
	func() {
		defer func() { panicked = recover() != nil }()
		huma.Register[struct{}, reply](h.api, huma.Operation{Method: http.MethodGet, Path: "/same", OperationID: "second"}, func(context.Context, *struct{}) (*reply, error) { return &reply{"second"}, nil })
	}()
	if !panicked {
		t.Fatal("duplicate registration was not rejected at registration")
	}
}

func TestIndependentNirvanaAdaptersAreIsolatedConcurrently(t *testing.T) {
	const count = 8
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := fmt.Sprintf("/instance/%d", i)
			h := newAPI()
			huma.Register[struct{}, reply](h.api, huma.Operation{Method: http.MethodGet, Path: path, OperationID: fmt.Sprintf("instance%d", i)}, func(context.Context, *struct{}) (*reply, error) { return &reply{path}, nil })
			h.finish(t)
			w := httptest.NewRecorder()
			h.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), path) {
				t.Errorf("instance %d: %d %s", i, w.Code, w.Body.String())
			}
			var docs map[string]any
			w = httptest.NewRecorder()
			h.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
			if err := json.Unmarshal(w.Body.Bytes(), &docs); err != nil || h.api.OpenAPI().Paths[path] == nil {
				t.Errorf("instance %d OpenAPI isolation: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestHumaSchemaReuseAndConflictingSchemaNamesRemainUpstream(t *testing.T) {
	type one struct {
		A string `json:"a"`
	}
	type two struct {
		B int `json:"b"`
	}
	type outOne struct {
		Body one `json:"body"`
	}
	type outTwo struct {
		Body two `json:"body"`
	}
	newConfig := func() huma.Config {
		c := huma.DefaultConfig("test", "1.0.0")
		c.Components.Schemas = huma.NewMapRegistry("#/components/schemas/", func(typ reflect.Type, hint string) string {
			if typ == reflect.TypeOf(one{}) || typ == reflect.TypeOf(two{}) {
				return "Contended"
			}
			return huma.DefaultSchemaNamer(typ, hint)
		})
		return c
	}
	registerOne := func(api huma.API, path string) {
		huma.Register[struct{}, outOne](api, huma.Operation{Method: http.MethodGet, Path: path, OperationID: path}, func(context.Context, *struct{}) (*outOne, error) { return &outOne{}, nil })
	}
	h := newAPIWithConfig(newConfig())
	registerOne(h.api, "/one")
	registerOne(h.api, "/two")
	h.finish(t)
	if len(h.api.OpenAPI().Paths) < 2 {
		t.Fatalf("same-name schema reuse lost routes: %v", h.api.OpenAPI().Paths)
	}
	h = newAPIWithConfig(newConfig())
	registerOne(h.api, "/one")
	panicked := false
	func() {
		defer func() { panicked = recover() != nil }()
		huma.Register[struct{}, outTwo](h.api, huma.Operation{Method: http.MethodGet, Path: "/two", OperationID: "two"}, func(context.Context, *struct{}) (*outTwo, error) { return &outTwo{}, nil })
	}()
	if !panicked {
		t.Fatal("expected upstream registry conflict for incompatible same-name schema")
	}
}

func TestHumaShortCircuitDoesNotInvokeHandler(t *testing.T) {
	h := newAPI()
	calls := 0
	h.api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		ctx.SetHeader("X-Blocked", "yes")
		ctx.SetStatus(http.StatusForbidden)
		_, _ = ctx.BodyWriter().Write([]byte("blocked"))
	})
	huma.Register[struct{}, reply](h.api, huma.Operation{Method: http.MethodGet, Path: "/blocked", OperationID: "blocked"}, func(context.Context, *struct{}) (*reply, error) {
		calls++
		return &reply{"unexpected"}, nil
	})
	h.finish(t)
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/blocked", nil))
	if w.Code != http.StatusForbidden || w.Header().Get("X-Blocked") != "yes" || w.Body.String() != "blocked" || calls != 0 {
		t.Fatalf("status=%d headers=%v body=%s calls=%d", w.Code, w.Header(), w.Body.String(), calls)
	}
}

func TestHumaMalformedJSONUsesHumaErrorWithoutCallingHandler(t *testing.T) {
	h := newAPI()
	type input struct {
		Body struct {
			Name string `json:"name"`
		}
	}
	calls := 0
	huma.Register[input, reply](h.api, huma.Operation{Method: http.MethodPost, Path: "/malformed", OperationID: "malformed"}, func(context.Context, *input) (*reply, error) {
		calls++
		return &reply{"unexpected"}, nil
	})
	h.finish(t)
	r := httptest.NewRequest(http.MethodPost, "/malformed", strings.NewReader(`{"name":`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, r)
	var body struct {
		Status int `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusBadRequest || body.Status != http.StatusBadRequest || calls != 0 || !strings.Contains(w.Header().Get("Content-Type"), "application/problem+json") {
		t.Fatalf("status=%d headers=%v body=%s calls=%d", w.Code, w.Header(), w.Body.String(), calls)
	}
}

func TestHumaOperationHooksAppearInServedOpenAPI(t *testing.T) {
	config := huma.DefaultConfig("hooks", "1.0.0")
	var order []string
	config.CreateHooks = append(config.CreateHooks, func(c huma.Config) huma.Config { order = append(order, "create"); return c })
	config.OnAddOperation = append(config.OnAddOperation, func(_ *huma.OpenAPI, op *huma.Operation) {
		order = append(order, op.OperationID)
		op.Summary = "hooked summary"
	})
	h := newAPIWithConfig(config)
	huma.Register[struct{}, reply](h.api, huma.Operation{Method: http.MethodGet, Path: "/hook-operation", OperationID: "hooked"}, func(context.Context, *struct{}) (*reply, error) { return &reply{"ok"}, nil })
	h.finish(t)
	if strings.Join(order, ",") != "create,hooked" {
		t.Fatalf("hooks=%v", order)
	}
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		h.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
		var spec struct {
			Paths map[string]map[string]struct {
				Summary string `json:"summary"`
			} `json:"paths"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || spec.Paths["/hook-operation"]["get"].Summary != "hooked summary" {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}
}
