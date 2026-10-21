package reviewcombination

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestNativeAndMixedEstimateHTTPAndSelectedOpenAPIAgree(t *testing.T) {
	native, err := Handler("native")
	if err != nil {
		t.Fatal(err)
	}
	mixed, err := Handler("mixed")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		h      http.Handler
		legacy bool
	}{{"native", native, false}, {"mixed", mixed, true}} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/review/estimates/platform?discount=15", strings.NewReader(`{"items":[{"name":"hosting","unit_cost":120,"quantity":2},{"name":"support","unit_cost":40,"quantity":1}]}`))
			req.Header.Set("X-Currency", "USD")
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			tc.h.ServeHTTP(w, req)
			if w.Code != http.StatusCreated || w.Header().Get("X-Team") != "platform" {
				t.Fatalf("response %d %v %s", w.Code, w.Header(), w.Body)
			}
			var got struct {
				Team      string `json:"team"`
				Currency  string `json:"currency"`
				Subtotal  int    `json:"subtotal"`
				Discount  int    `json:"discount_percent"`
				Total     int    `json:"total"`
				LineCount int    `json:"line_count"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			want := struct {
				Team      string `json:"team"`
				Currency  string `json:"currency"`
				Subtotal  int    `json:"subtotal"`
				Discount  int    `json:"discount_percent"`
				Total     int    `json:"total"`
				LineCount int    `json:"line_count"`
			}{"platform", "USD", 280, 15, 238, 2}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("body=%+v want=%+v", got, want)
			}
			if tc.legacy {
				legacy := httptest.NewRecorder()
				legacyReq := httptest.NewRequest(http.MethodGet, "/legacy/status", nil)
				legacyReq.Header.Set("Accept", "application/json")
				tc.h.ServeHTTP(legacy, legacyReq)
				if legacy.Code != http.StatusOK || !strings.Contains(legacy.Body.String(), "legacy-route") {
					t.Fatalf("legacy route: %d %s", legacy.Code, legacy.Body)
				}
			}
		})
	}
}

func TestEstimateValidationRejectsHeaderQueryAndNestedBody(t *testing.T) {
	h, err := Handler("native")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, currency, query, body string }{
		{"header enum", "GBP", "", `{"items":[{"name":"x","unit_cost":2,"quantity":1}]}`},
		{"query range", "USD", "?discount=51", `{"items":[{"name":"x","unit_cost":2,"quantity":1}]}`},
		{"nested quantity", "USD", "", `{"items":[{"name":"x","unit_cost":2,"quantity":0}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/review/estimates/platform"+tc.query, strings.NewReader(tc.body))
			req.Header.Set("X-Currency", tc.currency)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			b, err := io.ReadAll(w.Body)
			if err != nil || len(b) == 0 {
				t.Fatalf("validation response body=%q err=%v", b, err)
			}
		})
	}
}

func TestNativeAndMixedSelectedOperationAndSchemasMatch(t *testing.T) {
	native, err := Handler("native")
	if err != nil {
		t.Fatal(err)
	}
	mixed, err := Handler("mixed")
	if err != nil {
		t.Fatal(err)
	}
	doc := func(h http.Handler) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("openapi status=%d", w.Code)
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	selectContract := func(d map[string]any) map[string]any {
		paths := d["paths"].(map[string]any)
		path := paths["/review/estimates/{team}"].(map[string]any)
		op := path["post"].(map[string]any)
		responses := op["responses"].(map[string]any)
		created := responses["201"].(map[string]any)
		headers, ok := created["headers"].(map[string]any)
		if !ok || headers["X-Team"] == nil {
			t.Fatalf("201 missing X-Team: %v", created)
		}
		content, ok := created["content"].(map[string]any)
		if !ok || content["application/json"] == nil {
			t.Fatalf("201 missing JSON response schema: %v", created)
		}

		components := d["components"].(map[string]any)
		schemas := components["schemas"].(map[string]any)
		refs := map[string]bool{}
		var walk func(any)
		walk = func(v any) {
			switch x := v.(type) {
			case map[string]any:
				for k, val := range x {
					if k == "$ref" {
						ref, ok := val.(string)
						if ok && strings.HasPrefix(ref, "#/components/schemas/") {
							refs[strings.TrimPrefix(ref, "#/components/schemas/")] = true
						}
					}
					walk(val)
				}
			case []any:
				for _, val := range x {
					walk(val)
				}
			}
		}
		walk(op)
		selected := map[string]any{}
		pending := make([]string, 0, len(refs))
		for name := range refs {
			pending = append(pending, name)
		}
		for len(pending) > 0 {
			name := pending[0]
			pending = pending[1:]
			if _, done := selected[name]; done {
				continue
			}
			if schemas[name] == nil {
				t.Fatalf("unresolved schema reference %q", name)
			}
			if schema, ok := schemas[name]; ok {
				selected[name] = schema
				before := make(map[string]bool, len(refs))
				for ref := range refs {
					before[ref] = true
				}
				walk(schema)
				for ref := range refs {
					if !before[ref] {
						pending = append(pending, ref)
					}
				}
			}
		}
		return map[string]any{"operation": op, "schemas": selected}
	}
	if !reflect.DeepEqual(selectContract(doc(native)), selectContract(doc(mixed))) {
		a, _ := json.MarshalIndent(selectContract(doc(native)), "", "  ")
		b, _ := json.MarshalIndent(selectContract(doc(mixed)), "", "  ")
		t.Fatalf("selected operation or referenced schema differs\nnative=%s\nmixed=%s", a, b)
	}
	if _, ok := doc(mixed)["paths"].(map[string]any)["/legacy/status"]; ok {
		t.Fatal("Nirvana legacy route was unexpectedly projected into Huma OpenAPI")
	}
}
