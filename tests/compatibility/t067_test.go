package demo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	testkit "github.com/alswl/humanirvana/tests/compatibility/internal/testkit"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type t067Input struct {
	Body *struct {
		Mode string `json:"mode"`
	} `json:"body"`
}
type t067Output struct {
	Status  int    `json:"status,omitempty"`
	XResult string `header:"X-Result"`
	Body    struct {
		Mode string `json:"mode"`
	} `json:"body"`
}
type t067EmptyOutput struct {
	Status int    `json:"status,omitempty"`
	XEmpty string `header:"X-Empty"`
}
type t067NoStatusOutput struct {
	XResult string `header:"X-Result"`
	Body    struct {
		OK bool `json:"ok"`
	} `json:"body"`
}
type t067EmptyInput struct{}

type t067FormatOutput struct {
	ContentType string    `header:"Content-Type"`
	Date        time.Time `header:"Last-Modified" timeFormat:"Mon, 02 Jan 2006 15:04:05 MST"`
	Trace       []string  `header:"X-Trace"`
	Body        struct {
		Value string `json:"value"`
	} `json:"body"`
}

type t067BadOutput struct {
	Body chan int `json:"body"`
}

func t067Handler(_ context.Context, in *t067Input) (*t067Output, error) {
	out := &t067Output{}
	out.Status = http.StatusCreated
	out.XResult = "created"
	if in.Body != nil {
		out.Body.Mode = in.Body.Mode
	}
	return out, nil
}
func t067EmptyHandler(context.Context, *t067EmptyInput) (*t067EmptyOutput, error) {
	out := &t067EmptyOutput{}
	out.Status = http.StatusNoContent
	out.XEmpty = "none"
	return out, nil
}
func t067DefaultHandler(context.Context, *t067EmptyInput) (*t067NoStatusOutput, error) {
	out := &t067NoStatusOutput{}
	out.XResult = "default"
	out.Body.OK = true
	return out, nil
}
func t067FormatHandler(context.Context, *t067EmptyInput) (*t067FormatOutput, error) {
	out := &t067FormatOutput{ContentType: "application/json", Date: time.Date(2026, 10, 20, 1, 2, 3, 0, time.UTC), Trace: []string{"one", "two"}}
	out.Body.Value = "formatted"
	return out, nil
}
func t067BadHandler(context.Context, *t067EmptyInput) (*t067BadOutput, error) {
	return &t067BadOutput{Body: make(chan int)}, nil
}
func t067Config() huma.Config { return huma.DefaultConfig("T067 output", "1.0.0") }

