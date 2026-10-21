package demo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	testkit "github.com/alswl/humanirvana/tests/compatibility/internal/testkit"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type capabilityCookieInput struct {
	Session string `cookie:"session" required:"true"`
}

type capabilityCookieOutput struct {
	Body struct {
		Session string `json:"session"`
	} `json:"body"`
}

func registerCapabilityCookie(api huma.API) error {
	huma.Register[capabilityCookieInput, capabilityCookieOutput](api, huma.Operation{Method: http.MethodGet, Path: "/cookie", OperationID: "cookie"}, func(_ context.Context, in *capabilityCookieInput) (*capabilityCookieOutput, error) {
		out := &capabilityCookieOutput{}
		out.Body.Session = in.Session
		return out, nil
	})
	return nil
}

func capabilityCookieHandler(t *testing.T, build bool) http.Handler {
	t.Helper()
	c := huma.DefaultConfig("Cookie", "1.0.0")
	if !build {
		r := http.NewServeMux()
		if err := registerCapabilityCookie(humago.New(r, c)); err != nil {
			t.Fatal(err)
		}
		return r
	}

	result, err := testkit.NewAPIHandler(testkit.Spec{Config: c, Native: []testkit.Registration{{Register: registerCapabilityCookie}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

func TestCapabilityCookieParameterNativeHTTPBaseline(t *testing.T) {
	for _, tc := range []struct {
		name, cookie string
		status       int
		want         string
	}{
		{"present", "session=abc", http.StatusOK, "abc"},
		{"missing required", "", http.StatusUnprocessableEntity, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body [2]string
			var status [2]int
			for i, h := range []http.Handler{capabilityCookieHandler(t, false), capabilityCookieHandler(t, true)} {
				r := httptest.NewRequest(http.MethodGet, "/cookie", nil)
				if tc.cookie != "" {
					r.Header.Set("Cookie", tc.cookie)
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				status[i], body[i] = w.Code, w.Body.String()
			}
			if status[0] != tc.status || status[1] != status[0] || body[1] != body[0] {
				t.Fatalf("status=%v body=%v want=%d", status, body, tc.status)
			}
			if tc.status == http.StatusOK {
				var decoded map[string]any
				if err := json.Unmarshal([]byte(body[0]), &decoded); err != nil {
					t.Fatal(err)
				}
				if decoded["session"] != tc.want {
					t.Fatalf("cookie response=%v", decoded)
				}
			}
		})
	}
	t.Run("OpenAPI parameter location", func(t *testing.T) {
		for _, build := range []bool{false, true} {
			w := httptest.NewRecorder()
			capabilityCookieHandler(t, build).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
			var doc map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
			op := doc["paths"].(map[string]any)["/cookie"].(map[string]any)["get"].(map[string]any)
			params, _ := op["parameters"].([]any)
			found := false
			for _, raw := range params {
				param := raw.(map[string]any)
				if param["in"] == "cookie" {
					found = param["name"] == "session" && param["required"] == true
				}
			}
			if !found {
				t.Fatalf("cookie parameter is not documented as required: %v", params)
			}
		}
	})
}
