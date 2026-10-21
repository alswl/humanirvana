package demo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	testkit "github.com/alswl/humanirvana/tests/compatibility/internal/testkit"
	"github.com/danielgtaylor/huma/v2"
)

type capabilityResolverInput struct {
	Body capabilityResolverBody `json:"body"`
}

type capabilityResolverBody struct {
	Code string `json:"code"`
}

func (b *capabilityResolverBody) Resolve(_ huma.Context, prefix *huma.PathBuffer) []error {
	if b.Code == "bad" {
		return []error{&huma.ErrorDetail{Location: prefix.With("code"), Message: "resolver rejected value", Value: b.Code}}
	}
	return nil
}

type capabilityResolverOutput struct {
	Body struct {
		Accepted bool `json:"accepted"`
	} `json:"body"`
}

func registerCapabilityResolver(api huma.API) error {
	huma.Register[capabilityResolverInput, capabilityResolverOutput](api, huma.Operation{Method: http.MethodPost, Path: "/resolver", OperationID: "resolver"}, func(context.Context, *capabilityResolverInput) (*capabilityResolverOutput, error) {
		out := &capabilityResolverOutput{}
		out.Body.Accepted = true
		return out, nil
	})
	return nil
}

func capabilityResolverHandler(t *testing.T, build bool) http.Handler {
	t.Helper()

	result, err := testkit.NewAPIHandler(testkit.Spec{Config: huma.DefaultConfig("Resolver", "1.0.0"), Native: []testkit.Registration{{Register: registerCapabilityResolver}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

func TestCapabilityResolverWithPathNativeHTTPBaseline(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"accepted", `{"code":"ok"}`, http.StatusOK},
		{"resolver error", `{"code":"bad"}`, http.StatusUnprocessableEntity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var nativeCode, buildCode int
			var nativeHeader, buildHeader http.Header
			var nativeBody, buildBody string
			for i, h := range []http.Handler{capabilityResolverHandler(t, false), capabilityResolverHandler(t, true)} {
				r := httptest.NewRequest(http.MethodPost, "/resolver", strings.NewReader(tc.body))
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if i == 0 {
					nativeCode, nativeHeader, nativeBody = w.Code, w.Header().Clone(), w.Body.String()
				} else {
					buildCode, buildHeader, buildBody = w.Code, w.Header().Clone(), w.Body.String()
				}
			}
			if nativeCode != tc.status || buildCode != nativeCode || nativeHeader.Get("Content-Type") != buildHeader.Get("Content-Type") || nativeBody != buildBody {
				t.Fatalf("native=(%d,%v,%q) Build=(%d,%v,%q)", nativeCode, nativeHeader, nativeBody, buildCode, buildHeader, buildBody)
			}
			if tc.status == http.StatusUnprocessableEntity {
				var body struct {
					Errors []huma.ErrorDetail `json:"errors"`
				}
				if err := json.Unmarshal([]byte(nativeBody), &body); err != nil {
					t.Fatal(err)
				}
				if len(body.Errors) != 1 || body.Errors[0].Location != "body.code" || body.Errors[0].Message != "resolver rejected value" || body.Errors[0].Value != "bad" {
					t.Fatalf("resolver detail=%+v", body.Errors)
				}
			}
		})
	}
}
