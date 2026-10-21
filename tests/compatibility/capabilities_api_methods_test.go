package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	testkit "github.com/alswl/humanirvana/tests/compatibility/internal/testkit"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/negotiation"
)

type capabilityAPIMethodInput struct{}
type capabilityAPIMethodOutput struct{}

type capabilityAPIMethodEvidence struct {
	Negotiate    string
	Unmarshal    string
	Marshal      string
	UnknownIs    bool
	UnknownMedia bool
}

func inspectCapabilityAPIMethods(api huma.API) capabilityAPIMethodEvidence {
	e := capabilityAPIMethodEvidence{}
	e.Negotiate, _ = api.Negotiate("application/json")
	var decoded struct {
		Value string `json:"value"`
	}
	if err := api.Unmarshal("application/json", []byte(`{"value":"decoded"}`), &decoded); err == nil {
		e.Unmarshal = decoded.Value
	}
	var b bytes.Buffer
	if err := api.Marshal(&b, "application/json", map[string]string{"value": "encoded"}); err == nil {
		e.Marshal = b.String()
	}
	_, err := api.Negotiate("application/unknown")
	e.UnknownIs = err == huma.ErrUnknownAcceptContentType
	return e
}

func registerCapabilityAPIMethods(e *capabilityAPIMethodEvidence) func(huma.API) error {
	return func(api huma.API) error {
		*e = inspectCapabilityAPIMethods(api)
		huma.Register[capabilityAPIMethodInput, capabilityAPIMethodOutput](api, huma.Operation{Method: http.MethodGet, Path: "/api-methods", OperationID: "api-methods"}, func(context.Context, *capabilityAPIMethodInput) (*capabilityAPIMethodOutput, error) {
			return &capabilityAPIMethodOutput{}, nil
		})
		return nil
	}
}

func capabilityAPIMethodConfig() huma.Config {
	c := huma.DefaultConfig("API methods", "1.0.0")
	c.NoFormatFallback = true
	return c
}

func TestCapabilityAPIMarshalUnmarshalNegotiateNativeHTTPBaseline(t *testing.T) {
	var evidence [2]capabilityAPIMethodEvidence
	for i := range evidence {
		r := http.NewServeMux()
		register := registerCapabilityAPIMethods(&evidence[i])
		if i == 0 {
			if err := register(humago.New(r, capabilityAPIMethodConfig())); err != nil {
				t.Fatal(err)
			}
		} else {
			result, err := testkit.NewAPIHandler(testkit.Spec{Config: capabilityAPIMethodConfig(), Native: []testkit.Registration{{Register: register}}})
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			result.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api-methods", nil))
			if w.Code != http.StatusNoContent {
				t.Fatalf("standard API fixture route status=%d body=%s", w.Code, w.Body.String())
			}
		}
	}
	if evidence[0] != evidence[1] {
		t.Fatalf("native API=%+v Build API=%+v", evidence[0], evidence[1])
	}
	if evidence[0].Negotiate != "application/json" || evidence[0].Unmarshal != "decoded" || evidence[0].Marshal != `{"value":"encoded"}`+"\n" || !evidence[0].UnknownIs {
		t.Fatalf("API methods evidence=%+v", evidence[0])
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(evidence[0].Marshal), &schema); err != nil || schema["value"] != "encoded" {
		t.Fatalf("Marshal wire=%s err=%v", evidence[0].Marshal, err)
	}
}

