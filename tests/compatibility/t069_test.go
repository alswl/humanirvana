package demo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

func t069OpenAPI(t *testing.T, h http.Handler) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("OpenAPI status=%d body=%q", w.Code, w.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestT069OpenAPIStructuresMatchNativeAndBuildAndHTTP(t *testing.T) {
	for _, group := range []struct {
		name               string
		native, build      http.Handler
		method, path, body string
		status             int
		response           string
	}{{"typed input", t065Native(t), t065Adapted(t), http.MethodPost, "/v1/items/17?tag=red,blue&explode=red&explode=blue&version=0", `{"name":"widget","codes":["a","b"],"extra":{"enabled":true}}`, http.StatusAccepted, `{"$schema":"https://example.com/schemas/T065OutputBody.json","id":17,"name":"widget","tags":["red","blue"],"explode":["red","blue"],"version":3,"locale":"fr","codes":["a","b"],"enabled":true}`},
		{"validation", t066Native(t), t066Build(t), http.MethodPost, "/validate", `{"count":3,"name":"box","kind":"small","email":"a@example.com","children":[{"score":0}]}`, http.StatusOK, `{"$schema":"https://example.com/schemas/T066OutputBody.json","accepted":true}`}} {
		t.Run(group.name, func(t *testing.T) {
			docN, docB := t069OpenAPI(t, group.native), t069OpenAPI(t, group.build)
			if !reflect.DeepEqual(docN, docB) {
				t.Fatalf("OpenAPI documents differ\nnative=%v\nBuild=%v", docN, docB)
			}
			run := func(h http.Handler) (int, http.Header, string) {
				r := httptest.NewRequest(group.method, group.path, strings.NewReader(group.body))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("X-Locale", "fr")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				return w.Code, w.Header().Clone(), w.Body.String()
			}
			codeN, headN, bodyN := run(group.native)
			codeB, headB, bodyB := run(group.build)
			if codeN != group.status || codeB != codeN || bodyN != bodyB || headN.Get("Content-Type") != headB.Get("Content-Type") {
				t.Fatalf("native=(%d,%v,%s) Build=(%d,%v,%s)", codeN, headN, bodyN, codeB, headB, bodyB)
			}
			var got map[string]any
			if err := json.Unmarshal([]byte(bodyN), &got); err != nil {
				t.Fatal(err)
			}
			var want map[string]any
			if err := json.Unmarshal([]byte(group.response), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("HTTP body=%v want=%v", got, want)
			}
		})
	}
	t.Run("output and error schemas agree", func(t *testing.T) {
		docN, docB := t069OpenAPI(t, t067Native(t)), t069OpenAPI(t, t067Build(t))
		if !reflect.DeepEqual(docN, docB) {
			t.Fatalf("T067 structures differ native paths/components=%v/%v Build=%v/%v", docN["paths"], docN["components"], docB["paths"], docB["components"])
		}
		post := docN["paths"].(map[string]any)["/output"].(map[string]any)["post"].(map[string]any)
		resp := post["responses"].(map[string]any)["202"].(map[string]any)
		if resp["headers"].(map[string]any)["X-Result"] == nil || resp["content"] == nil {
			t.Fatalf("declared response lacks body/header schema: %v", resp)
		}
		for name, h := range map[string]http.Handler{"native": t067Native(t), "Build": t067Build(t)} {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/output", strings.NewReader(`{"mode":"fast"}`))
			r.Header.Set("Content-Type", "application/json")
			h.ServeHTTP(w, r)
			if w.Code != http.StatusCreated || w.Header().Get("X-Result") != "created" || w.Body.String() != `{"$schema":"https://example.com/schemas/T067OutputBody.json","mode":"fast"}`+"\n" {
				t.Errorf("%s response status=%d header=%v body=%q", name, w.Code, w.Header(), w.Body.String())
			}
		}
	})
	t.Run("T067 empty/default/typed output documents and wire are paired", func(t *testing.T) {
		docN, docB := t069OpenAPI(t, t067Native(t)), t069OpenAPI(t, t067Build(t))
		if !reflect.DeepEqual(docN, docB) {
			t.Fatalf("T067 full documents differ: native=%v Build=%v", docN, docB)
		}
		paths := docN["paths"].(map[string]any)
		for _, tc := range []struct {
			path, method, status string
			hasBody              bool
		}{
			{"/output", "post", "202", true}, {"/empty", "delete", "204", false}, {"/default", "get", "202", true}, {"/format-output", "get", "200", true},
		} {
			op := paths[tc.path].(map[string]any)[tc.method].(map[string]any)
			resp := op["responses"].(map[string]any)[tc.status].(map[string]any)
			if (resp["content"] != nil) != tc.hasBody {
				t.Errorf("%s %s response %s content=%v", tc.method, tc.path, tc.status, resp["content"])
			}
			if tc.path == "/output" && resp["headers"].(map[string]any)["X-Result"] == nil {
				t.Errorf("output header schema absent: %v", resp)
			}
		}
		for _, tc := range []struct {
			method, path, body string
			status             int
		}{
			{http.MethodDelete, "/empty", "{}", http.StatusNoContent}, {http.MethodGet, "/default", "", http.StatusAccepted}, {http.MethodGet, "/format-output", "", http.StatusOK},
		} {
			codeN, headN, bodyN := t067Call(t067Native(t), tc.method, tc.path, tc.body)
			codeB, headB, bodyB := t067Call(t067Build(t), tc.method, tc.path, tc.body)
			if codeN != tc.status || codeB != codeN || !reflect.DeepEqual(headN, headB) || bodyN != bodyB {
				t.Errorf("%s %s native=(%d,%v,%q) Build=(%d,%v,%q)", tc.method, tc.path, codeN, headN, bodyN, codeB, headB, bodyB)
			}
		}
	})
	t.Run("T068 custom errors are documented identically", func(t *testing.T) {
		docN, docB := t069OpenAPI(t, t068BuildCustomHandler(t, false, nil, nil, "custom-model")), t069OpenAPI(t, t068BuildCustomHandler(t, true, nil, nil, "custom-model"))
		if !reflect.DeepEqual(docN, docB) {
			t.Fatalf("T068 full documents differ: native=%v Build=%v", docN, docB)
		}
		responses := docN["paths"].(map[string]any)["/errors"].(map[string]any)["post"].(map[string]any)["responses"].(map[string]any)
		fallback, ok := responses["default"].(map[string]any)
		if !ok {
			t.Fatalf("expected Huma default error response: %v", responses)
		}
		content := fallback["content"].(map[string]any)
		problem := content["application/problem+json"].(map[string]any)
		if problem["schema"].(map[string]any)["$ref"] != "#/components/schemas/ErrorModel" {
			t.Fatalf("unexpected error schema: %v", problem)
		}
	})
	t.Run("middleware operation remains documented", func(t *testing.T) {
		events := []string{}
		mw := func(ctx huma.Context, next func(huma.Context)) { next(ctx) }
		doc := t069OpenAPI(t, t068BuildHandler(t, true, mw, mw))
		paths := doc["paths"].(map[string]any)
		if paths["/errors"].(map[string]any)["post"].(map[string]any)["operationId"] != "errors" {
			t.Fatalf("middleware changed operation document: %v", paths["/errors"])
		}
		_ = events
	})
}
