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

func registerCapabilityStream(api huma.API) error {
	huma.Register[struct{}, huma.StreamResponse](api, huma.Operation{Method: http.MethodGet, Path: "/stream", OperationID: "stream"}, func(context.Context, *struct{}) (*huma.StreamResponse, error) {
		return &huma.StreamResponse{Body: func(ctx huma.Context) {
			w := ctx.BodyWriter()
			_, _ = w.Write([]byte("first-"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			_, _ = w.Write([]byte("second"))
		}}, nil
	})
	return nil
}

func capabilityStreamHandler(t *testing.T, build bool) http.Handler {
	t.Helper()
	c := huma.DefaultConfig("Stream", "1.0.0")
	if !build {
		r := http.NewServeMux()
		if err := registerCapabilityStream(humago.New(r, c)); err != nil {
			t.Fatal(err)
		}
		return r
	}

	result, err := testkit.NewAPIHandler(testkit.Spec{Config: c, Native: []testkit.Registration{{Register: registerCapabilityStream}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

func TestCapabilityStreamResponseNativeHTTPBaseline(t *testing.T) {
	for _, build := range []bool{false, true} {
		h := capabilityStreamHandler(t, build)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/stream", nil))
		if w.Code != http.StatusOK || w.Body.String() != "first-second" || !w.Flushed {
			t.Fatalf("Build=%v status=%d flushed=%v body=%q", build, w.Code, w.Flushed, w.Body.String())
		}
	}
	t.Run("OpenAPI has response but no invented body schema", func(t *testing.T) {
		for _, build := range []bool{false, true} {
			w := httptest.NewRecorder()
			capabilityStreamHandler(t, build).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
			var doc map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
			response := doc["paths"].(map[string]any)["/stream"].(map[string]any)["get"].(map[string]any)["responses"].(map[string]any)["200"].(map[string]any)
			if response["content"] != nil {
				t.Fatalf("Build=%v stream response unexpectedly documents content: %v", build, response["content"])
			}
		}
	})
}
