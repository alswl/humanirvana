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
	"github.com/danielgtaylor/huma/v2/sse"
)

type capabilitySSEInput struct{}
type capabilitySSEData struct {
	Value string `json:"value"`
}

func registerCapabilitySSE(api huma.API) error {
	sse.Register[capabilitySSEInput](api, huma.Operation{Method: http.MethodGet, Path: "/events", OperationID: "events"}, map[string]any{"update": capabilitySSEData{}}, func(_ context.Context, _ *capabilitySSEInput, send sse.Sender) {
		_ = send(sse.Message{ID: 7, Retry: 1500, Data: capabilitySSEData{Value: "ready"}})
	})
	return nil
}

func capabilitySSEHandler(t *testing.T, build bool) http.Handler {
	t.Helper()
	c := huma.DefaultConfig("SSE", "1.0.0")
	if !build {
		r := http.NewServeMux()
		if err := registerCapabilitySSE(humago.New(r, c)); err != nil {
			t.Fatal(err)
		}
		return r
	}

	result, err := testkit.NewAPIHandler(testkit.Spec{Config: c, Native: []testkit.Registration{{Register: registerCapabilitySSE}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

func TestCapabilitySSERegisterNativeHTTPBaseline(t *testing.T) {
	var docs [2]any
	for i, h := range []http.Handler{capabilitySSEHandler(t, false), capabilitySSEHandler(t, true)} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/events", nil))
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "text/event-stream" {
			t.Fatalf("arm %d status=%d headers=%v", i, w.Code, w.Header())
		}
		want := "id: 7\nretry: 1500\nevent: update\ndata: {\"value\":\"ready\"}\n\n"
		if strings.TrimSpace(w.Body.String()) == "" || !strings.Contains(w.Body.String(), want) {
			t.Fatalf("arm %d SSE body=%q want containing %q", i, w.Body.String(), want)
		}
		d := httptest.NewRecorder()
		h.ServeHTTP(d, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
		if d.Code != http.StatusOK {
			t.Fatalf("OpenAPI status=%d", d.Code)
		}
		if err := json.Unmarshal(d.Body.Bytes(), &docs[i]); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(docs[0], docs[1]) {
		t.Fatal("SSE OpenAPI differs between native and Build")
	}
	content := docs[0].(map[string]any)["paths"].(map[string]any)["/events"].(map[string]any)["get"].(map[string]any)["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)
	if _, ok := content["text/event-stream"]; !ok {
		t.Fatalf("SSE response media missing: %v", content)
	}
}
