package demo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
)

type capabilityHumatestInput struct {
	Body struct {
		Value string `json:"value"`
	} `json:"body" required:"false"`
}
type capabilityHumatestOutput struct {
	Body struct {
		Method string `json:"method"`
		Value  string `json:"value"`
		Marker string `json:"marker"`
		Header string `json:"header"`
	} `json:"body"`
}

func TestCapabilityHumatestPublicRequestHelpers(t *testing.T) {
	h, api := humatest.New(t)
	_ = h
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		method := method
		huma.Register[capabilityHumatestInput, capabilityHumatestOutput](api, huma.Operation{Method: method, Path: "/" + strings.ToLower(method), OperationID: "test-" + strings.ToLower(method)}, func(ctx context.Context, in *capabilityHumatestInput) (*capabilityHumatestOutput, error) {
			out := &capabilityHumatestOutput{}
			out.Body.Method = method
			out.Body.Value = in.Body.Value
			out.Body.Marker, _ = ctx.Value("marker").(string)
			out.Body.Header = ""
			return out, nil
		})
	}
	cases := []struct {
		name         string
		call         func() *httptest.ResponseRecorder
		method, want string
	}{
		{"Do", func() *httptest.ResponseRecorder {
			return api.Do(http.MethodGet, "/get", "X-Probe: one", map[string]string{"value": "body"})
		}, http.MethodGet, ""},
		{"DoCtx", func() *httptest.ResponseRecorder {
			return api.DoCtx(context.WithValue(context.Background(), "marker", "ctx"), http.MethodGet, "/get", map[string]string{"value": "body"})
		}, http.MethodGet, "ctx"},
		{"Get", func() *httptest.ResponseRecorder { return api.Get("/get", map[string]string{"value": "body"}) }, http.MethodGet, ""},
		{"GetCtx", func() *httptest.ResponseRecorder {
			return api.GetCtx(context.WithValue(context.Background(), "marker", "ctx"), "/get", map[string]string{"value": "body"})
		}, http.MethodGet, "ctx"},
		{"Post", func() *httptest.ResponseRecorder { return api.Post("/post", map[string]string{"value": "json"}) }, http.MethodPost, ""},
		{"PostCtx", func() *httptest.ResponseRecorder {
			return api.PostCtx(context.WithValue(context.Background(), "marker", "ctx"), "/post", strings.NewReader(`{"value":"reader"}`), "Content-Type: application/json")
		}, http.MethodPost, "ctx"},
		{"Put", func() *httptest.ResponseRecorder { return api.Put("/put", map[string]string{"value": "typed"}) }, http.MethodPut, ""},
		{"PutCtx", func() *httptest.ResponseRecorder {
			return api.PutCtx(context.WithValue(context.Background(), "marker", "ctx"), "/put", map[string]string{"value": "body"})
		}, http.MethodPut, "ctx"},
		{"Patch", func() *httptest.ResponseRecorder {
			return api.Patch("/patch", "X-Probe: patch", map[string]string{"value": "body"})
		}, http.MethodPatch, ""},
		{"PatchCtx", func() *httptest.ResponseRecorder {
			return api.PatchCtx(context.WithValue(context.Background(), "marker", "ctx"), "/patch", map[string]string{"value": "body"})
		}, http.MethodPatch, "ctx"},
		{"Delete", func() *httptest.ResponseRecorder { return api.Delete("/delete", map[string]string{"value": "body"}) }, http.MethodDelete, ""},
		{"DeleteCtx", func() *httptest.ResponseRecorder {
			return api.DeleteCtx(context.WithValue(context.Background(), "marker", "ctx"), "/delete", map[string]string{"value": "body"})
		}, http.MethodDelete, "ctx"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.call()
			if r.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", r.Code, r.Body.String())
			}
			var got map[string]any
			if err := json.Unmarshal(r.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got["method"] != tc.method || got["marker"] != tc.want {
				t.Fatalf("top-level response=%v", got)
			}
			if _, exists := got["$schema"]; exists {
				t.Fatalf("humatest default config unexpectedly emitted $schema: %v", got["$schema"])
			}
			if len(got) != 4 {
				t.Fatalf("unexpected top-level response fields=%v", got)
			}
		})
	}
	defer func() {
		if got := recover(); got != "unsupported argument type, expected string header or io.Reader/slice/map/struct body" {
			t.Fatalf("unsupported arg panic=%v", got)
		}
	}()
	api.Get("/get", 42)
}

func TestCapabilityHumatestDumpHelpersPreserveBodies(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/dump", strings.NewReader(`{"a":1}`))
	r.Header.Set("Content-Type", "application/json")
	dump, err := humatest.DumpRequest(r)
	if err != nil || !strings.Contains(string(dump), "\"a\": 1") {
		t.Fatalf("DumpRequest=%q err=%v", dump, err)
	}
	b, err := io.ReadAll(r.Body)
	if err != nil || string(b) != `{"a":1}` {
		t.Fatalf("DumpRequest did not restore body: %q err=%v", b, err)
	}
	resp := httptest.NewRecorder()
	resp.Header().Set("Content-Type", "application/json")
	resp.Write([]byte(`{"b":2}`))
	dump, err = humatest.DumpResponse(resp.Result())
	if err != nil || !strings.Contains(string(dump), "\"b\": 2") {
		t.Fatalf("DumpResponse=%q err=%v", dump, err)
	}
}
