package humanirvana_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alswl/humanirvana"
	"github.com/caicloud/nirvana/service"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type contractKey string

type wrappedHumaContext interface{ huma.Context }

type contextWrapper struct{ wrappedHumaContext }

func (c contextWrapper) Unwrap() huma.Context { return c.wrappedHumaContext }

// Huma v2.35.0 context overrides leave the native request unchanged.
func TestContextWrappingContract(t *testing.T) {
	for _, native := range []bool{true, false} {
		name := "humanirvana"
		if native {
			name = "humago-v2.35.0"
		}
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				query string
				want  int
			}{
				{"?n=1", 202}, {"?n=2", 409}, {"?n=bad", 422}, {"?n=1&block=yes", 403},
			} {
				t.Run(tc.query, func(t *testing.T) {
					h := newAPI()
					api := h.api
					var handler http.Handler
					unwrap := humanirvana.Unwrap
					if native {
						mux := http.NewServeMux()
						api = humago.New(mux, huma.DefaultConfig("context contract", "1.0"))
						handler, unwrap = mux, humago.Unwrap
					}
					var originalRequest *http.Request
					var originalWriter http.ResponseWriter
					outerStatus, innerStatus, calls := 0, 0, 0
					api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
						originalRequest, originalWriter = unwrap(ctx)
						if ctx.Status() != 0 {
							t.Fatalf("initial status=%d", ctx.Status())
						}
						next(ctx)
						outerStatus = ctx.Status()
					})
					api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
						derived, cancel := context.WithDeadline(ctx.Context(), time.Now().Add(time.Minute))
						cancel()
						ctx = huma.WithContext(ctx, derived)
						ctx = huma.WithValue(ctx, contractKey("value"), "propagated")
						ctx = huma.WithValue(ctx, contractKey("nested"), true)
						next(contextWrapper{ctx})
						innerStatus = ctx.Status()
					})
					api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
						r, w := unwrap(ctx)
						if r != originalRequest || w != originalWriter {
							t.Fatal("Unwrap did not preserve the native request/writer")
						}
						if ctx.Context().Value(contractKey("value")) != "propagated" || ctx.Context().Value(contractKey("nested")) != true || ctx.Context().Err() != context.Canceled {
							t.Fatal("downstream middleware lost derived context")
						}
						if _, ok := ctx.Context().Deadline(); !ok {
							t.Fatal("derived deadline missing")
						}
						if r.Context().Value(contractKey("value")) != nil || r.Context().Err() != nil {
							t.Fatal("Huma wrapper unexpectedly modified native request context")
						}
						if !native && service.HTTPContextFrom(ctx.Context()).Request() != r {
							t.Fatal("Nirvana context inaccessible through derived context")
						}
						if ctx.Query("block") == "yes" {
							_ = huma.WriteErr(api, ctx, 403, "blocked")
							return
						}
						next(ctx)
					})
					type input struct {
						N int `query:"n"`
					}
					huma.Register[input, reply](api, huma.Operation{Method: "GET", Path: "/wrapped", DefaultStatus: 202}, func(ctx context.Context, in *input) (*reply, error) {
						calls++
						if ctx.Value(contractKey("value")) != "propagated" || ctx.Err() != context.Canceled {
							t.Fatal("typed handler lost context")
						}
						if in.N == 2 {
							return nil, huma.Error409Conflict("conflict")
						}
						return &reply{Body: "ok"}, nil
					})
					if !native {
						h.finish(t)
						handler = h.handler
					}
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, httptest.NewRequest("GET", "/wrapped"+tc.query, nil))
					wantCalls := 0
					if tc.want == 202 || tc.want == 409 {
						wantCalls = 1
					}
					if w.Code != tc.want || outerStatus != tc.want || innerStatus != tc.want || calls != wantCalls {
						t.Fatalf("HTTP=%d outer=%d inner=%d calls=%d body=%s", w.Code, outerStatus, innerStatus, calls, w.Body.String())
					}
				})
			}
		})
	}
}

func TestUnwrapRejectsForeignContext(t *testing.T) {
	for _, ctx := range []huma.Context{nil, huma.WithValue(humago.NewContext(&huma.Operation{}, httptest.NewRequest("GET", "/", nil), httptest.NewRecorder()), contractKey("foreign"), true)} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("expected foreign context panic")
				}
			}()
			humanirvana.Unwrap(ctx)
		}()
	}
}
