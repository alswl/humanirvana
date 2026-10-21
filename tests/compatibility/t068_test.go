package demo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	testkit "github.com/alswl/humanirvana/tests/compatibility/internal/testkit"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type t068Output struct {
	Body struct {
		Value string `json:"value"`
	} `json:"body"`
}
type t068Input struct {
	Body struct {
		Fail bool `json:"fail" validate:"required"`
	} `json:"body"`
}
type t068Key struct{}
type t068CustomDetail struct {
	Message  string `json:"message"`
	Location string `json:"location"`
}

func (e t068CustomDetail) Error() string { return e.Message }
func (e t068CustomDetail) ErrorDetail() *huma.ErrorDetail {
	return &huma.ErrorDetail{Message: e.Message, Location: e.Location}
}

func t068Config(t *testing.T) huma.Config {
	c := huma.DefaultConfig("T068", "1.0.0")
	return c
}

func t068BuildHandler(t *testing.T, build bool, apiMW func(huma.Context, func(huma.Context)), opMW func(huma.Context, func(huma.Context))) http.Handler {
	return t068BuildCustomHandler(t, build, apiMW, opMW, "")
}

func t068BuildCustomHandler(t *testing.T, build bool, apiMW func(huma.Context, func(huma.Context)), opMW func(huma.Context, func(huma.Context)), mode string) http.Handler {
	t.Helper()
	register := func(api huma.API) error {
		if apiMW != nil {
			api.UseMiddleware(apiMW)
		}
		op := huma.Operation{Method: http.MethodPost, Path: "/errors", OperationID: "errors"}
		if opMW != nil {
			op.Middlewares = huma.Middlewares{opMW}
		}
		huma.Register[t068Input, t068Output](api, op, func(ctx context.Context, in *t068Input) (*t068Output, error) {
			out := &t068Output{}
			if in.Body.Fail && mode == "custom-model" {
				return nil, &huma.ErrorModel{Status: http.StatusUnprocessableEntity, Title: "Custom", Detail: "custom detail", Errors: []*huma.ErrorDetail{{Message: "bad value", Location: "body.fail"}}}
			}
			if in.Body.Fail && mode == "panic" {
				panic("t068-handler-panic")
			}
			if in.Body.Fail {
				return nil, huma.ErrorWithHeaders(huma.NewError(http.StatusConflict, "business conflict", errors.New("conflict detail")), http.Header{"X-Error": []string{"business"}})
			}
			out.Body.Value, _ = ctx.Value(t068Key{}).(string)
			return out, nil
		})
		return nil
	}
	r := http.NewServeMux()
	if !build {
		register(humago.New(r, t068Config(t)))
		return r
	}
	result, err := testkit.NewAPIHandler(testkit.Spec{Config: t068Config(t), Native: []testkit.Registration{{Register: register}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

func t068Middleware(events *[]string, label string, mutate bool) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		*events = append(*events, label+":in")
		if mutate {
			ctx = huma.WithValue(ctx, t068Key{}, "request-value")
		}
		next(ctx)
		*events = append(*events, label+":out")
	}
}

func TestT068ErrorsMiddlewareContextNativeHTTPBaseline(t *testing.T) {
	for _, tc := range []struct {
		name            string
		fail            bool
		wantStatus      int
		wantErrorHeader string
	}{
		{"success context value", false, http.StatusOK, ""},
		{"custom status error with header", true, http.StatusConflict, "business"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var nativeEvents, buildEvents []string
			apiN, opN := t068Middleware(&nativeEvents, "api", true), t068Middleware(&nativeEvents, "op", false)
			apiB, opB := t068Middleware(&buildEvents, "api", true), t068Middleware(&buildEvents, "op", false)
			var results []struct {
				code   int
				header http.Header
				body   map[string]any
			}
			for _, h := range []http.Handler{t068BuildHandler(t, false, apiN, opN), t068BuildHandler(t, true, apiB, opB)} {
				body := `{"fail":false}`
				if tc.fail {
					body = `{"fail":true}`
				}
				r := httptest.NewRequest(http.MethodPost, "/errors", strings.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				got := map[string]any{}
				_ = json.Unmarshal(w.Body.Bytes(), &got)
				results = append(results, struct {
					code   int
					header http.Header
					body   map[string]any
				}{w.Code, w.Header().Clone(), got})
			}
			if results[0].code != tc.wantStatus || results[1].code != tc.wantStatus || !reflect.DeepEqual(results[0].body, results[1].body) || results[0].header.Get("X-Error") != tc.wantErrorHeader || results[1].header.Get("X-Error") != tc.wantErrorHeader {
				t.Fatalf("native=%+v Build=%+v", results[0], results[1])
			}
			wantOrder := []string{"api:in", "op:in", "op:out", "api:out"}
			if !reflect.DeepEqual(nativeEvents, wantOrder) || !reflect.DeepEqual(buildEvents, wantOrder) {
				t.Fatalf("orders native=%v Build=%v", nativeEvents, buildEvents)
			}
		})
	}
}

func TestT068MiddlewareShortCircuitAndConcurrentContextIsolation(t *testing.T) {
	short := func(events *[]string) func(huma.Context, func(huma.Context)) {
		return func(ctx huma.Context, next func(huma.Context)) {
			*events = append(*events, "short")
			ctx.SetStatus(http.StatusForbidden)
			_, _ = ctx.BodyWriter().Write([]byte(`{"denied":true}`))
		}
	}
	for _, build := range []bool{false, true} {
		var events []string
		h := t068BuildHandler(t, build, short(&events), nil)
		r := httptest.NewRequest(http.MethodPost, "/errors", strings.NewReader(`{"fail":false}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden || w.Body.String() != `{"denied":true}` || !reflect.DeepEqual(events, []string{"short"}) {
			t.Fatalf("Build=%v status=%d body=%q events=%v", build, w.Code, w.Body.String(), events)
		}
	}
	var mu sync.Mutex
	for _, build := range []bool{false, true} {
		opMW := func(ctx huma.Context, next func(huma.Context)) {
			next(huma.WithValue(ctx, t068Key{}, ctx.Context().Value("marker")))
		}
		h := t068BuildHandler(t, build, nil, opMW)
		const n = 16
		var wg sync.WaitGroup
		errs := make(chan string, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				want := fmt.Sprintf("request-%d", i)
				r := httptest.NewRequest(http.MethodPost, "/errors", strings.NewReader(`{"fail":false}`)).WithContext(context.WithValue(context.Background(), "marker", want))
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				var body map[string]any
				_ = json.Unmarshal(w.Body.Bytes(), &body)
				if w.Code != http.StatusOK || body["value"] != want {
					mu.Lock()
					errs <- fmt.Sprintf("Build=%v want=%s status=%d body=%v", build, want, w.Code, body)
					mu.Unlock()
				}
			}(i)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
	}
}

func TestT068CustomErrorModelAndErrorDetailerNativeHTTPBaseline(t *testing.T) {
	for _, build := range []bool{false, true} {
		h := t068BuildCustomHandler(t, build, nil, nil, "custom-model")
		r := httptest.NewRequest(http.MethodPost, "/errors", strings.NewReader(`{"fail":true}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var got map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusUnprocessableEntity || got["title"] != "Custom" || got["detail"] != "custom detail" {
			t.Fatalf("Build=%v status=%d body=%v", build, w.Code, got)
		}
		list := got["errors"].([]any)
		item := list[0].(map[string]any)
		if item["message"] != "bad value" || item["location"] != "body.fail" {
			t.Fatalf("Build=%v custom details=%v", build, item)
		}
	}
}

func TestT068HandlerPanicBoundaryNativeHTTPBaseline(t *testing.T) {
	for _, build := range []bool{false, true} {
		h := t068BuildCustomHandler(t, build, nil, nil, "panic")
		var recovered any
		func() {
			defer func() { recovered = recover() }()
			r := httptest.NewRequest(http.MethodPost, "/errors", strings.NewReader(`{"fail":true}`))
			r.Header.Set("Content-Type", "application/json")
			h.ServeHTTP(httptest.NewRecorder(), r)
		}()
		if recovered != "t068-handler-panic" {
			t.Fatalf("Build=%v panic=%v", build, recovered)
		}
	}
}

func TestT068CommittedResponseThenHandlerErrorDoesNotWriteSecondBody(t *testing.T) {
	for _, build := range []bool{false, true} {
		mw := func(ctx huma.Context, next func(huma.Context)) {
			ctx.SetStatus(http.StatusAccepted)
			_, _ = ctx.BodyWriter().Write([]byte("committed"))
			next(ctx)
		}
		h := t068BuildHandler(t, build, mw, nil)
		r := httptest.NewRequest(http.MethodPost, "/errors", strings.NewReader(`{"fail":true}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusAccepted || !strings.HasPrefix(w.Body.String(), "committed") || !strings.Contains(w.Body.String(), `"status":409`) {
			t.Fatalf("Build=%v status=%d body=%q", build, w.Code, w.Body.String())
		}
	}
}

func TestT068DeadlineAndCancellationContextNativeHTTPBaseline(t *testing.T) {
	for _, build := range []bool{false, true} {
		seen := make(chan error, 2)
		mw := func(ctx huma.Context, next func(huma.Context)) {
			if d, ok := ctx.Context().Deadline(); !ok || time.Until(d) > 0 {
				seen <- errors.New("deadline missing or not expired")
				return
			}
			seen <- nil
			next(ctx)
		}
		h := t068BuildHandler(t, build, mw, nil)
		base, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		r := httptest.NewRequest(http.MethodPost, "/errors", strings.NewReader(`{"fail":false}`)).WithContext(base)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		select {
		case err := <-seen:
			if err != nil {
				t.Fatalf("Build=%v %v", build, err)
			}
		default:
			t.Fatalf("Build=%v deadline middleware not invoked", build)
		}
		if w.Code != http.StatusOK {
			t.Fatalf("Build=%v expired-deadline pass-through status=%d body=%q", build, w.Code, w.Body.String())
		}
		cancelled, cancelReq := context.WithCancel(context.Background())
		cancelReq()
		cancelMW := func(ctx huma.Context, next func(huma.Context)) {
			if !errors.Is(ctx.Context().Err(), context.Canceled) {
				seen <- errors.New("cancellation missing")
				return
			}
			ctx.SetStatus(http.StatusRequestTimeout)
			_, _ = ctx.BodyWriter().Write([]byte("cancelled"))
		}
		h = t068BuildHandler(t, build, cancelMW, nil)
		r = httptest.NewRequest(http.MethodPost, "/errors", strings.NewReader(`{"fail":false}`)).WithContext(cancelled)
		r.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		select {
		case err := <-seen:
			if err != nil {
				t.Fatalf("Build=%v %v", build, err)
			}
		default:
		}
		if w.Code != http.StatusRequestTimeout || w.Body.String() != "cancelled" {
			t.Fatalf("Build=%v cancel response=%d %q", build, w.Code, w.Body.String())
		}
	}
}

func TestT068ValidationErrorNativeHTTPBaseline(t *testing.T) {
	for _, build := range []bool{false, true} {
		h := t068BuildHandler(t, build, nil, nil)
		r := httptest.NewRequest(http.MethodPost, "/errors", strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var got map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusUnprocessableEntity || got["status"] != float64(http.StatusUnprocessableEntity) || got["title"] != "Unprocessable Entity" {
			t.Fatalf("Build=%v validation status=%d body=%v", build, w.Code, got)
		}
		errs, ok := got["errors"].([]any)
		if !ok || len(errs) == 0 {
			t.Fatalf("Build=%v validation details=%v", build, got)
		}
	}
}

func TestT068MiddlewarePanicPropagatesNativeHTTPBaseline(t *testing.T) {
	for _, build := range []bool{false, true} {
		mw := func(huma.Context, func(huma.Context)) { panic("t068-middleware-panic") }
		h := t068BuildHandler(t, build, mw, nil)
		var got any
		func() {
			defer func() { got = recover() }()
			r := httptest.NewRequest(http.MethodPost, "/errors", strings.NewReader(`{"fail":false}`))
			r.Header.Set("Content-Type", "application/json")
			h.ServeHTTP(httptest.NewRecorder(), r)
		}()
		if got != "t068-middleware-panic" {
			t.Fatalf("Build=%v middleware panic=%v", build, got)
		}
	}
}
