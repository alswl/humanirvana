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

type t066Input struct {
	Body struct {
		Count    int    `json:"count" minimum:"1" maximum:"5"`
		Name     string `json:"name" minLength:"2" maxLength:"5"`
		Kind     string `json:"kind" enum:"small,large"`
		Email    string `json:"email" format:"email"`
		Children []struct {
			Score int `json:"score" minimum:"0"`
		} `json:"children"`
	} `json:"body"`
}
type t066Output struct {
	Body struct {
		Accepted bool `json:"accepted"`
	} `json:"body"`
}

func t066Handler(_ context.Context, in *t066Input) (*t066Output, error) {
	out := &t066Output{}
	out.Body.Accepted = true
	return out, nil
}
func t066Config() huma.Config { return huma.DefaultConfig("T066 validation", "1.0.0") }
func t066Native(t *testing.T) http.Handler {
	t.Helper()
	router := http.NewServeMux()
	api := humago.New(router, t066Config())
	huma.Register[t066Input, t066Output](api, huma.Operation{Method: http.MethodPost, Path: "/validate", OperationID: "validate"}, t066Handler)
	return router
}
func t066Build(t *testing.T) http.Handler {
	t.Helper()
	result, err := testkit.NewAPIHandler(testkit.Spec{Config: t066Config(), Native: []testkit.Registration{{Register: func(api huma.API) error {
		huma.Register[t066Input, t066Output](api, huma.Operation{Method: http.MethodPost, Path: "/validate", OperationID: "validate"}, t066Handler)
		return nil
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

func t066Post(h http.Handler, body string) (int, map[string]any, http.Header) {
	r := httptest.NewRequest(http.MethodPost, "/validate", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	got := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	return w.Code, got, w.Header().Clone()
}

func TestT066ValidationNativeAndBuildHTTPCases(t *testing.T) {
	valid := `{"count":3,"name":"box","kind":"small","email":"a@example.com","children":[{"score":0},{"score":4}]}`
	cases := []struct {
		id, body string
		status   int
		accepted bool
	}{
		{"valid inclusively bounded nested value", valid, http.StatusOK, true},
		{"required count absent", `{"name":"box","kind":"small","email":"a@example.com","children":[]}`, http.StatusUnprocessableEntity, false},
		{"number below minimum", `{"count":0,"name":"box","kind":"small","email":"a@example.com","children":[]}`, http.StatusUnprocessableEntity, false},
		{"number above maximum", `{"count":6,"name":"box","kind":"small","email":"a@example.com","children":[]}`, http.StatusUnprocessableEntity, false},
		{"string below min length", `{"count":3,"name":"x","kind":"small","email":"a@example.com","children":[]}`, http.StatusUnprocessableEntity, false},
		{"string above max length", `{"count":3,"name":"toolbox","kind":"small","email":"a@example.com","children":[]}`, http.StatusUnprocessableEntity, false},
		{"enum rejected", `{"count":3,"name":"box","kind":"medium","email":"a@example.com","children":[]}`, http.StatusUnprocessableEntity, false},
		{"format rejected", `{"count":3,"name":"box","kind":"small","email":"bad","children":[]}`, http.StatusUnprocessableEntity, false},
		{"nested array item rejected", `{"count":3,"name":"box","kind":"small","email":"a@example.com","children":[{"score":-1}]}`, http.StatusUnprocessableEntity, false},
		{"boundary minimum/length pass", `{"count":1,"name":"ab","kind":"large","email":"b@example.org","children":[{"score":0}]}`, http.StatusOK, true},
		{"boundary maximum/length pass", `{"count":5,"name":"abcde","kind":"small","email":"c@example.net","children":[{"score":5}]}`, http.StatusOK, true},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			codeN, bodyN, headN := t066Post(t066Native(t), tc.body)
			codeB, bodyB, headB := t066Post(t066Build(t), tc.body)
			if codeN != tc.status || codeB != codeN {
				t.Fatalf("status native=%d Build=%d want=%d bodies=%v/%v", codeN, codeB, tc.status, bodyN, bodyB)
			}
			if tc.accepted && bodyN["accepted"] != true {
				t.Fatalf("response=%v", bodyN)
			}
			if !reflect.DeepEqual(bodyN, bodyB) {
				t.Fatalf("native body=%v Build body=%v", bodyN, bodyB)
			}
			if headN.Get("Content-Type") != headB.Get("Content-Type") {
				t.Fatalf("content type native=%q Build=%q", headN.Get("Content-Type"), headB.Get("Content-Type"))
			}
		})
	}
}

func TestT066ValidationOpenAPIConstraintsMatchRuntimeInputs(t *testing.T) {
	for _, entry := range []struct {
		name    string
		handler http.Handler
	}{{"native", t066Native(t)}, {"Build", t066Build(t)}} {
		t.Run(entry.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			entry.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
			if w.Code != 200 {
				t.Fatalf("status=%d", w.Code)
			}
			var doc map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
			paths := doc["paths"].(map[string]any)
			op := paths["/validate"].(map[string]any)["post"].(map[string]any)
			request := op["requestBody"].(map[string]any)
			content := request["content"].(map[string]any)
			ref := content["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"].(string)
			name := strings.TrimPrefix(ref, "#/components/schemas/")
			schema := doc["components"].(map[string]any)["schemas"].(map[string]any)[name].(map[string]any)
			props := schema["properties"].(map[string]any)
			if props["count"].(map[string]any)["minimum"] != float64(1) || props["count"].(map[string]any)["maximum"] != float64(5) || props["name"].(map[string]any)["minLength"] != float64(2) || props["name"].(map[string]any)["maxLength"] != float64(5) {
				t.Fatalf("constraints missing: %v", props)
			}
		})
	}
}
