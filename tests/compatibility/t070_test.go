package demo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	testkit "github.com/alswl/humanirvana/tests/compatibility/internal/testkit"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type t070Input struct {
	Body struct {
		Value string `json:"value"`
	} `json:"body"`
}

type t070Output struct {
	Body struct {
		Value string `json:"value"`
	} `json:"body"`
}

func t070Config() huma.Config {
	c := huma.DefaultConfig("T070 formats", "1.0.0")
	c.Formats = map[string]huma.Format{
		"application/json": huma.DefaultJSONFormat,
		"json":             huma.DefaultJSONFormat,
		"application/x-test": {
			Marshal: func(w io.Writer, v any) error {
				b, err := json.Marshal(v)
				if err != nil {
					return err
				}
				_, err = io.WriteString(w, "test:"+string(b))
				return err
			},
			Unmarshal: func(data []byte, v any) error {
				return json.Unmarshal([]byte(strings.TrimPrefix(string(data), "test:")), v)
			},
		},
	}
	c.DefaultFormat = "application/json"
	c.NoFormatFallback = true
	return c
}

func t070Register(api huma.API) error {
	huma.Register[t070Input, t070Output](api, huma.Operation{Method: http.MethodPost, Path: "/format", OperationID: "format"}, func(_ context.Context, in *t070Input) (*t070Output, error) {
		out := &t070Output{}
		out.Body.Value = in.Body.Value
		return out, nil
	})
	return nil
}

func t070Handler(t *testing.T, build bool) http.Handler {
	t.Helper()
	r := http.NewServeMux()
	if !build {
		if err := t070Register(humago.New(r, t070Config())); err != nil {
			t.Fatal(err)
		}
		return r
	}
	result, err := testkit.NewAPIHandler(testkit.Spec{Config: t070Config(), Native: []testkit.Registration{{Register: t070Register}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

func TestT070NativeFormatNegotiationAndNirvanaMediaLimit(t *testing.T) {
	cases := []struct {
		name, contentType, accept, body string
		status                          int
		responseType, responseBody      string
	}{
		{"custom format round trip", "application/x-test", "application/x-test", "test:{\"value\":\"roundtrip\"}", http.StatusOK, "application/x-test", "test:{\"$schema\":\"https://example.com/schemas/T070OutputBody.json\",\"value\":\"roundtrip\"}"},
		{"unknown request format", "application/unknown", "application/json", `{"value":"x"}`, http.StatusUnsupportedMediaType, "application/problem+json", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var native, build *httptest.ResponseRecorder
			for i, h := range []http.Handler{t070Handler(t, false), t070Handler(t, true)} {
				r := httptest.NewRequest(http.MethodPost, "/format", strings.NewReader(tc.body))
				r.Header.Set("Content-Type", tc.contentType)
				r.Header.Set("Accept", tc.accept)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if i == 0 {
					native = w
				} else {
					build = w
				}
			}
			if native.Code != tc.status {
				t.Fatalf("status native=%d Build=%d want=%d", native.Code, build.Code, tc.status)
			}
			if native.Header().Get("Content-Type") != tc.responseType {
				t.Fatalf("content-type native=%q Build=%q want=%q", native.Header().Get("Content-Type"), build.Header().Get("Content-Type"), tc.responseType)
			}
			if tc.name == "unknown request format" {
				if !strings.Contains(native.Body.String(), `"title":"Unsupported Media Type"`) || !strings.Contains(native.Body.String(), `"message":"unknown content type: application/unknown"`) {
					t.Fatalf("unexpected 415 body=%q", native.Body.String())
				}
			} else if native.Body.String() != tc.responseBody {
				t.Fatalf("body=%q want=%q", native.Body.String(), tc.responseBody)
			}
			if build.Code != http.StatusUnsupportedMediaType || build.Header().Get("Content-Type") != "application/json" || !strings.Contains(build.Body.String(), "Nirvana:Service:NoExecutorForContentType") {
				t.Fatalf("Nirvana media admission=%d %v %s", build.Code, build.Header(), build.Body.String())
			}
		})
	}
	t.Run("unacceptable response type panics consistently", func(t *testing.T) {
		for _, build := range []bool{false, true} {
			var got any
			func() {
				defer func() { got = recover() }()
				r := httptest.NewRequest(http.MethodPost, "/format", strings.NewReader(`{"value":"x"}`))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Accept", "application/xml")
				t070Handler(t, build).ServeHTTP(httptest.NewRecorder(), r)
			}()
			if !errors.Is(asError(got), huma.ErrUnknownAcceptContentType) {
				t.Fatalf("Build=%v panic=%v", build, got)
			}
		}
	})
}

func asError(v any) error {
	if err, ok := v.(error); ok {
		return err
	}
	return nil
}
