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

type capabilityRegistrationInput struct{}
type capabilityRegistrationOutput struct{}

type capabilityAutoRegistrar struct{}

func (*capabilityAutoRegistrar) RegisterGet(api huma.API) {
	huma.Get[capabilityRegistrationInput, capabilityRegistrationOutput](api, "/verbs/get", func(context.Context, *capabilityRegistrationInput) (*capabilityRegistrationOutput, error) {
		return &capabilityRegistrationOutput{}, nil
	}, func(op *huma.Operation) {
		op.Summary = "first modifier"
		op.Description = "modified by the convenience registration fixture"
	}, huma.OperationTags("helpers"), func(op *huma.Operation) {
		op.Summary = "last modifier"
		op.ExternalDocs = &huma.ExternalDocs{Description: "helper docs", URL: "https://example.test/helpers"}
	})
}
func (*capabilityAutoRegistrar) RegisterPost(api huma.API) {
	huma.Post[capabilityRegistrationInput, capabilityRegistrationOutput](api, "/verbs/post", func(context.Context, *capabilityRegistrationInput) (*capabilityRegistrationOutput, error) {
		return &capabilityRegistrationOutput{}, nil
	})
}
func (*capabilityAutoRegistrar) RegisterHead(api huma.API) {
	huma.Head[capabilityRegistrationInput, capabilityRegistrationOutput](api, "/verbs/head", func(context.Context, *capabilityRegistrationInput) (*capabilityRegistrationOutput, error) {
		return &capabilityRegistrationOutput{}, nil
	})
}
func (*capabilityAutoRegistrar) RegisterPut(api huma.API) {
	huma.Put[capabilityRegistrationInput, capabilityRegistrationOutput](api, "/verbs/put", func(context.Context, *capabilityRegistrationInput) (*capabilityRegistrationOutput, error) {
		return &capabilityRegistrationOutput{}, nil
	})
}
func (*capabilityAutoRegistrar) RegisterPatch(api huma.API) {
	huma.Patch[capabilityRegistrationInput, capabilityRegistrationOutput](api, "/verbs/patch", func(context.Context, *capabilityRegistrationInput) (*capabilityRegistrationOutput, error) {
		return &capabilityRegistrationOutput{}, nil
	})
}
func (*capabilityAutoRegistrar) RegisterDelete(api huma.API) {
	huma.Delete[capabilityRegistrationInput, capabilityRegistrationOutput](api, "/verbs/delete", func(context.Context, *capabilityRegistrationInput) (*capabilityRegistrationOutput, error) {
		return &capabilityRegistrationOutput{}, nil
	})
}

func capabilityAutoConfig() huma.AutoConfig {
	return huma.AutoConfig{
		Security: "token",
		Headers:  map[string]string{"X-Client": "demo"},
		Prompt: map[string]huma.AutoConfigVar{"region": {
			Description: "deployment region", Default: "west", Enum: []any{"west", "east"},
		}},
		Params: map[string]string{"project": "demo"},
	}
}

func capabilityRegistrationConfig() huma.Config {
	c := huma.DefaultConfig("Registration helpers", "1.0.0")
	c.OpenAPI.Extensions = map[string]any{"x-cli-config": capabilityAutoConfig()}
	return c
}

func capabilityRegisterHelpers(api huma.API) error {
	huma.AutoRegister(api, &capabilityAutoRegistrar{})
	return nil
}

func capabilityRegistrationHandler(t *testing.T, build bool) http.Handler {
	t.Helper()
	c := capabilityRegistrationConfig()
	if !build {
		r := http.NewServeMux()
		if err := capabilityRegisterHelpers(humago.New(r, c)); err != nil {
			t.Fatal(err)
		}
		return r
	}

	result, err := testkit.NewAPIHandler(testkit.Spec{Config: c, Native: []testkit.Registration{{Register: capabilityRegisterHelpers}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

func TestCapabilityAutoRegisterConvenienceVerbsAndAutoConfigNativeHTTPBaseline(t *testing.T) {
	handlers := [2]http.Handler{capabilityRegistrationHandler(t, false), capabilityRegistrationHandler(t, true)}
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		path := "/verbs/" + map[string]string{http.MethodGet: "get", http.MethodPost: "post", http.MethodHead: "head", http.MethodPut: "put", http.MethodPatch: "patch", http.MethodDelete: "delete"}[method]
		var wires [2]struct {
			status int
			header http.Header
			body   string
		}
		for i, h := range handlers {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
			wires[i] = struct {
				status int
				header http.Header
				body   string
			}{w.Code, w.Header().Clone(), w.Body.String()}
		}
		wantStatus := http.StatusNoContent
		if method == http.MethodHead {
			wantStatus = http.StatusOK
		}
		if !reflect.DeepEqual(wires[0], wires[1]) || wires[0].status != wantStatus {
			t.Fatalf("%s %s native=%+v Build=%+v", method, path, wires[0], wires[1])
		}
	}
	var docs [2]map[string]any
	for i, h := range handlers {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("OpenAPI status=%d body=%s", w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &docs[i]); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(docs[0], docs[1]) {
		t.Fatal("native and Build OpenAPI documents differ")
	}
	paths := docs[0]["paths"].(map[string]any)
	for _, method := range []string{"get", "post", "head", "put", "patch", "delete"} {
		path := "/verbs/" + method
		op := paths[path].(map[string]any)[method].(map[string]any)
		if op["operationId"] == "" {
			t.Fatalf("missing generated operation ID for %s %s", method, path)
		}
		if method == "get" && !reflect.DeepEqual(op["tags"], []any{"helpers"}) {
			t.Fatalf("OperationTags did not reach OpenAPI: %v", op["tags"])
		}
		if method == "get" && (op["summary"] != "last modifier" || op["description"] != "modified by the convenience registration fixture" || op["externalDocs"].(map[string]any)["url"] != "https://example.test/helpers") {
			t.Fatalf("convenience operation modifier order/fields=%v", op)
		}
	}
	config := docs[0]["x-cli-config"].(map[string]any)
	if config["security"] != "token" || config["params"].(map[string]any)["project"] != "demo" || config["prompt"].(map[string]any)["region"].(map[string]any)["default"] != "west" {
		t.Fatalf("AutoConfig OpenAPI extension=%v", config)
	}
}
