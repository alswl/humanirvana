package cborcapability

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
	"github.com/danielgtaylor/huma/v2/formats/cbor"
)

type capabilityCBORBody struct {
	Value string `json:"value"`
}

func (*capabilityCBORBody) ContentType(string) string { return "application/cbor" }

type capabilityCBORInput struct {
	Body capabilityCBORBody `json:"body"`
}
type capabilityCBOROutput struct {
	Body capabilityCBORBody `json:"body"`
}

func registerCapabilityCBOR(api huma.API) error {
	huma.Register[capabilityCBORInput, capabilityCBOROutput](api, huma.Operation{Method: http.MethodPost, Path: "/cbor", OperationID: "cbor-roundtrip"}, func(_ context.Context, in *capabilityCBORInput) (*capabilityCBOROutput, error) {
		out := &capabilityCBOROutput{}
		out.Body.Value = in.Body.Value
		return out, nil
	})
	return nil
}

func capabilityCBORHandler(t *testing.T, build bool) http.Handler {
	t.Helper()
	c := huma.DefaultConfig("CBOR", "1.0.0")
	c.Formats = map[string]huma.Format{
		"application/json": huma.DefaultJSONFormat,
		"json":             huma.DefaultJSONFormat,
		"application/cbor": cbor.DefaultCBORFormat,
	}
	if !build {
		r := http.NewServeMux()
		if err := registerCapabilityCBOR(humago.New(r, c)); err != nil {
			t.Fatal(err)
		}
		return r
	}

	result, err := testkit.NewAPIHandler(testkit.Spec{Config: c, Native: []testkit.Registration{{Register: registerCapabilityCBOR}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

func TestCapabilityCBORFormatNativeHTTPBaseline(t *testing.T) {
	format := cbor.DefaultCBORFormat
	var requestBody strings.Builder
	if err := format.Marshal(&requestBody, map[string]string{"value": "binary"}); err != nil {
		t.Fatal(err)
	}
	handlers := [2]http.Handler{capabilityCBORHandler(t, false), capabilityCBORHandler(t, true)}
	var wires [2]struct {
		status int
		header http.Header
		body   string
	}
	var docs [2]map[string]any
	for i, h := range handlers {
		r := httptest.NewRequest(http.MethodPost, "/cbor", strings.NewReader(requestBody.String()))
		r.Header.Set("Content-Type", "application/cbor")
		r.Header.Set("Accept", "application/cbor")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		wires[i] = struct {
			status int
			header http.Header
			body   string
		}{w.Code, w.Header().Clone(), w.Body.String()}
		if wires[i].status != http.StatusOK || wires[i].header.Get("Content-Type") != "application/cbor" {
			t.Fatalf("arm %d status=%d headers=%v body=%x", i, wires[i].status, wires[i].header, wires[i].body)
		}
		var decoded map[string]any
		if err := format.Unmarshal([]byte(wires[i].body), &decoded); err != nil {
			t.Fatalf("arm %d response CBOR decode: %v", i, err)
		}
		if decoded["value"] != "binary" || decoded["$schema"] != nil {
			t.Fatalf("arm %d decoded CBOR response=%v", i, decoded)
		}
		openapi := httptest.NewRecorder()
		h.ServeHTTP(openapi, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
		if openapi.Code != http.StatusOK {
			t.Fatalf("arm %d OpenAPI status=%d", i, openapi.Code)
		}
		if err := json.Unmarshal(openapi.Body.Bytes(), &docs[i]); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(wires[0], wires[1]) || !reflect.DeepEqual(docs[0], docs[1]) {
		t.Fatal("Native and Build CBOR wire/OpenAPI differ")
	}
	operation := docs[0]["paths"].(map[string]any)["/cbor"].(map[string]any)["post"].(map[string]any)
	requestContent := operation["requestBody"].(map[string]any)["content"].(map[string]any)
	responseContent := operation["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)
	if requestContent["application/json"] == nil || responseContent["application/cbor"] == nil {
		t.Fatalf("CBOR request/response media missing from OpenAPI: request=%v response=%v", requestContent, responseContent)
	}
}