func TestCapabilityAPIMethodEdgesNativeHTTPBaseline(t *testing.T) {
	type transformValue struct {
		Value string `json:"value"`
	}
	type edge struct {
		name, accept, unmarshalType, unmarshalBody, marshalType string
		fallback, wantUnknown                                   bool
		wantFormat, wantDecoded, wantMarshaled                  string
		wantUnknownMedia                                        bool
		wantTransformError                                      bool
		wantTransformIdentity                                   bool
	}
	boom := errors.New("transform stopped")
	cases := []edge{
		{name: "empty API Accept falls back when enabled", unmarshalType: "application/json", unmarshalBody: `{"value":"ok"}`, marshalType: "application/json", fallback: true, wantFormat: "application/json", wantDecoded: "ok", wantMarshaled: `{"value":"encoded"}` + "\n"},
		{name: "unknown Accept falls back to JSON", accept: "application/x-unknown", unmarshalType: "application/json", unmarshalBody: `{"value":"ok"}`, marshalType: "application/json", fallback: true, wantFormat: "application/json", wantDecoded: "ok", wantMarshaled: `{"value":"encoded"}` + "\n"},
		{name: "quality and exact media type", accept: "text/plain;q=0.8, application/json;q=0.9", unmarshalType: "application/json", unmarshalBody: `{"value":"ok"}`, marshalType: "application/json", wantFormat: "application/json", wantDecoded: "ok", wantMarshaled: `{"value":"encoded"}` + "\n"},
		{name: "wildcard fallback enabled selects default", accept: "text/*;q=0.8", unmarshalType: "application/json", unmarshalBody: `{"value":"ok"}`, marshalType: "application/json", fallback: true, wantFormat: "application/json", wantDecoded: "ok", wantMarshaled: `{"value":"encoded"}` + "\n"},
		{name: "unknown media type", accept: "application/json", unmarshalType: "application/unknown", unmarshalBody: `{}`, marshalType: "application/unknown", wantFormat: "application/json", wantUnknownMedia: true},
		{name: "plus json suffix", accept: "application/json", unmarshalType: "application/vnd.example+json", unmarshalBody: `{"value":"ok"}`, marshalType: "application/vnd.example+json", wantFormat: "application/json", wantDecoded: "ok", wantMarshaled: `{"value":"encoded"}` + "\n"},
		{name: "fallback disabled rejects unknown Accept", accept: "application/x-unknown", unmarshalType: "application/json", unmarshalBody: `{"value":"ok"}`, marshalType: "application/json", wantUnknown: true, wantDecoded: "ok", wantMarshaled: `{"value":"encoded"}` + "\n"},
		{name: "Transform succeeds without configured transformer", accept: "application/json", unmarshalType: "application/json", unmarshalBody: `{}`, marshalType: "application/json", fallback: true, wantFormat: "application/json", wantMarshaled: `{"value":"encoded"}` + "\n"},
		{name: "Transform returns configured error", accept: "application/json", unmarshalType: "application/json", unmarshalBody: `{}`, marshalType: "application/json", fallback: true, wantFormat: "application/json", wantMarshaled: `{"value":"encoded"}` + "\n", wantTransformError: true, wantTransformIdentity: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var observations [2]capabilityAPIMethodEvidence
			var transformErrors [2]error
			for i := range observations {
				cfg := huma.DefaultConfig("API method edges", "1.0.0")
				cfg.DefaultFormat = "application/json"
				cfg.Formats["application/yaml"] = huma.Format{
					Marshal: func(w io.Writer, v any) error {
						_, err := w.Write([]byte("yaml"))
						return err
					},
					Unmarshal: func([]byte, any) error { return nil },
				}
				cfg.NoFormatFallback = !tc.fallback
				cfg.DefaultFormat = "application/json"
				if tc.wantTransformError {
					cfg.Transformers = []huma.Transformer{func(huma.Context, string, any) (any, error) { return nil, boom }}
				}
				r := http.NewServeMux()
				api := humago.New(r, cfg)
				huma.Register[capabilityAPIMethodInput, capabilityAPIMethodOutput](api, huma.Operation{Method: http.MethodGet, Path: "/api-method-edges", OperationID: "api-method-edges"}, func(context.Context, *capabilityAPIMethodInput) (*capabilityAPIMethodOutput, error) {
					return &capabilityAPIMethodOutput{}, nil
				})

				var negotiateErr error
				observations[i].Negotiate, negotiateErr = api.Negotiate(tc.accept)
				observations[i].UnknownIs = errors.Is(negotiateErr, huma.ErrUnknownAcceptContentType)
				var decoded struct {
					Value string `json:"value"`
				}
				if err := api.Unmarshal(tc.unmarshalType, []byte(tc.unmarshalBody), &decoded); err == nil {
					observations[i].Unmarshal = decoded.Value
				} else if errors.Is(err, huma.ErrUnknownContentType) {
					observations[i].UnknownMedia = true
				}
				var b bytes.Buffer
				if err := api.Marshal(&b, tc.marshalType, map[string]string{"value": "encoded"}); err == nil {
					observations[i].Marshal = b.String()
				} else if errors.Is(err, huma.ErrUnknownContentType) {
					observations[i].UnknownMedia = true
				}
				_, err := api.Transform(humago.NewContext(&huma.Operation{}, httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder()), "200", transformValue{Value: "original"})
				transformErrors[i] = err
			}
			if observations[0] != observations[1] || (transformErrors[0] == nil) != (transformErrors[1] == nil) || (transformErrors[0] != nil && !errors.Is(transformErrors[0], boom)) != (transformErrors[1] != nil && !errors.Is(transformErrors[1], boom)) {
				t.Fatalf("native=%+v Build=%+v transform errors=%v", observations[0], observations[1], transformErrors)
			}
			got := observations[0]
			if got.Negotiate != tc.wantFormat || got.Unmarshal != tc.wantDecoded || got.Marshal != tc.wantMarshaled || got.UnknownIs != tc.wantUnknown || got.UnknownMedia != tc.wantUnknownMedia {
				t.Fatalf("evidence=%+v want=(format=%q decoded=%q marshaled=%q unknownAccept=%v unknownMedia=%v)", got, tc.wantFormat, tc.wantDecoded, tc.wantMarshaled, tc.wantUnknown, tc.wantUnknownMedia)
			}
			if (transformErrors[0] != nil) != tc.wantTransformError || (tc.wantTransformIdentity && !errors.Is(transformErrors[0], boom)) {
				t.Fatalf("Transform error=%v wantError=%v wantIdentity=%v", transformErrors[0], tc.wantTransformError, tc.wantTransformIdentity)
			}
		})
	}
}

