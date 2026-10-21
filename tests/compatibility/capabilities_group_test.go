package demo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	testkit "github.com/alswl/humanirvana/tests/compatibility/internal/testkit"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type capabilityGroupOutput struct {
	Body struct {
		Path string `json:"path"`
	} `json:"body"`
}

func registerCapabilityGroup(api huma.API) error {
	group := huma.NewGroup(api, "/v1", "/v2")
	group.UseSimpleModifier(func(op *huma.Operation) {
		op.Summary = "grouped operation"
	})
	huma.Get(group, "/items", func(context.Context, *struct{}) (*capabilityGroupOutput, error) {
		out := &capabilityGroupOutput{}
		out.Body.Path = "routed"
		return out, nil
	})
	return nil
}

func capabilityGroupHandler(t *testing.T, build bool) http.Handler {
	t.Helper()
	c := huma.DefaultConfig("Group", "1.0.0")
	if !build {
		r := http.NewServeMux()
		if err := registerCapabilityGroup(humago.New(r, c)); err != nil {
			t.Fatal(err)
		}
		return r
	}

	result, err := testkit.NewAPIHandler(testkit.Spec{Config: c, Native: []testkit.Registration{{Register: registerCapabilityGroup}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

func TestCapabilityGroupPrefixFanoutAndModifierNativeHTTPBaseline(t *testing.T) {
	for _, build := range []bool{false, true} {
		h := capabilityGroupHandler(t, build)
		for _, prefix := range []string{"/v1", "/v2"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, prefix+"/items", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("Build=%v path=%s status=%d body=%s", build, prefix, w.Code, w.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["path"] != "routed" {
				t.Fatalf("Build=%v path=%s response=%v", build, prefix, body)
			}
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
		var doc map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		paths := doc["paths"].(map[string]any)
		var ids []string
		for _, prefix := range []string{"/v1", "/v2"} {
			op := paths[prefix+"/items"].(map[string]any)["get"].(map[string]any)
			ids = append(ids, op["operationId"].(string))
			if op["summary"] != "grouped operation" {
				t.Fatalf("Build=%v path=%s operation=%v", build, prefix, op)
			}
		}
		if !reflect.DeepEqual(ids, []string{"v1-get-items", "v2-get-items"}) {
			t.Fatalf("Build=%v operation IDs=%v", build, ids)
		}
	}
}
