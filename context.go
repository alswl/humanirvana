package humanirvana

import (
	"context"
	"crypto/tls"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/caicloud/nirvana/service"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/queryparam"
)

type humaContext struct {
	op       *huma.Operation
	route    string
	ctx      context.Context
	request  *http.Request
	response http.ResponseWriter
	values   service.ValueContainer
	status   int
	formRead bool
	formErr  error
}

var _ huma.Context = (*humaContext)(nil)

// Unwrap extracts the Nirvana request and response writer, including through
// huma.WithContext/WithValue wrappers. It panics for another adapter's context.
// The writer is Nirvana's tracking writer, not the original server writer.
// Huma context overrides are available via ctx.Context(), not request.Context().
func Unwrap(ctx huma.Context) (*http.Request, http.ResponseWriter) {
	for {
		if wrapped, ok := ctx.(interface{ Unwrap() huma.Context }); ok {
			ctx = wrapped.Unwrap()
			continue
		}
		if c, ok := ctx.(*humaContext); ok {
			return c.request, c.response
		}
		panic("not a humanirvana context")
	}
}

func newContext(op *huma.Operation, route string, ctx context.Context, request *http.Request, response http.ResponseWriter, values service.ValueContainer) *humaContext {
	return &humaContext{op: op, route: route, ctx: ctx, request: request, response: response, values: values}
}

func (c *humaContext) Operation() *huma.Operation { return c.op }
func (c *humaContext) Context() context.Context   { return c.ctx }
func (c *humaContext) TLS() *tls.ConnectionState  { return c.request.TLS }
func (c *humaContext) Version() huma.ProtoVersion {
	return huma.ProtoVersion{Proto: c.request.Proto, ProtoMajor: c.request.ProtoMajor, ProtoMinor: c.request.ProtoMinor}
}
func (c *humaContext) Method() string     { return c.request.Method }
func (c *humaContext) Host() string       { return c.request.Host }
func (c *humaContext) RemoteAddr() string { return c.request.RemoteAddr }
func (c *humaContext) URL() url.URL       { return *c.request.URL }
func (c *humaContext) Param(name string) string {
	value, _ := c.values.Get(name)
	// Shared templates can swap parameter names between methods.
	if c.route != c.op.Path {
		opParams := routeParamNames(c.op.Path)
		matchedParams := routeParamNames(c.route)
		for i, param := range opParams {
			if param == name && i < len(matchedParams) {
				value, _ = c.values.Get(matchedParams[i])
				break
			}
		}
	}
	if c.request.URL.RawPath != "" {
		if decoded, err := url.PathUnescape(value); err == nil {
			return decoded
		}
	}
	return value
}

func routeParamNames(path string) []string {
	var names []string
	for _, segment := range strings.Split(path, "/") {
		if len(segment) >= 3 && segment[0] == '{' && segment[len(segment)-1] == '}' {
			names = append(names, strings.Trim(segment, "{}"))
		}
	}
	return names
}
func (c *humaContext) Query(name string) string  { return queryparam.Get(c.request.URL.RawQuery, name) }
func (c *humaContext) Header(name string) string { return c.request.Header.Get(name) }
func (c *humaContext) EachHeader(callback func(name, value string)) {
	for name, values := range c.request.Header {
		for _, value := range values {
			callback(name, value)
		}
	}
}
func (c *humaContext) BodyReader() io.Reader { return c.request.Body }
func (c *humaContext) GetMultipartForm() (*multipart.Form, error) {
	// Retrying a partially consumed body can lose the original parse error.
	if !c.formRead {
		c.formRead = true
		// ParseMultipartForm's memory threshold does not limit the total body.
		if c.op.MaxBodyBytes > 0 {
			c.request.Body = http.MaxBytesReader(c.response, c.request.Body, c.op.MaxBodyBytes)
		}
		c.formErr = c.request.ParseMultipartForm(8 << 10)
		if c.formErr == nil && c.op.MaxBodyBytes > 0 {
			// Multipart parsers may stop before the epilogue, which also counts toward the limit.
			_, c.formErr = io.Copy(io.Discard, c.request.Body)
		}
	}
	return c.request.MultipartForm, c.formErr
}
func (c *humaContext) SetReadDeadline(deadline time.Time) error {
	return huma.SetReadDeadline(c.response, deadline)
}
func (c *humaContext) SetStatus(status int)            { c.status = status; c.response.WriteHeader(status) }
func (c *humaContext) Status() int                     { return c.status }
func (c *humaContext) SetHeader(name, value string)    { c.response.Header().Set(name, value) }
func (c *humaContext) AppendHeader(name, value string) { c.response.Header().Add(name, value) }
func (c *humaContext) BodyWriter() io.Writer           { return c.response }