func TestCapabilityNegotiationQualityAndTieOrder(t *testing.T) {
	allowed := []string{"application/json", "application/yaml"}
	for _, tc := range []struct{ accept, want string }{
		{"application/yaml;q=0.9, application/json;q=0.7", "application/yaml"},
		{"application/yaml;q=0.7, application/json;q=0.7", "application/json"},
		{"text/*;q=0.9", ""},
	} {
		if got := negotiation.SelectQValue(tc.accept, allowed); got != tc.want {
			t.Fatalf("SelectQValue(%q)=%q want %q", tc.accept, got, tc.want)
		}
		if got := negotiation.SelectQValueFast(tc.accept, allowed); tc.accept != "application/yaml;q=0.9, application/json;q=0.7" && got != tc.want {
			t.Fatalf("SelectQValueFast(%q)=%q want %q", tc.accept, got, tc.want)
		}
	}
}

func TestCapabilityAPIMultiFormatNegotiationNativeHTTPBaseline(t *testing.T) {
	newConfig := func() huma.Config {
		c := huma.DefaultConfig("Multi format negotiation", "1.0.0")
		c.DefaultFormat = "application/json"
		c.Formats["application/yaml"] = huma.Format{
			Marshal:   func(w io.Writer, _ any) error { _, err := w.Write([]byte("yaml")); return err },
			Unmarshal: func([]byte, any) error { return nil },
		}
		return c
	}
	for _, accept := range []string{
		"application/yaml;q=0.9, application/json;q=0.7",
		"application/yaml;q=0.7, application/json;q=0.7",
	} {
		var results [2][2]string
		for arm := range results {
			for repeat := range results[arm] {
				r := http.NewServeMux()
				config := newConfig()
				api := humago.New(r, config)
				results[arm][repeat], _ = api.Negotiate(accept)
			}
		}
		if results[0][0] != results[0][1] || results[1][0] != results[1][1] {
			t.Fatalf("unstable result for %q: %+v", accept, results)
		}
		if results[0][0] != results[1][0] {
			t.Fatalf("native=%q Build=%q for %q", results[0][0], results[1][0], accept)
		}
		if results[0][0] != "application/json" && results[0][0] != "application/yaml" {
			t.Fatalf("unexpected negotiated format %q", results[0][0])
		}
	}
}

type capabilityFailingWriter struct{ err error }

func (w capabilityFailingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestCapabilityAPIMethodDecodeAndWriteErrorsNativeHTTPBaseline(t *testing.T) {
	boom := errors.New("writer stopped")
	var apis [2]huma.API
	for i := range apis {
		r := http.NewServeMux()
		config := huma.DefaultConfig("API method errors", "1.0.0")
		register := func(api huma.API) error {
			apis[i] = api
			huma.Register[capabilityAPIMethodInput, capabilityAPIMethodOutput](api, huma.Operation{Method: http.MethodGet, Path: "/api-method-errors", OperationID: "api-method-errors"}, func(context.Context, *capabilityAPIMethodInput) (*capabilityAPIMethodOutput, error) {
				return &capabilityAPIMethodOutput{}, nil
			})
			return nil
		}
		if i == 0 {
			if err := register(humago.New(r, config)); err != nil {
				t.Fatal(err)
			}
		} else {
			if _, err := testkit.NewAPIHandler(testkit.Spec{Config: config, Native: []testkit.Registration{{Register: register}}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	type outcome struct {
		decoded         bool
		decodeError     string
		writeIsSentinel bool
		openAPI         bool
	}
	var outcomes [2]outcome
	for i, api := range apis {
		var decoded map[string]any
		decodeErr := api.Unmarshal("application/json", []byte(`{"value":`), &decoded)
		writeErr := api.Marshal(capabilityFailingWriter{err: boom}, "application/json", map[string]string{"value": "x"})
		outcomes[i] = outcome{
			decoded:         decodeErr == nil,
			decodeError:     fmt.Sprintf("%T", decodeErr),
			writeIsSentinel: errors.Is(writeErr, boom),
			openAPI:         api.OpenAPI() != nil,
		}
	}
	if outcomes[0] != outcomes[1] {
		t.Fatalf("native=%+v Build=%+v", outcomes[0], outcomes[1])
	}
	if outcomes[0].decoded || outcomes[0].decodeError == "<nil>" || !outcomes[0].writeIsSentinel || !outcomes[0].openAPI {
		t.Fatalf("unexpected API method errors: %+v", outcomes[0])
	}
}
