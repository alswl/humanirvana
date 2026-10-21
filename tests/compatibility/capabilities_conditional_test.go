package demo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	testkit "github.com/alswl/humanirvana/tests/compatibility/internal/testkit"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/danielgtaylor/huma/v2/conditional"
)

type capabilityConditionalInput struct{ conditional.Params }
type capabilityConditionalOutput struct {
	Body struct {
		Conditional bool `json:"conditional"`
	} `json:"body"`
}

func registerCapabilityConditional(api huma.API) error {
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		method := method
		huma.Register[capabilityConditionalInput, capabilityConditionalOutput](api, huma.Operation{Method: method, Path: "/conditional", OperationID: "conditional-" + method}, func(_ context.Context, in *capabilityConditionalInput) (*capabilityConditionalOutput, error) {
			if !in.HasConditionalParams() {
				return &capabilityConditionalOutput{}, nil
			}
			if err := in.PreconditionFailed("v1", time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)); err != nil {
				return nil, err
			}
			out := &capabilityConditionalOutput{}
			out.Body.Conditional = true
			return out, nil
		})
	}
	return nil
}

func capabilityConditionalHandler(t *testing.T, build bool) http.Handler {
	t.Helper()
	c := huma.DefaultConfig("Conditional", "1.0.0")
	if !build {
		r := http.NewServeMux()
		if err := registerCapabilityConditional(humago.New(r, c)); err != nil {
			t.Fatal(err)
		}
		return r
	}

	result, err := testkit.NewAPIHandler(testkit.Spec{Config: c, Native: []testkit.Registration{{Register: registerCapabilityConditional}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

func TestCapabilityConditionalParamsNativeHTTPBaseline(t *testing.T) {
	for _, tc := range []struct {
		name, method, ifMatch, ifNone string
		want                          int
		wantDetails                   int
	}{
		{"no condition", http.MethodGet, "", "", http.StatusOK, 0},
		{"matching If-None-Match read", http.MethodGet, "", "\"v1\"", http.StatusNotModified, 0},
		{"nonmatching If-Match read", http.MethodGet, "\"other\"", "", http.StatusNotModified, 0},
		{"matching If-Match", http.MethodGet, "W/\"v1\"", "", http.StatusOK, 0},
		{"write failed precondition", http.MethodPut, "\"other\"", "", http.StatusPreconditionFailed, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got [2]*httptest.ResponseRecorder
			for i, h := range []http.Handler{capabilityConditionalHandler(t, false), capabilityConditionalHandler(t, true)} {
				r := httptest.NewRequest(tc.method, "/conditional", nil)
				if tc.ifMatch != "" {
					r.Header.Set("If-Match", tc.ifMatch)
				}
				if tc.ifNone != "" {
					r.Header.Set("If-None-Match", tc.ifNone)
				}
				got[i] = httptest.NewRecorder()
				h.ServeHTTP(got[i], r)
			}
			if got[0].Code != tc.want || got[1].Code != tc.want || got[0].Header().Get("Content-Type") != got[1].Header().Get("Content-Type") || got[0].Body.String() != got[1].Body.String() {
				t.Fatalf("native=(%d,%v,%s) Build=(%d,%v,%s)", got[0].Code, got[0].Header(), got[0].Body.String(), got[1].Code, got[1].Header(), got[1].Body.String())
			}
			if tc.wantDetails > 0 {
				var body huma.ErrorModel
				if err := json.Unmarshal(got[0].Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if len(body.Errors) != tc.wantDetails || body.Errors[0].Location != "headers.If-Match" {
					t.Fatalf("precondition details=%+v", body.Errors)
				}
			}
		})
	}
	var docs [2]any
	for i, h := range []http.Handler{capabilityConditionalHandler(t, false), capabilityConditionalHandler(t, true)} {
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
		t.Fatal("conditional OpenAPI differs")
	}
}
