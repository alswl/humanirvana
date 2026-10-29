package demo

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	testkit "github.com/alswl/humanirvana/tests/compatibility/internal/testkit"
	"github.com/danielgtaylor/huma/v2"
)

type capabilityContextInput struct {
	ID string `path:"id"`
	Q  string `query:"q"`
}
type capabilityContextObservation struct{}

type capabilityContextOutput struct {
	Body struct {
		Observation map[string]any `json:"observation"`
	} `json:"body"`
}

func registerCapabilityContext(api huma.API) error {
	huma.Register[capabilityContextInput, capabilityContextOutput](api, huma.Operation{Method: http.MethodPost, Path: "/context/{id}", OperationID: "context", Middlewares: []func(huma.Context, func(huma.Context)){func(ctx huma.Context, next func(huma.Context)) {
		ctx = huma.WithContext(ctx, context.WithValue(ctx.Context(), "override", "visible"))
		ctx = huma.WithContext(ctx, context.WithValue(ctx.Context(), capabilityContextObservation{}, ctx))
		next(ctx)
	}}}, func(ctx context.Context, in *capabilityContextInput) (*capabilityContextOutput, error) {
		hc := ctx.Value(capabilityContextObservation{}).(huma.Context)
		body, err := io.ReadAll(hc.BodyReader())
		if err != nil {
			return nil, err
		}
		var headers []string
		hc.EachHeader(func(k, v string) { headers = append(headers, strings.ToLower(k)+":"+v) })
		sortStrings(headers)
		op := hc.Operation()
		url := hc.URL()
		out := &capabilityContextOutput{}
		out.Body.Observation = map[string]any{
			"operation": op.OperationID, "method": hc.Method(), "host": hc.Host(), "remote": hc.RemoteAddr(),
			"url_path": url.Path, "raw_query": url.RawQuery, "param": hc.Param("id"), "query": hc.Query("q"),
			"header": hc.Header("X-Probe"), "headers": headers, "body_reader": string(body),
			"override": ctx.Value("override"), "protocol": hc.Version().Proto, "protocol_major": hc.Version().ProtoMajor, "protocol_minor": hc.Version().ProtoMinor,
		}
		hc.SetHeader("X-Probe-Write", "set")
		hc.AppendHeader("X-Probe-Write", "append")
		if hc.Status() != 0 {
			return nil, huma.Error500InternalServerError("unexpected preexisting status")
		}
		hc.SetStatus(http.StatusAccepted)
		_, _ = hc.BodyWriter().Write([]byte(`{"direct":true}`))
		return out, nil
	})
	return nil
}

