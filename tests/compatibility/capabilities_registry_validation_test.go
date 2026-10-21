package demo

import (
	"context"
	"encoding/json"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	testkit "github.com/alswl/humanirvana/tests/compatibility/internal/testkit"
	"github.com/danielgtaylor/huma/v2"
)

type capabilityRegistryAlias string
type capabilityRegistryAliasTarget struct {
	Text string `json:"text"`
}
type capabilityRegistryNode struct {
	Value string                  `json:"value"`
	Next  *capabilityRegistryNode `json:"next,omitempty"`
}
type capabilityRegistryProvided string

func (*capabilityRegistryProvided) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Format: "email", Description: "provided schema"}
}

type capabilityRegistryTransformed string

func (*capabilityRegistryTransformed) TransformSchema(_ huma.Registry, s *huma.Schema) *huma.Schema {
	s.Format = "uuid"
	s.Description = "transformed schema"
	return s
}

type capabilityRegistryInput struct {
	Body struct {
		Node        *capabilityRegistryNode       `json:"node"`
		Provided    capabilityRegistryProvided    `json:"provided"`
		Transformed capabilityRegistryTransformed `json:"transformed"`
	} `json:"body"`
}
type capabilityRegistryOutput struct {
	Body struct {
		OK bool `json:"ok"`
	} `json:"body"`
}

