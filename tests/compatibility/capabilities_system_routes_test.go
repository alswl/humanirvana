package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	testkit "github.com/alswl/humanirvana/tests/compatibility/internal/testkit"
	"github.com/danielgtaylor/huma/v2"
)

type capabilitySystemInput struct{}
type capabilitySystemOutput struct {
	Body struct {
		Value string `json:"value"`
	} `json:"body"`
}

func registerCapabilitySystem(api huma.API) error {
	huma.Register[capabilitySystemInput, capabilitySystemOutput](api, huma.Operation{Method: http.MethodGet, Path: "/system-item", OperationID: "system-item"}, func(context.Context, *capabilitySystemInput) (*capabilitySystemOutput, error) {
		out := &capabilitySystemOutput{}
		out.Body.Value = "ok"
		return out, nil
	})
	return nil
}

func capabilitySystemConfig() huma.Config {
	c := huma.DefaultConfig("System routes", "1.0.0")
	c.OpenAPIPath = "/spec"
	c.DocsPath = "/reference"
	c.SchemasPath = "/components"
	return c
}

func capabilitySystemHandler(t *testing.T, build bool) http.Handler {
	t.Helper()
	r := http.NewServeMux()
	c := capabilitySystemConfig()
	if !build {
		if err := registerCapabilitySystem(humago.New(r, c)); err != nil {
			t.Fatal(err)
		}
		return r
	}
	result, err := testkit.NewAPIHandler(testkit.Spec{Config: c, Native: []testkit.Registration{{Register: registerCapabilitySystem}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

type capabilityWire struct {
	status int
	header http.Header
	body   string
}

func capabilityGet(h http.Handler, path string) capabilityWire {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return capabilityWire{w.Code, w.Header().Clone(), w.Body.String()}
}

func TestCapabilitySystemRoutesNativeHTTPBaseline(t *testing.T) {
	paths := []string{"/spec.json", "/spec-3.0.json", "/spec.yaml", "/spec-3.0.yaml", "/reference", "/components/CapabilitySystemOutputBody"}
	handlers := [2]http.Handler{capabilitySystemHandler(t, false), capabilitySystemHandler(t, true)}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			wires := [2]capabilityWire{capabilityGet(handlers[0], path), capabilityGet(handlers[1], path)}
			if wires[0].status != wires[1].status || !reflect.DeepEqual(wires[0].header, wires[1].header) || wires[0].body != wires[1].body {
				t.Fatalf("native=%+v Build=%+v", wires[0], wires[1])
			}
			if wires[0].status != http.StatusOK {
				t.Fatalf("status=%d body=%s", wires[0].status, wires[0].body)
			}
			switch path {
			case "/spec.json", "/spec-3.0.json":
				var docs [2]map[string]any
				for i := range wires {
					if err := json.Unmarshal([]byte(wires[i].body), &docs[i]); err != nil {
						t.Fatal(err)
					}
				}
				if !reflect.DeepEqual(docs[0], docs[1]) {
					t.Fatalf("OpenAPI differs native=%v Build=%v", docs[0], docs[1])
				}
				wantVersion := "3.1.0"
				if strings.Contains(path, "3.0") {
					wantVersion = "3.0.3"
				}
				if docs[0]["openapi"] != wantVersion {
					t.Fatalf("OpenAPI version=%v want %s", docs[0]["openapi"], wantVersion)
				}
			case "/spec.yaml", "/spec-3.0.yaml":
				if !strings.Contains(wires[0].body, "system-item") || !strings.Contains(wires[0].body, "openapi:") {
					t.Fatalf("unexpected YAML body %q", wires[0].body)
				}
			case "/reference":
				if wires[0].header.Get("Content-Type") != "text/html" || !strings.Contains(wires[0].body, "<elements-api") || !strings.Contains(wires[0].body, "apiDescriptionUrl=\"/spec.yaml\"") {
					t.Fatalf("docs HTML/header mismatch: header=%v body=%q", wires[0].header, wires[0].body)
				}
			case "/components/CapabilitySystemOutputBody":
				if !strings.Contains(wires[0].body, "value") {
					t.Fatalf("schema body=%q", wires[0].body)
				}
			}
		})
	}
}

func TestCapabilitySystemRoutesDisabled(t *testing.T) {
	c := huma.DefaultConfig("Disabled routes", "1.0.0")
	c.OpenAPIPath, c.DocsPath, c.SchemasPath = "", "", ""
	r := http.NewServeMux()
	if err := registerCapabilitySystem(humago.New(r, c)); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/openapi.json", "/openapi.yaml", "/docs", "/schemas/Anything"} {
		if got := capabilityGet(r, path); got.status != http.StatusNotFound {
			t.Fatalf("disabled path %s status=%d body=%s", path, got.status, got.body)
		}
	}
}

func TestCapabilitySystemRouteDuplicateUsesNativeHumaBehavior(t *testing.T) {
	c := capabilitySystemConfig()
	r := http.NewServeMux()
	api := humago.New(r, c)
	defer func() {
		if recovered := recover(); recovered == nil || !strings.Contains(fmt.Sprint(recovered), `conflicts with pattern "GET /spec.json"`) {
			t.Fatalf("native duplicate route registration panic=%v", recovered)
		}
	}()
	huma.Register[capabilitySystemInput, capabilitySystemOutput](api, huma.Operation{Method: http.MethodGet, Path: "/spec.json", OperationID: "system-path-collision"}, func(context.Context, *capabilitySystemInput) (*capabilitySystemOutput, error) {
		return &capabilitySystemOutput{}, nil
	})
}
