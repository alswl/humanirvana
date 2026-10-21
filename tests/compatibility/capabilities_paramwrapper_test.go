package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	testkit "github.com/alswl/humanirvana/tests/compatibility/internal/testkit"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type capabilityOptionalInt struct {
	Value  int
	IsSet  bool
	Parsed any
}

func (o *capabilityOptionalInt) Receiver() reflect.Value { return reflect.ValueOf(o).Elem().Field(0) }
func (o capabilityOptionalInt) Schema(r huma.Registry) *huma.Schema {
	return huma.SchemaFromType(r, reflect.TypeOf(o.Value))
}
func (o *capabilityOptionalInt) OnParamSet(isSet bool, parsed any) {
	o.IsSet = isSet
	o.Parsed = parsed
}

type capabilityParamInput struct {
	Wrapped capabilityOptionalInt `query:"wrapped"`
}
type capabilityParamOutput struct {
	Body struct {
		Value  int    `json:"value"`
		IsSet  bool   `json:"is_set"`
		Parsed string `json:"parsed"`
	} `json:"body"`
}

func registerCapabilityParam(api huma.API) error {
	huma.Register[capabilityParamInput, capabilityParamOutput](api, huma.Operation{Method: http.MethodGet, Path: "/param-wrapper", OperationID: "param-wrapper"}, func(_ context.Context, in *capabilityParamInput) (*capabilityParamOutput, error) {
		out := &capabilityParamOutput{}
		out.Body.Value = in.Wrapped.Value
		out.Body.IsSet = in.Wrapped.IsSet
		if in.Wrapped.Parsed != nil {
			out.Body.Parsed = fmt.Sprint(in.Wrapped.Parsed)
		}
		return out, nil
	})
	return nil
}
func capabilityParamHandler(t *testing.T, build bool) http.Handler {
	t.Helper()
	c := huma.DefaultConfig("Param Wrapper", "1.0.0")
	if !build {
		r := http.NewServeMux()
		if err := registerCapabilityParam(humago.New(r, c)); err != nil {
			t.Fatal(err)
		}
		return r
	}

	result, err := testkit.NewAPIHandler(testkit.Spec{Config: c, Native: []testkit.Registration{{Register: registerCapabilityParam}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

func TestCapabilityParamWrapperReactorNativeHTTPBaseline(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		value       int
		set         bool
		parsed      string
	}{{"present", "?wrapped=42", 42, true, "42"}, {"absent", "", 0, false, ""}} {
		t.Run(tc.name, func(t *testing.T) {
			var bodies [2]map[string]any
			var docs [2]any
			for i, h := range []http.Handler{capabilityParamHandler(t, false), capabilityParamHandler(t, true)} {
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/param-wrapper"+tc.query, nil))
				if w.Code != http.StatusOK {
					t.Fatalf("arm %d status=%d body=%s", i, w.Code, w.Body.String())
				}
				if err := json.Unmarshal(w.Body.Bytes(), &bodies[i]); err != nil {
					t.Fatal(err)
				}
				if bodies[i]["value"] != float64(tc.value) || bodies[i]["is_set"] != tc.set || bodies[i]["parsed"] != tc.parsed {
					t.Fatalf("arm %d full top-level response=%v", i, bodies[i])
				}
				docs[i] = captureCapabilityContextOpenAPI(t, h)
			}
			if !reflect.DeepEqual(bodies[0], bodies[1]) || !reflect.DeepEqual(docs[0], docs[1]) {
				t.Fatal("ParamWrapper Native/Build response or full OpenAPI differs")
			}
		})
	}
}
