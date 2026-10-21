package demo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	testkit "github.com/alswl/humanirvana/tests/compatibility/internal/testkit"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type capabilityRawInput struct {
	RawBody []byte `contentType:"application/x-raw"`
}

type capabilityRawOutput struct {
	Body struct {
		Bytes int `json:"bytes"`
	} `json:"body"`
}

func registerCapabilityRaw(api huma.API) error {
	huma.Register[capabilityRawInput, capabilityRawOutput](api, huma.Operation{Method: http.MethodPut, Path: "/raw", OperationID: "raw", MaxBodyBytes: 8}, func(_ context.Context, in *capabilityRawInput) (*capabilityRawOutput, error) {
		out := &capabilityRawOutput{}
		out.Body.Bytes = len(in.RawBody)
		return out, nil
	})
	return nil
}

func capabilityRawHandler(t *testing.T, build bool) http.Handler {
	t.Helper()
	c := huma.DefaultConfig("Raw body", "1.0.0")
	if !build {
		r := http.NewServeMux()
		if err := registerCapabilityRaw(humago.New(r, c)); err != nil {
			t.Fatal(err)
		}
		return r
	}

	result, err := testkit.NewAPIHandler(testkit.Spec{Config: c, Native: []testkit.Registration{{Register: registerCapabilityRaw}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

func TestCapabilityRawBodyNativeBaselineAndNirvanaMediaLimit(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		wantBytes  int
	}{
		{"below body size limit", "1234567", http.StatusOK, 7},
		{"equal body size limit is rejected", "12345678", http.StatusRequestEntityTooLarge, 0},
		{"above body size limit is rejected", "123456789", http.StatusRequestEntityTooLarge, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type response struct {
				code int
				head http.Header
				body string
			}
			var got [2]response
			for i, h := range []http.Handler{capabilityRawHandler(t, false), capabilityRawHandler(t, true)} {
				r := httptest.NewRequest(http.MethodPut, "/raw", strings.NewReader(tc.body))
				r.Header.Set("Content-Type", "application/x-raw")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				got[i] = response{w.Code, w.Header().Clone(), w.Body.String()}
			}
			if got[0].code != tc.status {
				t.Fatalf("native=%+v want status=%d", got[0], tc.status)
			}
			if got[1].code != http.StatusUnsupportedMediaType || !strings.Contains(got[1].body, "Nirvana:Service:NoExecutorForContentType") {
				t.Fatalf("unregistered raw media must fail Nirvana admission: %+v", got[1])
			}
			if tc.status == http.StatusOK {
				var body struct {
					Bytes int `json:"bytes"`
				}
				if err := json.Unmarshal([]byte(got[0].body), &body); err != nil {
					t.Fatal(err)
				}
				if body.Bytes != tc.wantBytes {
					t.Fatalf("raw byte count=%d want=%d", body.Bytes, tc.wantBytes)
				}
			}
		})
	}
	t.Run("openapi declares raw media as binary", func(t *testing.T) {
		for i, h := range []http.Handler{capabilityRawHandler(t, false), capabilityRawHandler(t, true)} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("arm=%d OpenAPI status=%d", i, w.Code)
			}
			var doc map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
			requestBody := doc["paths"].(map[string]any)["/raw"].(map[string]any)["put"].(map[string]any)["requestBody"].(map[string]any)
			content := requestBody["content"].(map[string]any)
			schema := content["application/x-raw"].(map[string]any)["schema"].(map[string]any)
			if schema["format"] != "binary" {
				t.Fatalf("arm=%d raw request schema=%v", i, schema)
			}
		}
	})
}
