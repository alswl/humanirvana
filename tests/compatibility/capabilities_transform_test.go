package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	testkit "github.com/alswl/humanirvana/tests/compatibility/internal/testkit"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type capabilityTransformInput struct{}

type capabilityTransformBody struct {
	Value string `json:"value"`
}

type capabilityTransformOutput struct {
	Body capabilityTransformBody `json:"body"`
}

func capabilityTransformConfig(hookCalls *int, order *[]string) huma.Config {
	c := huma.DefaultConfig("Transform", "1.0.0")
	c.CreateHooks = append(c.CreateHooks, func(c huma.Config) huma.Config {
		*hookCalls++
		c.Transformers = append(c.Transformers,
			func(_ huma.Context, status string, v any) (any, error) {
				*order = append(*order, "first:"+status+":"+fmt.Sprintf("%T", v))
				mutateCapabilityTransformValue(v, "first-")
				return v, nil
			},
			func(_ huma.Context, status string, v any) (any, error) {
				*order = append(*order, "second:"+status+":"+fmt.Sprintf("%T", v))
				mutateCapabilityTransformValue(v, "second-")
				return v, nil
			},
		)
		return c
	})
	return c
}

func mutateCapabilityTransformValue(v any, prefix string) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return
	}
	field := rv.Elem().FieldByName("Value")
	if field.IsValid() && field.CanSet() && field.Kind() == reflect.String {
		field.SetString(prefix + field.String())
	}
}

func registerCapabilityTransform(api huma.API) error {
	huma.Register[capabilityTransformInput, capabilityTransformOutput](api, huma.Operation{Method: http.MethodGet, Path: "/transform", OperationID: "transform"}, func(context.Context, *capabilityTransformInput) (*capabilityTransformOutput, error) {
		out := &capabilityTransformOutput{}
		out.Body.Value = "base"
		return out, nil
	})
	return nil
}

func capabilityTransformServer(t *testing.T, build bool) (http.Handler, int, *[]string) {
	t.Helper()
	calls := 0
	order := make([]string, 0, 4)
	c := capabilityTransformConfig(&calls, &order)
	if !build {
		r := http.NewServeMux()
		if err := registerCapabilityTransform(humago.New(r, c)); err != nil {
			t.Fatal(err)
		}
		return r, calls, &order
	}

	result, err := testkit.NewAPIHandler(testkit.Spec{Config: c, Native: []testkit.Registration{{Register: registerCapabilityTransform}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler, calls, &order
}

func TestCapabilityCreateHooksAndTransformersNativeHTTPBaseline(t *testing.T) {
	for _, build := range []bool{false, true} {
		h, hookCalls, order := capabilityTransformServer(t, build)
		if hookCalls != 1 {
			t.Fatalf("Build=%v create hook calls=%d want=1", build, hookCalls)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/transform", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("Build=%v status=%d body=%s", build, w.Code, w.Body.String())
		}
		if len(*order) != 2 || !strings.HasPrefix((*order)[0], "first:200:") || !strings.HasPrefix((*order)[1], "second:200:") {
			t.Fatalf("Build=%v transformer order=%v", build, order)
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body["value"] != "second-first-base" {
			t.Fatalf("Build=%v transformed body=%v raw=%s transformer order=%v", build, body, w.Body.String(), *order)
		}
	}
}