func capabilityRegistryConfig() huma.Config {
	c := huma.DefaultConfig("Registry validation", "1.0.0")
	r := huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer)
	r.RegisterTypeAlias(reflect.TypeOf(capabilityRegistryAlias("")), reflect.TypeOf(capabilityRegistryAliasTarget{}))
	r.Schema(reflect.TypeOf(capabilityRegistryAlias("")), true, "CapabilityRegistryAlias")
	r.Map()["Preloaded"] = &huma.Schema{Type: "string", Description: "preloaded"}
	c.Components.Schemas = r
	return c
}
func registerCapabilityRegistry(api huma.API) error {
	huma.Register[capabilityRegistryInput, capabilityRegistryOutput](api, huma.Operation{Method: http.MethodPost, Path: "/registry", OperationID: "registry"}, func(context.Context, *capabilityRegistryInput) (*capabilityRegistryOutput, error) {
		return &capabilityRegistryOutput{}, nil
	})
	return nil
}
func capabilityRegistryHandler(t *testing.T, build bool) http.Handler {
	t.Helper()
	r := http.NewServeMux()
	c := capabilityRegistryConfig()
	if !build {
		if err := registerCapabilityRegistry(humago.New(r, c)); err != nil {
			t.Fatal(err)
		}
		return r
	}
	result, err := testkit.NewAPIHandler(testkit.Spec{Config: c, Native: []testkit.Registration{{Register: registerCapabilityRegistry}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}
func TestCapabilityRegistryAndSchemaHooksNativeHTTPBaseline(t *testing.T) {
	handlers := [2]http.Handler{capabilityRegistryHandler(t, false), capabilityRegistryHandler(t, true)}
	wires := [2]capabilityWire{capabilityGet(handlers[0], "/openapi.json"), capabilityGet(handlers[1], "/openapi.json")}
	if wires[0].status != wires[1].status || wires[0].body != wires[1].body {
		t.Fatalf("native=%+v Build=%+v", wires[0], wires[1])
	}
	var docs [2]map[string]any
	for i := range wires {
		if err := json.Unmarshal([]byte(wires[i].body), &docs[i]); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(docs[0], docs[1]) {
		t.Fatal("full OpenAPI differs")
	}
	components := docs[0]["components"].(map[string]any)["schemas"].(map[string]any)
	for _, name := range []string{"Preloaded", "CapabilityRegistryInputBody", "CapabilityRegistryNode", "CapabilityRegistryAliasTarget"} {
		if _, ok := components[name]; !ok {
			t.Fatalf("missing schema %s; keys=%v", name, components)
		}
	}
	if !strings.Contains(wires[0].body, "provided schema") || !strings.Contains(wires[0].body, "transformed schema") || !strings.Contains(wires[0].body, "CapabilityRegistryAliasTarget") || !strings.Contains(wires[0].body, "CapabilityRegistryNode") {
		t.Fatalf("custom/alias schema evidence absent: %s", wires[0].body)
	}
	for _, tc := range []struct {
		name, body string
		status     int
	}{{"valid custom formats", `{"node":{"value":"root","next":{"value":"child"}},"provided":"person@example.com","transformed":"123e4567-e89b-12d3-a456-426614174000"}`, http.StatusOK}, {"invalid custom formats", `{"node":{"value":"root"},"provided":"x","transformed":"x"}`, http.StatusUnprocessableEntity}} {
		var got [2]capabilityWire
		for i, h := range handlers {
			req := httptest.NewRequest(http.MethodPost, "/registry", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			got[i] = capabilityWire{w.Code, w.Header().Clone(), w.Body.String()}
		}
		if got[0].status != tc.status || got[0].status != got[1].status || !reflect.DeepEqual(got[0].header, got[1].header) || got[0].body != got[1].body {
			t.Fatalf("%s native=%+v Build=%+v", tc.name, got[0], got[1])
		}
	}
}

type capabilityValidationInput struct {
	Body struct {
		Count int    `json:"count" minimum:"1" required:"true"`
		Name  string `json:"name" minLength:"2" required:"true"`
	} `json:"body"`
}
type capabilityValidationOutput struct {
	Body struct {
		Count int    `json:"count"`
		Name  string `json:"name"`
	} `json:"body"`
}

func registerCapabilityValidation(api huma.API) error {
	huma.Register[capabilityValidationInput, capabilityValidationOutput](api, huma.Operation{Method: http.MethodPost, Path: "/validate", OperationID: "validate"}, func(_ context.Context, in *capabilityValidationInput) (*capabilityValidationOutput, error) {
		out := &capabilityValidationOutput{}
		out.Body.Count = in.Body.Count
		out.Body.Name = in.Body.Name
		return out, nil
	})
	huma.Register[capabilityValidationInput, capabilityValidationOutput](api, huma.Operation{Method: http.MethodPost, Path: "/skip", OperationID: "skip", SkipValidateBody: true}, func(_ context.Context, in *capabilityValidationInput) (*capabilityValidationOutput, error) {
		out := &capabilityValidationOutput{}
		out.Body.Count = in.Body.Count
		out.Body.Name = in.Body.Name
		return out, nil
	})
	return nil
}
func capabilityValidationHandler(t *testing.T, build bool) http.Handler {
	t.Helper()
	r := http.NewServeMux()
	c := huma.DefaultConfig("Validation", "1.0.0")
	if !build {
		if err := registerCapabilityValidation(humago.New(r, c)); err != nil {
			t.Fatal(err)
		}
		return r
	}
	result, err := testkit.NewAPIHandler(testkit.Spec{Config: c, Native: []testkit.Registration{{Register: registerCapabilityValidation}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}
func TestCapabilityValidationAggregateAndSkipNativeHTTPBaseline(t *testing.T) {
	handlers := [2]http.Handler{capabilityValidationHandler(t, false), capabilityValidationHandler(t, true)}
	for _, tc := range []struct {
		path, body string
		status     int
		wantErrors int
	}{{"/validate", `{"count":0,"name":"x"}`, 422, 2}, {"/skip", `{"count":0,"name":"x"}`, 200, 0}} {
		var wires [2]capabilityWire
		for i, h := range handlers {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			wires[i] = capabilityWire{w.Code, w.Header().Clone(), w.Body.String()}
		}
		if wires[0].status != wires[1].status || !reflect.DeepEqual(wires[0].header, wires[1].header) || wires[0].body != wires[1].body || wires[0].status != tc.status {
			t.Fatalf("%s native=%+v Build=%+v", tc.path, wires[0], wires[1])
		}
		if tc.wantErrors > 0 {
			var body struct {
				Errors []huma.ErrorDetail `json:"errors"`
			}
			if err := json.Unmarshal([]byte(wires[0].body), &body); err != nil {
				t.Fatal(err)
			}
			if len(body.Errors) != tc.wantErrors {
				t.Fatalf("errors=%+v", body.Errors)
			}
		}
	}
}