func t067Register(api huma.API) {
	huma.Register[t067Input, t067Output](api, huma.Operation{Method: http.MethodPost, Path: "/output", OperationID: "output", DefaultStatus: http.StatusAccepted}, t067Handler)
	huma.Register[t067EmptyInput, t067EmptyOutput](api, huma.Operation{Method: http.MethodDelete, Path: "/empty", OperationID: "empty", DefaultStatus: http.StatusNoContent}, t067EmptyHandler)
	huma.Register[t067EmptyInput, t067NoStatusOutput](api, huma.Operation{Method: http.MethodGet, Path: "/default", OperationID: "default", DefaultStatus: http.StatusAccepted}, t067DefaultHandler)
	huma.Register[t067EmptyInput, t067FormatOutput](api, huma.Operation{Method: http.MethodGet, Path: "/format-output", OperationID: "format-output"}, t067FormatHandler)
	huma.Register[t067EmptyInput, t067BadOutput](api, huma.Operation{Method: http.MethodGet, Path: "/marshal-failure", OperationID: "marshal-failure"}, t067BadHandler)
}
func t067Native(t *testing.T) http.Handler {
	t.Helper()
	r := http.NewServeMux()
	api := humago.New(r, t067Config())
	t067Register(api)
	return r
}
func t067Build(t *testing.T) http.Handler {
	t.Helper()
	result, err := testkit.NewAPIHandler(testkit.Spec{Config: t067Config(), Native: []testkit.Registration{{Register: func(api huma.API) error { t067Register(api); return nil }}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

func TestT067OutputHeaderFormatsAndMarshalFailureNativeHTTPBaseline(t *testing.T) {
	for _, arm := range []struct {
		name    string
		handler http.Handler
	}{{"native", t067Native(t)}, {"Build", t067Build(t)}} {
		t.Run(arm.name, func(t *testing.T) {
			code, headers, body := t067Call(arm.handler, http.MethodGet, "/format-output", "")
			if code != http.StatusOK || headers.Get("Content-Type") != "application/json" || headers.Values("X-Trace")[0] != "one" || headers.Values("X-Trace")[1] != "two" || headers.Get("Last-Modified") != "Tue, 20 Oct 2026 01:02:03 UTC" {
				t.Fatalf("formatted output status=%d headers=%v body=%q", code, headers, body)
			}
			var got map[string]any
			if err := json.Unmarshal([]byte(body), &got); err != nil || got["value"] != "formatted" {
				t.Fatalf("body=%q decoded=%v err=%v", body, got, err)
			}
			var panicValue any
			func() {
				defer func() { panicValue = recover() }()
				code, _, body = t067Call(arm.handler, http.MethodGet, "/marshal-failure", "")
			}()
			if panicValue == nil || !strings.Contains(panicValue.(error).Error(), "error marshaling response") {
				t.Fatalf("marshal panic=%v", panicValue)
			}
		})
	}
}
func t067Call(h http.Handler, method, path, body string) (int, http.Header, string) {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code, w.Header().Clone(), w.Body.String()
}

func TestT067OutputStatusHeaderAndBodyNativeHTTPBaseline(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
		header                   string
		response                 string
	}{
		{"body status overrides operation default", http.MethodPost, "/output", `{"mode":"fast"}`, http.StatusCreated, "created", `{"$schema":"https://example.com/schemas/T067OutputBody.json","mode":"fast"}`},
		{"empty response and header", http.MethodDelete, "/empty", `{}`, http.StatusNoContent, "none", ""},
		{"default status with body", http.MethodGet, "/default", "", http.StatusAccepted, "default", `{"$schema":"https://example.com/schemas/T067NoStatusOutputBody.json","ok":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, arm := range []struct {
				name    string
				handler http.Handler
			}{{"native", t067Native(t)}, {"Build", t067Build(t)}} {
				t.Run(arm.name, func(t *testing.T) {
					code, headers, body := t067Call(arm.handler, tc.method, tc.path, tc.body)
					if code != tc.status || headers.Get("X-Result") != tc.header && headers.Get("X-Empty") != tc.header {
						t.Fatalf("status=%d headers=%v", code, headers)
					}
					if tc.response == "" {
						if body != "" {
							t.Fatalf("unexpected body=%q", body)
						}
					} else {
						var got map[string]any
						if err := json.Unmarshal([]byte(body), &got); err != nil {
							t.Fatal(err)
						}
						var want map[string]any
						if err := json.Unmarshal([]byte(tc.response), &want); err != nil {
							t.Fatal(err)
						}
						if !reflect.DeepEqual(got, want) {
							t.Fatalf("response=%v want=%v", got, want)
						}
						if tc.path == "/default" && got["ok"] != true {
							t.Fatalf("body=%v", got)
						}
					}
				})
			}
			codeN, headN, bodyN := t067Call(t067Native(t), tc.method, tc.path, tc.body)
			codeB, headB, bodyB := t067Call(t067Build(t), tc.method, tc.path, tc.body)
			if codeN != codeB || headN.Get("X-Result") != headB.Get("X-Result") || headN.Get("X-Empty") != headB.Get("X-Empty") || bodyN != bodyB {
				t.Fatalf("native=(%d,%v,%q) Build=(%d,%v,%q)", codeN, headN, bodyN, codeB, headB, bodyB)
			}
		})
	}
}

func TestT067OutputOpenAPISchemasAndResponses(t *testing.T) {
	for _, arm := range []struct {
		name    string
		handler http.Handler
	}{{"native", t067Native(t)}, {"Build", t067Build(t)}} {
		t.Run(arm.name, func(t *testing.T) {
			code, _, raw := t067Call(arm.handler, http.MethodGet, "/openapi.json", "")
			if code != 200 {
				t.Fatalf("status=%d", code)
			}
			var doc map[string]any
			if err := json.Unmarshal([]byte(raw), &doc); err != nil {
				t.Fatal(err)
			}
			paths := doc["paths"].(map[string]any)
			post := paths["/output"].(map[string]any)["post"].(map[string]any)
			if post["responses"].(map[string]any)["202"] == nil || post["responses"].(map[string]any)["201"] != nil {
				t.Fatalf("status responses=%v", post["responses"])
			}
			resp := post["responses"].(map[string]any)["202"].(map[string]any)
			if resp["headers"].(map[string]any)["X-Result"] == nil {
				t.Fatalf("header schema missing: %v", resp)
			}
			if paths["/empty"].(map[string]any)["delete"].(map[string]any)["responses"].(map[string]any)["204"] == nil {
				t.Fatal("empty response status missing")
			}
		})
	}
}
