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
}

var _ huma.Context = (*humaContext)(nil)

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
	if value == "" && c.route != c.op.Path {
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
	if err := c.request.ParseMultipartForm(8 << 10); err != nil {
		return c.request.MultipartForm, err
	}
	return c.request.MultipartForm, nil
}
func (c *humaContext) SetReadDeadline(deadline time.Time) error {
	return huma.SetReadDeadline(c.response, deadline)
}
func (c *humaContext) SetStatus(status int)            { c.status = status; c.response.WriteHeader(status) }
func (c *humaContext) Status() int                     { return c.status }
func (c *humaContext) SetHeader(name, value string)    { c.response.Header().Set(name, value) }
func (c *humaContext) AppendHeader(name, value string) { c.response.Header().Add(name, value) }
func (c *humaContext) BodyWriter() io.Writer           { return c.response }
