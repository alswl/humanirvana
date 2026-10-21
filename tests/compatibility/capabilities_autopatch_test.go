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
	"github.com/danielgtaylor/huma/v2/autopatch"
)

type capabilityPatchInput struct {
	Body struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	} `json:"body"`
}
type capabilityPatchOutput struct {
	Body struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	} `json:"body"`
}

func registerCapabilityAutoPatch(api huma.API) error {
	state := capabilityPatchOutput{}
	state.Body.Name = "before"
	state.Body.Count = 1
	huma.Register[struct{}, capabilityPatchOutput](api, huma.Operation{Method: http.MethodGet, Path: "/resource", OperationID: "get-resource"}, func(context.Context, *struct{}) (*capabilityPatchOutput, error) { out := state; return &out, nil })
	huma.Register[capabilityPatchInput, struct{}](api, huma.Operation{Method: http.MethodPut, Path: "/resource", OperationID: "put-resource"}, func(_ context.Context, in *capabilityPatchInput) (*struct{}, error) {
		state.Body.Name = in.Body.Name
		state.Body.Count = in.Body.Count
		return &struct{}{}, nil
	})
	autopatch.AutoPatch(api)
	return nil
}

func capabilityAutoPatchHandler(t *testing.T, build bool) http.Handler {
	t.Helper()
	c := huma.DefaultConfig("AutoPatch", "1.0.0")
	if !build {
		r := http.NewServeMux()
		if err := registerCapabilityAutoPatch(humago.New(r, c)); err != nil {
			t.Fatal(err)
		}
		return r
	}

	result, err := testkit.NewAPIHandler(testkit.Spec{Config: c, Native: []testkit.Registration{{Register: registerCapabilityAutoPatch}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

func TestCapabilityAutoPatchNativeHTTPBaseline(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		want                    int
		wantName                string
	}{
		{"merge patch", "application/merge-patch+json", `{"name":"after"}`, http.StatusNoContent, ""},
		{"invalid media", "application/xml", `<x/>`, http.StatusUnsupportedMediaType, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got [2]*httptest.ResponseRecorder
			for i, h := range []http.Handler{capabilityAutoPatchHandler(t, false), capabilityAutoPatchHandler(t, true)} {
				r := httptest.NewRequest(http.MethodPatch, "/resource", strings.NewReader(tc.body))
				r.Header.Set("Content-Type", tc.contentType)
				got[i] = httptest.NewRecorder()
				h.ServeHTTP(got[i], r)
			}
			if got[0].Code != tc.want || got[1].Code != tc.want || got[0].Header().Get("Content-Type") != got[1].Header().Get("Content-Type") || got[0].Body.String() != got[1].Body.String() {
				t.Fatalf("native=(%d,%v,%q) Build=(%d,%v,%q)", got[0].Code, got[0].Header(), got[0].Body.String(), got[1].Code, got[1].Header(), got[1].Body.String())
			}
		})
	}
	var docs [2]any
	for i, h := range []http.Handler{capabilityAutoPatchHandler(t, false), capabilityAutoPatchHandler(t, true)} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("OpenAPI status=%d", w.Code)
		}
		if err := json.Unmarshal(w.Body.Bytes(), &docs[i]); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(docs[0], docs[1]) {
		t.Fatal("AutoPatch OpenAPI differs")
	}
	op := docs[0].(map[string]any)["paths"].(map[string]any)["/resource"].(map[string]any)
	if _, ok := op["patch"]; !ok {
		t.Fatalf("PATCH operation not documented: %v", op)
	}
}