func capabilityContextHandler(t *testing.T, build bool) http.Handler {
	t.Helper()
	c := huma.DefaultConfig("Context", "1.0.0")
	if !build {
		r := http.NewServeMux()
		if err := registerCapabilityContext(humago.New(r, c)); err != nil {
			t.Fatal(err)
		}
		return r
	}

	result, err := testkit.NewAPIHandler(testkit.Spec{Config: c, Native: []testkit.Registration{{Register: registerCapabilityContext}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

func TestCapabilityContextMethodsNativeHTTPBaseline(t *testing.T) {
	var docs [2]any
	for i, h := range []http.Handler{capabilityContextHandler(t, false), capabilityContextHandler(t, true)} {
		r := httptest.NewRequest(http.MethodPost, "http://context.example/context/a%2Fb?q=hello+world", strings.NewReader("request-body"))
		r.Host = "context.example:8443"
		r.RemoteAddr = "192.0.2.9:4321"
		r.Header.Add("X-Probe", "one")
		r.Header.Add("X-Probe", "two")
		r.Proto = "HTTP/1.1"
		r.ProtoMajor = 1
		r.ProtoMinor = 1
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		body := w.Body.String()
		const direct = `{"direct":true}`
		if w.Code != http.StatusAccepted || !reflect.DeepEqual(w.Header().Values("X-Probe-Write"), []string{"set", "append"}) || !strings.HasPrefix(body, direct) {
			t.Fatalf("arm %d response status=%d headers=%v body=%q", i, w.Code, w.Header(), body)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(body, direct)), &payload); err != nil {
			t.Fatalf("arm %d serialized context body: %v", i, err)
		}
		got := payload["observation"].(map[string]any)
		want := map[string]any{"operation": "context", "method": "POST", "host": "context.example:8443", "remote": "192.0.2.9:4321", "url_path": "/context/a/b", "raw_query": "q=hello+world", "param": "a/b", "query": "hello world", "header": "one", "headers": []any{"x-probe:one", "x-probe:two"}, "body_reader": "request-body", "override": "visible", "protocol": "HTTP/1.1", "protocol_major": float64(1), "protocol_minor": float64(1)}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("arm %d context observation=%v want=%v", i, got, want)
		}
		docs[i] = captureCapabilityContextOpenAPI(t, h)
	}
	if !reflect.DeepEqual(docs[0], docs[1]) {
		t.Fatal("native and Build OpenAPI differ")
	}
}

func TestCapabilityQueryParamEdgesNativeHTTPBaseline(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"q=first&q=second", "first"},
		{"q=", ""},
		{"q", "true"},
		{"%71=escaped", "escaped"},
		{"other=value", ""},
	} {
		var responses [2]string
		var fullJSON [2]map[string]any
		var headers [2]http.Header
		var statuses [2]int
		for i, h := range []http.Handler{capabilityContextHandler(t, false), capabilityContextHandler(t, true)} {
			r := httptest.NewRequest(http.MethodPost, "/context/id?"+tc.raw, strings.NewReader(""))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusAccepted {
				t.Fatalf("query=%q arm=%d status=%d body=%s", tc.raw, i, w.Code, w.Body.String())
			}
			statuses[i], headers[i] = w.Code, w.Header().Clone()
			const direct = `{"direct":true}`
			var payload map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(w.Body.String(), direct)), &payload); err != nil {
				t.Fatal(err)
			}
			fullJSON[i] = payload
			obs := payload["observation"].(map[string]any)
			responses[i], _ = obs["query"].(string)
		}
		if !reflect.DeepEqual(fullJSON[0], fullJSON[1]) || statuses[0] != statuses[1] || !reflect.DeepEqual(headers[0], headers[1]) {
			t.Fatalf("full response differs: native=%v Build=%v", fullJSON[0], fullJSON[1])
		}
		if responses[0] != tc.want || responses[1] != tc.want || responses[0] != responses[1] {
			t.Fatalf("raw query=%q results=%v want=%q", tc.raw, responses, tc.want)
		}
	}
	var docs [2]any
	for i, h := range []http.Handler{capabilityContextHandler(t, false), capabilityContextHandler(t, true)} {
		docs[i] = captureCapabilityContextOpenAPI(t, h)
	}
	if !reflect.DeepEqual(docs[0], docs[1]) {
		t.Fatal("queryparam case OpenAPI differs")
	}
}

func TestCapabilityHumagoContextAdapterEntriesBaseline(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	state := &tls.ConnectionState{Version: tls.VersionTLS12, ServerName: "context.example"}
	r.TLS = state
	r.Proto, r.ProtoMajor, r.ProtoMinor = "HTTP/2.0", 2, 0
	w := httptest.NewRecorder()
	ctx := humago.NewContext(&huma.Operation{OperationID: "manual"}, r, w)
	gotReq, gotWriter := humago.Unwrap(ctx)
	if gotReq != r || gotWriter != w || ctx.Operation().OperationID != "manual" || ctx.TLS() != state || ctx.Version() != (huma.ProtoVersion{Proto: "HTTP/2.0", ProtoMajor: 2}) {
		t.Fatalf("NewContext/Unwrap observation mismatch")
	}
	if gotReq2, gotWriter2 := humago.Unwrap(huma.WithValue(ctx, "nested", true)); gotReq2 != r || gotWriter2 != w {
		t.Fatalf("Unwrap failed through WithValue")
	}
	if err := ctx.SetReadDeadline(time.Now().Add(time.Minute)); err == nil || err.Error() != "feature not supported" {
		t.Fatalf("deadline fallback error=%v", err)
	}
	for _, tc := range []struct {
		name, contentType, body string
		wantErr                 bool
	}{
		{"valid", "multipart/form-data; boundary=b", "--b\r\nContent-Disposition: form-data; name=\"field\"\r\n\r\nvalue\r\n--b--\r\n", false},
		{"malformed", "multipart/form-data; boundary=b", "not multipart", true},
	} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", tc.contentType)
		hc := humago.NewContext(&huma.Operation{}, r, httptest.NewRecorder())
		form, err := hc.GetMultipartForm()
		if (err != nil) != tc.wantErr {
			t.Fatalf("case=%s form=%v err=%v", tc.name, form, err)
		}
		if !tc.wantErr && form.Value["field"][0] != "value" {
			t.Fatalf("case=%s form=%v", tc.name, form.Value)
		}
	}
}

func captureCapabilityContextOpenAPI(t *testing.T, h http.Handler) any {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("OpenAPI status=%d", w.Code)
	}
	var d any
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
