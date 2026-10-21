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
	"github.com/danielgtaylor/huma/v2/casing"
)

type capabilityCasingInput struct{}
type capabilityCasingOutput struct {
	Body struct {
		Split      string `json:"split"`
		Join       string `json:"join"`
		Camel      string `json:"camel"`
		LowerCamel string `json:"lower_camel"`
		Snake      string `json:"snake"`
		Kebab      string `json:"kebab"`
	} `json:"body"`
}

func registerCapabilityCasing(api huma.API) error {
	huma.Register[capabilityCasingInput, capabilityCasingOutput](api, huma.Operation{Method: http.MethodGet, Path: "/casing", OperationID: "casing"}, func(context.Context, *capabilityCasingInput) (*capabilityCasingOutput, error) {
		out := &capabilityCasingOutput{}
		out.Body.Split = jsonString(casing.Split("HTTPServer2020_ID"))
		out.Body.Join = casing.Join([]string{"http", "server", "2020", "id"}, "-", casing.Initialism)
		out.Body.Camel = casing.Camel("HTTPServer_2020_ID", casing.Initialism)
		out.Body.LowerCamel = casing.LowerCamel("HTTPServer_2020_ID", casing.Initialism)
		out.Body.Snake = casing.Snake("HTTPServer_2020_ID", casing.Initialism)
		out.Body.Kebab = casing.Kebab("HTTPServer_2020_ID", casing.Initialism)
		return out, nil
	})
	return nil
}

func jsonString(v any) string { b, _ := json.Marshal(v); return string(b) }

func capabilityCasingHandler(t *testing.T, build bool) http.Handler {
	t.Helper()
	c := huma.DefaultConfig("Casing", "1.0.0")
	if !build {
		r := http.NewServeMux()
		if err := registerCapabilityCasing(humago.New(r, c)); err != nil {
			t.Fatal(err)
		}
		return r
	}

	result, err := testkit.NewAPIHandler(testkit.Spec{Config: c, Native: []testkit.Registration{{Register: registerCapabilityCasing}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

func TestCapabilityCasingNativeHTTPBaseline(t *testing.T) {
	var results [2]map[string]any
	var docs [2]any
	for i, h := range []http.Handler{capabilityCasingHandler(t, false), capabilityCasingHandler(t, true)} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/casing", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("arm %d status=%d body=%s", i, w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &results[i]); err != nil {
			t.Fatal(err)
		}
		docs[i] = captureCapabilityContextOpenAPI(t, h)
	}
	if !reflect.DeepEqual(results[0], results[1]) {
		t.Fatalf("native=%v Build=%v", results[0], results[1])
	}
	if !reflect.DeepEqual(docs[0], docs[1]) {
		t.Fatal("casing OpenAPI differs")
	}
	want := map[string]any{"split": "[\"HTTP\",\"Server\",\"2020\",\"ID\"]", "join": "HTTP-server-2020-ID", "camel": "HTTPServer2020ID", "lower_camel": "hTTPServer2020ID", "snake": "HTTP_Server2020_ID", "kebab": "HTTP-Server2020-ID"}
	for k, v := range want {
		if results[0][k] != v {
			t.Errorf("%s=%v want %v", k, results[0][k], v)
		}
	}
	if _, ok := results[0]["$schema"]; ok {
		delete(results[0], "$schema")
	}
	if len(results[0]) != len(want) {
		t.Fatalf("unexpected top-level response fields %v", results[0])
	}
}
