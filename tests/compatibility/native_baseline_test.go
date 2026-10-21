package demo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type nativeIn struct {
	ID string `path:"id"`
}
type nativeOut struct {
	Body struct {
		ID string `json:"id"`
	} `json:"body"`
}

func TestNativeHumaGroupMiddlewareHiddenAndSystemRoutes(t *testing.T) {
	router := http.NewServeMux()
	api := humago.New(router, huma.DefaultConfig("Native baseline", "1.0.0"))
	var order []string
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		order = append(order, "api.before")
		next(ctx)
		order = append(order, "api.after")
	})
	group := huma.NewGroup(api, "/v1")
	group.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		order = append(order, "group.before")
		next(ctx)
		order = append(order, "group.after")
	})
	op := huma.Operation{Method: http.MethodGet, Path: "/things/{id}", OperationID: "getThing", Summary: "Get a thing"}
	huma.Register[nativeIn, nativeOut](group, op, func(_ context.Context, in *nativeIn) (*nativeOut, error) {
		order = append(order, "handler")
		out := &nativeOut{}
		out.Body.ID = in.ID
		return out, nil
	})
	huma.Register[nativeIn, nativeOut](group, huma.Operation{Method: http.MethodGet, Path: "/hidden/{id}", OperationID: "hiddenThing", Hidden: true}, func(_ context.Context, in *nativeIn) (*nativeOut, error) {
		out := &nativeOut{}
		out.Body.ID = in.ID
		return out, nil
	})
	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/things/abc")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got["id"] != "abc" {
		t.Fatalf("response body=%s", body)
	}
	if want := []string{"api.before", "group.before", "handler", "group.after", "api.after"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("middleware order=%v want=%v", order, want)
	}

	resp, err = http.Get(server.URL + "/v1/hidden/secret")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("hidden route status=%d", resp.StatusCode)
	}

	resp, err = http.Get(server.URL + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("OpenAPI status=%d", resp.StatusCode)
	}
	var openapi map[string]any
	if err := json.Unmarshal(doc, &openapi); err != nil {
		t.Fatal(err)
	}
	paths := openapi["paths"].(map[string]any)
	if paths["/v1/things/{id}"] == nil || paths["/v1/hidden/{id}"] != nil {
		t.Fatalf("OpenAPI paths=%v", paths)
	}
	path := paths["/v1/things/{id}"].(map[string]any)
	operation := path["get"].(map[string]any)
	parameters := operation["parameters"].([]any)
	if len(parameters) != 1 || parameters[0].(map[string]any)["in"] != "path" {
		t.Fatalf("OpenAPI parameters=%v", parameters)
	}
}

func TestNativeHumaOperationMiddleware(t *testing.T) {
	router := http.NewServeMux()
	api := humago.New(router, huma.DefaultConfig("Native operation middleware", "1.0.0"))
	var order []string
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		order = append(order, "api.before")
		next(ctx)
		order = append(order, "api.after")
	})
	group := huma.NewGroup(api, "/v1")
	group.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		order = append(order, "group.before")
		next(ctx)
		order = append(order, "group.after")
	})
	op := huma.Operation{
		Method: http.MethodGet, Path: "/things/{id}", OperationID: "operationMiddlewareThing",
		Middlewares: huma.Middlewares{func(ctx huma.Context, next func(huma.Context)) {
			order = append(order, "operation.before")
			next(ctx)
			order = append(order, "operation.after")
		}},
	}
	huma.Register[nativeIn, nativeOut](group, op, func(_ context.Context, in *nativeIn) (*nativeOut, error) {
		order = append(order, "handler")
		out := &nativeOut{}
		out.Body.ID = in.ID
		return out, nil
	})
	server := httptest.NewServer(router)
	defer server.Close()
	resp, err := http.Get(server.URL + "/v1/things/op")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if want := []string{"api.before", "group.before", "operation.before", "handler", "operation.after", "group.after", "api.after"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("middleware order=%v want=%v", order, want)
	}
	var doc map[string]any
	resp, err = http.Get(server.URL + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		resp.Body.Close()
		t.Fatal(err)
	}
	resp.Body.Close()
	paths := doc["paths"].(map[string]any)
	if paths["/v1/things/{id}"] == nil {
		t.Fatalf("operation middleware registration missing from OpenAPI paths: %v", paths)
	}
}

func TestNativeHumaRegistrationNegativeCases(t *testing.T) {
	t.Run("empty method panics before route registration", func(t *testing.T) {
		router := http.NewServeMux()
		api := humago.New(router, huma.DefaultConfig("Negative", "1.0.0"))
		before := len(api.OpenAPI().Paths)
		panicked := false
		func() {
			defer func() { panicked = recover() != nil }()
			huma.Register[nativeIn, nativeOut](api, huma.Operation{Path: "/bad"}, func(context.Context, *nativeIn) (*nativeOut, error) { return &nativeOut{}, nil })
		}()
		if !panicked {
			t.Fatal("expected method validation panic")
		}
		if len(api.OpenAPI().Paths) != before {
			t.Fatal("invalid registration mutated OpenAPI paths")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/bad", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("invalid route became servable: %d", w.Code)
		}
	})
}
