package demo

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	humayaml "github.com/danielgtaylor/huma/v2/yaml"
)

func TestCapabilityOpenAPIJSONYAMLandDowngradeModels(t *testing.T) {
	minimum := 2.5
	exclusiveMaximum := 9.0
	schema := &huma.Schema{
		Type: "string", Nullable: true, ContentEncoding: "base64",
		ExclusiveMinimum: &minimum, ExclusiveMaximum: &exclusiveMaximum,
		Examples: []any{"YWJj"},
	}
	spec := &huma.OpenAPI{
		OpenAPI: "3.1.0",
		Info:    &huma.Info{Title: "Model coverage", Version: "1"},
		Servers: []*huma.Server{{URL: "/api"}},
		Paths: map[string]*huma.PathItem{
			"/value": {Post: &huma.Operation{
				OperationID: "post-value",
				RequestBody: &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{"application/json": {Schema: schema}}},
				Responses:   map[string]*huma.Response{"200": {Description: "ok", Content: map[string]*huma.MediaType{"application/json": {Schema: schema}}}},
			}},
		},
		Tags:         []*huma.Tag{{Name: "model"}},
		ExternalDocs: &huma.ExternalDocs{Description: "reference", URL: "https://example.test/docs"},
		Extensions:   map[string]any{"x-model-check": true},
	}

	jsonBytes, err := spec.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var jsonDoc map[string]any
	if err := json.Unmarshal(jsonBytes, &jsonDoc); err != nil {
		t.Fatal(err)
	}
	if jsonDoc["openapi"] != "3.1.0" || jsonDoc["x-model-check"] != true || jsonDoc["servers"].([]any)[0].(map[string]any)["url"] != "/api" {
		t.Fatalf("OpenAPI JSON root fields=%v", jsonDoc)
	}
	jsonSchema := capabilityOpenAPITestSchema(jsonDoc)
	if !reflect.DeepEqual(jsonSchema["type"], []any{"string", "null"}) || jsonSchema["contentEncoding"] != "base64" || jsonSchema["exclusiveMinimum"] != 2.5 || jsonSchema["exclusiveMaximum"] != 9.0 || !reflect.DeepEqual(jsonSchema["examples"], []any{"YWJj"}) {
		t.Fatalf("OpenAPI 3.1 schema JSON=%v", jsonSchema)
	}

	yamlBytes, err := spec.YAML()
	if err != nil {
		t.Fatal(err)
	}
	yamlText := string(yamlBytes)
	for _, fragment := range []string{"openapi: 3.1.0", "operationId: post-value", "contentEncoding: base64", "exclusiveMinimum: 2.5", "exclusiveMaximum: 9", "x-model-check: true"} {
		if !strings.Contains(yamlText, fragment) {
			t.Fatalf("OpenAPI YAML missing %q: %s", fragment, yamlText)
		}
	}

	downgraded, err := spec.Downgrade()
	if err != nil {
		t.Fatal(err)
	}
	var downgradedDoc map[string]any
	if err := json.Unmarshal(downgraded, &downgradedDoc); err != nil {
		t.Fatal(err)
	}
	if downgradedDoc["openapi"] != "3.0.3" {
		t.Fatalf("downgraded version=%v", downgradedDoc["openapi"])
	}
	downgradedSchema := capabilityOpenAPITestSchema(downgradedDoc)
	if downgradedSchema["type"] != "string" || downgradedSchema["nullable"] != true || downgradedSchema["minimum"] != 2.5 || downgradedSchema["exclusiveMinimum"] != true || downgradedSchema["maximum"] != 9.0 || downgradedSchema["exclusiveMaximum"] != true || downgradedSchema["format"] != "base64" || downgradedSchema["contentEncoding"] != nil || downgradedSchema["example"] != "YWJj" || downgradedSchema["examples"] != nil {
		t.Fatalf("downgraded schema=%v", downgradedSchema)
	}

	downgradedYAML, err := spec.DowngradeYAML()
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"openapi: 3.0.3", "nullable: true", "exclusiveMinimum: true", "format: base64", "example: YWJj"} {
		if !strings.Contains(string(downgradedYAML), fragment) {
			t.Fatalf("downgraded YAML missing %q: %s", fragment, downgradedYAML)
		}
	}
}

func TestCapabilityOpenAPIYAMLConverter(t *testing.T) {
	var out bytes.Buffer
	if err := humayaml.Convert(&out, strings.NewReader(`{"name":"widget","enabled":true,"tags":["a","b"]}`)); err != nil {
		t.Fatal(err)
	}
	want := "name: widget\nenabled: true\ntags:\n  - a\n  - b\n"
	if out.String() != want {
		t.Fatalf("JSON-to-YAML=%q want %q", out.String(), want)
	}
	out.Reset()
	if err := humayaml.Convert(&out, strings.NewReader(`{"name":`)); err == nil {
		t.Fatal("malformed JSON unexpectedly converted")
	}
}

func capabilityOpenAPITestSchema(doc map[string]any) map[string]any {
	paths := doc["paths"].(map[string]any)
	post := paths["/value"].(map[string]any)["post"].(map[string]any)
	request := post["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)
	return request["schema"].(map[string]any)
}
