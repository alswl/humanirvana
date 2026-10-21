package demo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	testkit "github.com/alswl/humanirvana/tests/compatibility/internal/testkit"
	"github.com/caicloud/nirvana/definition"
	"github.com/caicloud/nirvana/service"
	restbuilder "github.com/caicloud/nirvana/service/rest"
	"github.com/danielgtaylor/huma/v2"
)

type t067LegacyBody struct {
	Value string `json:"value"`
}

type t067LegacyResultOperator struct{}

func (t067LegacyResultOperator) Kind() string      { return "t067-result-description" }
func (t067LegacyResultOperator) In() reflect.Type  { return reflect.TypeOf(t067LegacyBody{}) }
func (t067LegacyResultOperator) Out() reflect.Type { return reflect.TypeOf(t067LegacyBody{}) }
func (t067LegacyResultOperator) Operate(_ context.Context, _ string, value any) (any, error) {
	body := value.(t067LegacyBody)
	body.Value += "-operator"
	return body, nil
}

func t067LegacyHandler(context.Context) (t067LegacyBody, map[string]string, error) {
	return t067LegacyBody{Value: "legacy-result"}, map[string]string{
		"X-Result-Source": "nirvana-meta",
		"X-Result-Mode":   "created",
	}, nil
}

func t067LegacyDescriptor() definition.Descriptor {
	return definition.Descriptor{
		Path: "/result-contract",
		Definitions: []definition.Definition{{
			Method:     definition.Create,
			Function:   t067LegacyHandler,
			Parameters: []definition.Parameter{{Source: definition.Prefab, Name: "context"}},
			Consumes:   []string{definition.MIMEAll},
			Produces:   []string{definition.MIMEJSON},
			Results: []definition.Result{
				{Destination: definition.Data, Operators: []definition.Operator{t067LegacyResultOperator{}}, Description: "response body after result operator"},
				{Destination: definition.Meta, Description: "response metadata"},
				{Destination: definition.Error, Description: "handler error"},
			},
		}},
	}
}

func t067LegacyNative(t *testing.T) http.Handler {
	t.Helper()
	b := restbuilder.NewBuilder()
	b.SetModifier(service.FirstContextParameter())
	if err := b.AddDescriptor(t067LegacyDescriptor()); err != nil {
		t.Fatal(err)
	}
	h, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func t067LegacyRegisterLegacy(t *testing.T) http.Handler {
	t.Helper()
	result, err := testkit.NewAPIHandler(testkit.Spec{
		Config: huma.DefaultConfig("T067 legacy Result", "1.0.0"),
		Legacy: &testkit.LegacyApplication{Descriptors: []definition.Descriptor{t067LegacyDescriptor()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

func t067LegacyResponse(h http.Handler) (int, http.Header, string) {
	r := httptest.NewRequest(http.MethodPost, "/result-contract", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code, w.Header().Clone(), w.Body.String()
}

func TestT067LegacyResultMetaNirvanaAgainstRegisterLegacySameHandler(t *testing.T) {
	// The original Nirvana builder and runtime.RegisterLegacy use separate routers over the same Definition and handler.
	codeN, headersN, bodyN := t067LegacyResponse(t067LegacyNative(t))
	codeB, headersB, bodyB := t067LegacyResponse(t067LegacyRegisterLegacy(t))
	if codeN != http.StatusCreated || codeB != codeN {
		t.Fatalf("status native=%d RegisterLegacy=%d want=%d", codeN, codeB, http.StatusCreated)
	}
	for _, name := range []string{"Content-Type", "X-Result-Source", "X-Result-Mode"} {
		if headersN.Values(name) == nil || !reflect.DeepEqual(headersN.Values(name), headersB.Values(name)) {
			t.Errorf("header %s native=%v RegisterLegacy=%v", name, headersN.Values(name), headersB.Values(name))
		}
	}
	if bodyN != bodyB || bodyN != "{\"value\":\"legacy-result-operator\"}\n" {
		t.Fatalf("body native=%q Build=%q", bodyN, bodyB)
	}
}
