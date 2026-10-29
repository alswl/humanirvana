package humanirvana_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

func multipartBody(t *testing.T, size int) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.SetBoundary("contract-boundary"); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteField("label", "sample"); err != nil {
		t.Fatal(err)
	}
	f, err := w.CreateFormFile("file", "upload.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(f, strings.Repeat("x", size)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), w.FormDataContentType()
}

func uploadRequest(body []byte, contentType string) *http.Request {
	r := httptest.NewRequest("POST", "/upload", bytes.NewReader(body))
	r.Header.Set("Content-Type", contentType)
	return r
}

func cleanupForm(t *testing.T, r *http.Request) {
	t.Helper()
	if r.MultipartForm != nil {
		if err := r.MultipartForm.RemoveAll(); err != nil {
			t.Error(err)
		}
	}
}

func TestMultipartTotalLimitAndMemoryThreshold(t *testing.T) {
	// Exceed the 8 KiB memory threshold to exercise spill-to-disk behavior.
	body, contentType := multipartBody(t, 32<<10)
	for _, tc := range []struct {
		name  string
		limit int64
		body  []byte
		fail  bool
	}{
		{"exact", int64(len(body)), body, false},
		{"one-byte-over", int64(len(body) - 1), body, true},
		{"epilogue-over", int64(len(body)), append(append([]byte{}, body...), []byte("epilogue")...), true},
		{"disabled", -1, body, false},
		{"direct-adapter-zero-unlimited", 0, body, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newAPI()
			h.adapter.Handle(&huma.Operation{Method: "POST", Path: "/upload", MaxBodyBytes: tc.limit}, func(ctx huma.Context) {
				form, err := ctx.GetMultipartForm()
				_, again := ctx.GetMultipartForm()
				if again != err {
					t.Error("repeated GetMultipartForm lost the parsing result")
				}
				if tc.fail {
					var tooLarge *http.MaxBytesError
					if !errors.As(err, &tooLarge) || tooLarge.Limit != tc.limit {
						t.Errorf("form=%v error=%v; want MaxBytesError(%d)", form, err, tc.limit)
					}
					ctx.SetStatus(422)
					return
				}
				if err != nil || form == nil || form.Value["label"][0] != "sample" {
					t.Fatalf("form=%v error=%v", form, err)
				}
				f, err := form.File["file"][0].Open()
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if _, onDisk := f.(*os.File); !onDisk {
					t.Error("file larger than memory threshold should spill to disk")
				}
				content, err := io.ReadAll(f)
				if err != nil || len(content) != 32<<10 {
					t.Errorf("file size=%d error=%v", len(content), err)
				}
				ctx.SetStatus(204)
			})
			h.finish(t)
			r := uploadRequest(tc.body, contentType)
			defer cleanupForm(t, r)
			w := httptest.NewRecorder()
			h.handler.ServeHTTP(w, r)
			want := 204
			if tc.fail {
				want = 422
			}
			if w.Code != want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

// Huma v2.35.0 maps multipart errors to 422; native humago needs an external limiter.
func TestMultipartTypedLimitPinnedHumaContract(t *testing.T) {
	body, contentType := multipartBody(t, 16<<10)
	largeBody, _ := multipartBody(t, 1<<20)
	for _, decoded := range []bool{false, true} {
		for _, tc := range []struct {
			name  string
			limit int64
			body  []byte
			want  int
		}{
			{"exact", int64(len(body)), body, 200},
			{"one-byte-over", int64(len(body) - 1), body, 422},
			{"disabled", -1, body, 200},
			{"zero-fits", 0, body, 200},
			{"zero-large-unlimited", 0, largeBody, 200},
			{"malformed", int64(len(body)), []byte("invalid multipart"), 422},
		} {
			t.Run(tc.name+map[bool]string{true: "/decoded", false: "/raw"}[decoded], func(t *testing.T) {
				var problems [2]huma.ErrorModel
				for arm := 0; arm < 3; arm++ {
					h := newAPI()
					api := h.api
					var handler http.Handler
					if arm != 0 {
						mux := http.NewServeMux()
						api = humago.New(mux, huma.DefaultConfig("multipart contract", "1.0"))
						handler = mux
					}
					calls := 0
					op := huma.Operation{Method: "POST", Path: "/upload", MaxBodyBytes: tc.limit}
					if decoded {
						type input struct {
							RawBody huma.MultipartFormFiles[struct {
								Label string        `form:"label"`
								File  huma.FormFile `form:"file"`
							}]
						}
						huma.Register[input, reply](api, op, func(_ context.Context, in *input) (*reply, error) {
							calls++
							defer in.RawBody.Data().File.Close()
							return &reply{Body: in.RawBody.Data().Label}, nil
						})
					} else {
						type input struct{ RawBody multipart.Form }
						huma.Register[input, reply](api, op, func(_ context.Context, in *input) (*reply, error) {
							calls++
							return &reply{Body: in.RawBody.Value["label"][0]}, nil
						})
					}
					if arm == 0 {
						h.finish(t)
						handler = h.handler
					}
					r := uploadRequest(tc.body, contentType)
					// Streaming uploads must not bypass the limit.
					r.ContentLength = -1
					if arm == 1 && tc.limit > 0 {
						r.Body = http.MaxBytesReader(nil, r.Body, tc.limit)
					}
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, r)
					cleanupForm(t, r)
					want := tc.want
					if arm == 2 && tc.name == "one-byte-over" {
						// The pinned native adapter ignores MaxBodyBytes for multipart.
						want = 200
					}
					if w.Code != want || (calls == 1) != (want == 200) {
						t.Fatalf("arm=%d status=%d calls=%d body=%s", arm, w.Code, calls, w.Body.String())
					}
					if want == 422 {
						var problem huma.ErrorModel
						if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
							t.Fatal(err)
						}
						if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/problem+json") || problem.Status != 422 || problem.Detail != "validation failed" || len(problem.Errors) != 1 || problem.Errors[0].Location != "body" {
							t.Fatalf("arm=%d problem=%+v", arm, problem)
						}
						if arm < 2 {
							problems[arm] = problem
						}
					}
				}
				if tc.want == 422 {
					a, _ := json.Marshal(problems[0])
					b, _ := json.Marshal(problems[1])
					if !bytes.Equal(a, b) {
						t.Fatalf("problem differs from pinned Huma: adapter=%s native+limiter=%s", a, b)
					}
				}
			})
		}
	}
}
