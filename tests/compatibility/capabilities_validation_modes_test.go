package demo

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
	"github.com/danielgtaylor/huma/v2/validation"
)

type capabilityStrictCasingInput struct {
	Body struct {
		DisplayName string `json:"displayName"`
	} `json:"body"`
}
type capabilityStrictCasingOutput struct {
	Body struct {
		DisplayName string `json:"displayName"`
	} `json:"body"`
}

func registerCapabilityStrictCasing(api huma.API) error {
	huma.Register[capabilityStrictCasingInput, capabilityStrictCasingOutput](api, huma.Operation{Method: http.MethodPost, Path: "/strict-casing", OperationID: "strict-casing"}, func(_ context.Context, in *capabilityStrictCasingInput) (*capabilityStrictCasingOutput, error) {
		out := &capabilityStrictCasingOutput{}
		out.Body.DisplayName = in.Body.DisplayName
		return out, nil
	})
	return nil
}

func capabilityStrictCasingHandler(t *testing.T, build bool) http.Handler {
	t.Helper()
	c := huma.DefaultConfig("Strict casing", "1.0.0")
	if !build {
		r := http.NewServeMux()
		if err := registerCapabilityStrictCasing(humago.New(r, c)); err != nil {
			t.Fatal(err)
		}
		return r
	}

	result, err := testkit.NewAPIHandler(testkit.Spec{Config: c, Native: []testkit.Registration{{Register: registerCapabilityStrictCasing}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

func TestCapabilityStrictCasingNativeHTTPBaseline(t *testing.T) {
	previous := huma.ValidateStrictCasing
	defer func() { huma.ValidateStrictCasing = previous }()
	handlers := [2]http.Handler{capabilityStrictCasingHandler(t, false), capabilityStrictCasingHandler(t, true)}
	for _, tc := range []struct {
		name     string
		strict   bool
		body     string
		status   int
		wantBody string
	}{
		{"case-insensitive field name accepted by default", false, `{"displayname":"Ada"}`, http.StatusOK, `{"$schema":"https://example.com/schemas/CapabilityStrictCasingOutputBody.json","displayName":"Ada"}` + "\n"},
		{"case-insensitive field name rejected in strict mode", true, `{"displayname":"Ada"}`, http.StatusUnprocessableEntity, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			huma.ValidateStrictCasing = tc.strict
			var wires [2]struct {
				status int
				header http.Header
				body   string
			}
			for i, h := range handlers {
				w := httptest.NewRecorder()
				r := httptest.NewRequest(http.MethodPost, "/strict-casing", strings.NewReader(tc.body))
				r.Header.Set("Content-Type", "application/json")
				h.ServeHTTP(w, r)
				wires[i] = struct {
					status int
					header http.Header
					body   string
				}{w.Code, w.Header().Clone(), w.Body.String()}
			}
			if !reflect.DeepEqual(wires[0], wires[1]) || wires[0].status != tc.status {
				t.Fatalf("native=%+v Build=%+v want status=%d", wires[0], wires[1], tc.status)
			}
			if tc.status == http.StatusOK {
				if wires[0].body != tc.wantBody {
					t.Fatalf("success body=%q want %q", wires[0].body, tc.wantBody)
				}
				var got map[string]any
				if err := json.Unmarshal([]byte(wires[0].body), &got); err != nil || got["displayName"] != "Ada" {
					t.Fatalf("response body=%q decoded=%v err=%v", wires[0].body, got, err)
				}
			} else if !strings.Contains(wires[0].body, "unexpected property") {
				t.Fatalf("strict validation response=%q", wires[0].body)
			}
		})
	}
}

func TestCapabilityValidationMessageConstants(t *testing.T) {
	got := []string{
		validation.MsgUnexpectedProperty,
		validation.MsgExpectedRFC3339DateTime,
		validation.MsgExpectedRFC5322Email,
		validation.MsgExpectedInteger,
		validation.MsgExpectedMinimumNumber,
		validation.MsgExpectedRequiredProperty,
	}
	want := []string{
		"unexpected property",
		"expected string to be RFC 3339 date-time",
		"expected string to be RFC 5322 email: %v",
		"expected integer",
		"expected number >= %v",
		"expected required property %s to be present",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("validation message constants=%q want %q", got, want)
	}
}
