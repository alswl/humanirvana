package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	testkit "github.com/alswl/humanirvana/tests/compatibility/internal/testkit"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type capabilityMultipartInput struct {
	RawBody huma.MultipartFormFiles[struct {
		Label string        `form:"label" required:"true"`
		File  huma.FormFile `form:"file" contentType:"text/plain" required:"true"`
	}]
}

type capabilityMultipartOutput struct {
	Body struct {
		Label    string `json:"label"`
		Filename string `json:"filename"`
		Mime     string `json:"mime"`
		Content  string `json:"content"`
	} `json:"body"`
}

func registerCapabilityMultipart(api huma.API) error {
	huma.Register[capabilityMultipartInput, capabilityMultipartOutput](api, huma.Operation{Method: http.MethodPost, Path: "/multipart", OperationID: "multipart"}, func(_ context.Context, in *capabilityMultipartInput) (*capabilityMultipartOutput, error) {
		data := in.RawBody.Data()
		content, err := io.ReadAll(data.File)
		if err != nil {
			return nil, err
		}
		out := &capabilityMultipartOutput{}
		out.Body.Label = data.Label
		out.Body.Filename = data.File.Filename
		out.Body.Mime = data.File.ContentType
		out.Body.Content = string(content)
		return out, nil
	})
	return nil
}

func capabilityMultipartHandler(t *testing.T, build bool) http.Handler {
	t.Helper()
	c := huma.DefaultConfig("Multipart", "1.0.0")
	if !build {
		r := http.NewServeMux()
		if err := registerCapabilityMultipart(humago.New(r, c)); err != nil {
			t.Fatal(err)
		}
		return r
	}

	result, err := testkit.NewAPIHandler(testkit.Spec{Config: c, Native: []testkit.Registration{{Register: registerCapabilityMultipart}}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Handler
}

func capabilityMultipartRequest(t *testing.T, withFile bool) (*http.Request, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("label", "sample"); err != nil {
		t.Fatal(err)
	}
	if withFile {
		h, err := w.CreatePart(textproto.MIMEHeader{
			"Content-Disposition": {`form-data; name="file"; filename="hello.txt"`},
			"Content-Type":        {"text/plain"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.Write([]byte("hello upload")); err != nil {
			t.Fatal(err)
		}
	}
	boundary := w.Boundary()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/multipart", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	return r, boundary
}

func TestCapabilityMultipartFormFileNativeHTTPBaseline(t *testing.T) {
	t.Run("form scalar and file metadata/content", func(t *testing.T) {
		type result struct {
			code int
			head http.Header
			body string
		}
		var got [2]result
		for i, h := range []http.Handler{capabilityMultipartHandler(t, false), capabilityMultipartHandler(t, true)} {
			r, _ := capabilityMultipartRequest(t, true)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			got[i] = result{w.Code, w.Header().Clone(), w.Body.String()}
		}
		if got[0].code != http.StatusOK || got[1].code != got[0].code || got[1].head.Get("Content-Type") != got[0].head.Get("Content-Type") || got[1].body != got[0].body {
			t.Fatalf("native=%+v Build=%+v", got[0], got[1])
		}
		var body struct {
			Label    string `json:"label"`
			Filename string `json:"filename"`
			Mime     string `json:"mime"`
			Content  string `json:"content"`
		}
		if err := json.Unmarshal([]byte(got[0].body), &body); err != nil {
			t.Fatal(err)
		}
		if body.Label != "sample" || body.Filename != "hello.txt" || body.Mime != "text/plain" || body.Content != "hello upload" {
			t.Fatalf("decoded upload=%+v raw=%s", body, got[0].body)
		}
	})
	t.Run("required file error", func(t *testing.T) {
		for i, h := range []http.Handler{capabilityMultipartHandler(t, false), capabilityMultipartHandler(t, true)} {
			r, _ := capabilityMultipartRequest(t, false)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			var body huma.ErrorModel
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusUnprocessableEntity || len(body.Errors) == 0 {
				t.Fatalf("arm=%d status=%d error=%+v", i, w.Code, body)
			}
		}
	})
	t.Run("multipart OpenAPI encoding", func(t *testing.T) {
		for i, h := range []http.Handler{capabilityMultipartHandler(t, false), capabilityMultipartHandler(t, true)} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
			var doc map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
			content := doc["paths"].(map[string]any)["/multipart"].(map[string]any)["post"].(map[string]any)["requestBody"].(map[string]any)["content"].(map[string]any)["multipart/form-data"].(map[string]any)
			encoding := content["encoding"].(map[string]any)
			file := encoding["file"].(map[string]any)
			if file["contentType"] != "text/plain" {
				t.Fatalf("arm=%d file encoding=%v", i, file)
			}
		}
	})
}
