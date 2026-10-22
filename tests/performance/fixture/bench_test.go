package fixture

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alswl/humanirvana"
	"github.com/caicloud/nirvana/service/rest"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

// BenchmarkServe measures in-process handler cost and allocations per frozen
// request, without a network client. It is not a P95 measurement.
func BenchmarkServe(b *testing.B) {
	cases, err := Load(testdata)
	if err != nil {
		b.Fatal(err)
	}
	for _, c := range cases {
		for _, arm := range []string{Native, Adapter} {
			h, err := NewHandler(arm)
			if err != nil {
				b.Fatal(err)
			}
			b.Run(c.ID+"/"+arm, func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					r := httptest.NewRequest(c.Method, c.Path, bytes.NewReader(c.Body))
					for k, v := range c.Header {
						r.Header.Set(k, v)
					}
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					if w.Code != c.WantStatus {
						b.Fatalf("status %d", w.Code)
					}
					_, _ = io.Copy(io.Discard, w.Body)
				}
			})
		}
	}
}

// BenchmarkRegister measures registering n equivalent operations on a fresh
// API, including building and binding the router for the adapter arm.
func BenchmarkRegister(b *testing.B) {
	register := func(api huma.API, n int) {
		for i := 0; i < n; i++ {
			huma.Register(api, huma.Operation{OperationID: fmt.Sprintf("op-%d", i), Method: http.MethodGet, Path: fmt.Sprintf("/r%d/{id}", i)},
				func(_ context.Context, in *ParamsInput) (*ParamsOutput, error) { return &ParamsOutput{}, nil })
		}
	}
	for _, n := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("%d/%s", n, Native), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				register(humago.New(http.NewServeMux(), Config()), n)
			}
		})
		b.Run(fmt.Sprintf("%d/%s", n, Adapter), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				builder := rest.NewBuilder()
				adapter := humanirvana.NewAdapter(builder)
				register(huma.NewAPI(Config(), adapter), n)
				svc, err := builder.Build()
				if err != nil {
					b.Fatal(err)
				}
				if err := adapter.Bind(svc); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
