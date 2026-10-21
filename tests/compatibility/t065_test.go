package demo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	testkit "github.com/alswl/humanirvana/tests/compatibility/internal/testkit"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type t065Input struct {
	ID      int      `path:"id"`
	Tags    []string `query:"tag"`
	Explode []string `query:"explode,explode"`
	Version int      `query:"version" default:"3"`
	Locale  string   `header:"X-Locale" default:"en"`
	Body    t065Body `json:"body"`
}

type t065Body struct {
	Name  string   `json:"name"`
	Codes []string `json:"codes"`
	Extra struct {
		Enabled bool `json:"enabled"`
	} `json:"extra"`
}

type t065Output struct {
	Body struct {
		ID      int      `json:"id"`
		Name    string   `json:"name"`
		Tags    []string `json:"tags"`
		Explode []string `json:"explode"`
		Version int      `json:"version"`
		Locale  string   `json:"locale"`
		Codes   []string `json:"codes"`
		Enabled bool     `json:"enabled"`
	} `json:"body"`
}

func t065Handler(_ context.Context, in *t065Input) (*t065Output, error) {
	out := &t065Output{}
	out.Body.ID, out.Body.Name, out.Body.Tags = in.ID, in.Body.Name, in.Tags
	out.Body.Explode = in.Explode
	out.Body.Version = in.Version
	out.Body.Locale, out.Body.Codes, out.Body.Enabled = in.Locale, in.Body.Codes, in.Body.Extra.Enabled
	return out, nil
}

func t065Config() huma.Config { return huma.DefaultConfig("T065", "1.0.0") }

func t065Native(t *testing.T) http.Handler {
	t.Helper()
	router := http.NewServeMux()
	api := humago.New(router, t065Config())
	group := huma.NewGroup(api, "/v1")
	huma.Register[t065Input, t065Output](group, huma.Operation{
		Method: http.MethodPost, Path: "/items/{id}", OperationID: "replaceItem",
		Summary: "Replace item", Description: "Typed input registration",
		Tags: []string{"items"}, DefaultStatus: http.StatusAccepted,
		Metadata: map[string]any{"x-owner": "inventory"},
	}, t065Handler)
	return router
}

func t065Adapted(t *testing.T) http.Handler {
	t.Helper()
	result, err := testkit.NewAPIHandler(testkit.Spec{Config: t065Config(), Native: []testkit.Registration{{
		Register: func(api huma.API) error {
			group := huma.NewGroup(api, "/v1")
			huma.Register[t065Input, t065Output](group, huma.Operation{
				Method: http.MethodPost, Path: "/items/{id}", OperationID: "replaceItem",
				Summary: "Replace item", Description: "Typed input registration",
				Tags: []string{"items"}, DefaultStatus: http.StatusAccepted,
				Metadata: map[string]any{"x-owner": "inventory"},
			}, t065Handler)
			return nil
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

func t065Request(t *testing.T, h http.Handler, target string, body string) (int, map[string]any, http.Header) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Locale", "fr")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var got map[string]any
	if w.Body.Len() != 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode %q: %v", w.Body.String(), err)
		}
	}
	return w.Code, got, w.Header().Clone()
}

func TestT065NativeAndBuildTypedInputOperationHTTP(t *testing.T) {
	const body = `{"name":"widget","codes":["a","b"],"extra":{"enabled":true}}`
	path := "/v1/items/17?tag=red,blue&explode=red&explode=blue&version=0"
	codeN, gotN, headN := t065Request(t, t065Native(t), path, body)
	codeA, gotA, headA := t065Request(t, t065Adapted(t), path, body)
	if codeN != http.StatusAccepted || codeA != codeN {
		t.Fatalf("status native=%d adapted=%d", codeN, codeA)
	}
	if !reflect.DeepEqual(gotN, gotA) {
		t.Fatalf("body native=%v adapted=%v", gotN, gotA)
	}
	want := map[string]any{"id": float64(17), "name": "widget", "tags": []any{"red", "blue"}, "explode": []any{"red", "blue"}, "version": float64(3), "locale": "fr", "codes": []any{"a", "b"}, "enabled": true}
	if gotN["id"] != want["id"] || gotN["name"] != want["name"] || !reflect.DeepEqual(gotN["tags"], want["tags"]) || !reflect.DeepEqual(gotN["explode"], want["explode"]) || gotN["version"] != want["version"] || gotN["locale"] != want["locale"] || !reflect.DeepEqual(gotN["codes"], want["codes"]) || gotN["enabled"] != want["enabled"] {
		t.Fatalf("bound response=%v want fields=%v", gotN, want)
	}
	if headN.Get("Content-Type") != headA.Get("Content-Type") {
		t.Fatalf("content type native=%q adapted=%q", headN.Get("Content-Type"), headA.Get("Content-Type"))
	}
}

func TestT065QueryDuplicatesAndDefaultPresenceAreIndependentCases(t *testing.T) {
	for _, tc := range []struct {
		name, query           string
		wantTags, wantExplode []any
		wantVersion           float64
	}{
		{name: "comma default non-exploded", query: "tag=red,blue", wantTags: []any{"red", "blue"}, wantVersion: 3},
		{name: "repeated explode", query: "explode=red&explode=blue", wantExplode: []any{"red", "blue"}, wantVersion: 3},
		{name: "explicit zero is currently overwritten by default (known gap)", query: "version=0", wantVersion: 3},
		{name: "missing uses default", query: "", wantVersion: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := "/v1/items/1"
			if tc.query != "" {
				path += "?" + tc.query
			}
			_, got, _ := t065Request(t, t065Native(t), path, `{"name":"n","codes":[],"extra":{"enabled":false}}`)
			_, adapted, _ := t065Request(t, t065Adapted(t), path, `{"name":"n","codes":[],"extra":{"enabled":false}}`)
			if !reflect.DeepEqual(got, adapted) {
				t.Fatalf("native=%v adapted=%v", got, adapted)
			}
			if got["version"] != tc.wantVersion {
				t.Fatalf("version=%v want=%v", got["version"], tc.wantVersion)
			}
			if tc.wantTags != nil && !reflect.DeepEqual(got["tags"], tc.wantTags) {
				t.Fatalf("tags=%v", got["tags"])
			}
			if tc.wantExplode != nil && !reflect.DeepEqual(got["explode"], tc.wantExplode) {
				t.Fatalf("explode=%v", got["explode"])
			}
		})
	}
}

func TestT065OpenAPIAndNativeRegistration(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.Handler
	}{{"native", t065Native(t)}, {"Build", t065Adapted(t)}} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("openapi status=%d", w.Code)
			}
			var doc map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
			paths := doc["paths"].(map[string]any)
			op := paths["/v1/items/{id}"].(map[string]any)["post"].(map[string]any)
			if op["operationId"] != "replaceItem" || op["summary"] != "Replace item" || op["description"] != "Typed input registration" {
				t.Fatalf("operation metadata=%v", op)
			}
			if op["x-owner"] != nil {
				t.Fatalf("Huma Operation.Metadata was unexpectedly serialized: %v", op["x-owner"])
			}
			if op["responses"].(map[string]any)["202"] == nil {
				t.Fatalf("DefaultStatus response missing: %v", op["responses"])
			}
			params := op["parameters"].([]any)
			if len(params) != 5 {
				t.Fatalf("parameters=%v", params)
			}
			if op["requestBody"] == nil {
				t.Fatalf("requestBody missing: %v", op)
			}
		})
	}
	assertT065PointerParameterRejected(t)
}

func assertT065PointerParameterRejected(t *testing.T) {
	t.Helper()
	type pointerInput struct {
		Version *int `query:"version"`
	}
	type pointerOutput struct {
		Body struct {
			OK bool `json:"ok"`
		} `json:"body"`
	}
	register := func(api huma.API) error {
		huma.Register[pointerInput, pointerOutput](api, huma.Operation{Method: http.MethodGet, Path: "/pointer", OperationID: "pointer"}, func(context.Context, *pointerInput) (*pointerOutput, error) { return &pointerOutput{}, nil })
		return nil
	}
	// Locked Huma rejects this during registration (huma.go:155-160).
	api := huma.NewAPI(t065Config(), humago.NewAdapter(http.NewServeMux(), ""))
	panicked := false
	func() { defer func() { panicked = recover() != nil }(); _ = register(api) }()
	if !panicked {
		t.Fatal("native Huma accepted pointer query parameter")
	}
}
