// Package pilot provides a constrained legacy-handler migration demo.
// It reuses Nirvana's executor; it is not the general-purpose adapter API.
package pilot

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/caicloud/nirvana/definition"
	nerrors "github.com/caicloud/nirvana/errors"
	"github.com/caicloud/nirvana/service"
	"github.com/caicloud/nirvana/service/executor"
	"github.com/danielgtaylor/huma/v2"
)

// Operation binds an original definition to its resolved descriptor path.
// Only explicit middleware lists and JSON/scalar parameters are supported.
type Operation struct {
	Path        string
	Definition  definition.Definition
	Middlewares []definition.Middleware
}

var pathParameter = regexp.MustCompile(`^\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

// Register analyzes the whole batch before registering it on a fresh Huma/Chi API.
// Callers must abort startup on error. Descriptor inheritance is resolved by the
// caller. This pilot does not support dynamic registration or arbitrary adapters.
func Register(api huma.API, operations ...Operation) error {
	type compiled struct {
		op      *huma.Operation
		prepare func()
		run     func(huma.Context)
	}
	batch := make([]compiled, 0, len(operations))
	seen := map[string]bool{}
	for _, operation := range operations {
		d := operation.Definition
		method := service.HTTPMethodFor(d.Method)
		fail := func(err error) error { return fmt.Errorf("%s %s: %w", method, operation.Path, err) }

		fn := reflect.ValueOf(d.Function)
		if !fn.IsValid() || fn.Kind() != reflect.Func || fn.IsNil() {
			return fail(fmt.Errorf("non-nil handler function required"))
		}
		for i, m := range operation.Middlewares {
			if m == nil {
				return fail(fmt.Errorf("middleware %d is nil", i))
			}
		}
		if method == "" {
			return fail(fmt.Errorf("unsupported method %q", d.Method))
		}
		if !strings.HasPrefix(operation.Path, "/") {
			return fail(fmt.Errorf("absolute path required"))
		}
		canonical := []string{}
		names := []string{}
		nameSet := map[string]bool{}
		for _, part := range strings.Split(operation.Path, "/") {
			if match := pathParameter.FindStringSubmatch(part); match != nil {
				if nameSet[match[1]] {
					return fail(fmt.Errorf("duplicate path parameter %q", match[1]))
				}
				nameSet[match[1]] = true
				names = append(names, match[1])
				canonical = append(canonical, "{}")
			} else {
				if strings.ContainsAny(part, "{}*:") {
					return fail(fmt.Errorf("unsupported path segment %q", part))
				}
				canonical = append(canonical, part)
			}
		}
		key := method + " " + strings.Join(canonical, "/")
		if seen[key] {
			return fail(fmt.Errorf("duplicate operation"))
		}
		seen[key] = true
		if item := api.OpenAPI().Paths[operation.Path]; item != nil {
			return fail(fmt.Errorf("path already registered; use a fresh API"))
		}
		for _, p := range d.Parameters {
			switch p.Source {
			case definition.Path, definition.Query, definition.Header, definition.Body:
			default:
				return fail(fmt.Errorf("unsupported parameter source %q", p.Source))
			}
			if p.Source == definition.Path && !nameSet[p.Name] {
				return fail(fmt.Errorf("path parameter %q missing from route", p.Name))
			}
		}
		if len(d.Consumes) == 0 {
			d.Consumes = []string{definition.MIMEJSON}
		}
		if len(d.Produces) == 0 {
			d.Produces = []string{definition.MIMEJSON}
		}
		if len(d.ErrorProduces) == 0 {
			d.ErrorProduces = []string{definition.MIMEJSON}
		}
		service.FirstContextParameter()(&d)
		service.ConsumeNoneForHTTPGet()(&d)
		e, err := executor.DefinitionToExecutor(operation.Path, d, 0)
		if err != nil {
			return fail(err)
		}
		op := &huma.Operation{Method: method, Path: operation.Path, OperationID: strings.ToLower(method) + strings.NewReplacer("/", "-", "{", "", "}", "").Replace(operation.Path), Summary: d.Summary, Description: d.Description, Tags: d.Tags, Responses: map[string]*huma.Response{strconv.Itoa(service.HTTPCodeFor(d.Method)): {Description: "Original Nirvana success response"}, "default": {Description: "Original Nirvana error response"}}}
		typ := reflect.TypeOf(d.Function)
		parameters := append([]definition.Parameter(nil), d.Parameters...)
		results := append([]definition.Result(nil), d.Results...)
		prepare := func() {
			for i, p := range parameters {
				if p.Source == definition.Prefab {
					continue
				}
				inputType := typ.In(i)
				if len(p.Operators) > 0 {
					inputType = p.Operators[0].In()
				}
				schema := api.OpenAPI().Components.Schemas.Schema(inputType, true, "")
				if p.Source == definition.Body {
					op.RequestBody = &huma.RequestBody{Required: !p.Optional, Content: map[string]*huma.MediaType{"application/json": {Schema: schema}}}
				} else {
					op.Parameters = append(op.Parameters, &huma.Param{Name: p.Name, In: strings.ToLower(string(p.Source)), Required: p.Source == definition.Path || (!p.Optional && p.Default == nil), Schema: schema})
				}
			}
			for i, r := range results {
				if r.Destination == definition.Data {
					op.Responses[strconv.Itoa(service.HTTPCodeFor(d.Method))].Content = map[string]*huma.MediaType{"application/json": {Schema: api.OpenAPI().Components.Schemas.Schema(typ.Out(i), true, "")}}
				}
			}
		}
		middleware := append([]definition.Middleware(nil), operation.Middlewares...)
		run := func(ctx huma.Context) {
			req, w := unwrap(ctx)
			defer w.(*bufferedHumaWriter).commit()
			nctx := service.NewHTTPContext(w, req.WithContext(ctx.Context()))
			nctx.SetRoutePath(operation.Path)
			for _, name := range names {
				nctx.ValueContainer().Set(name, ctx.Param(name))
			}
			writeError := func(err error) {
				if nctx.ResponseWriter().HeaderWritable() {
					_ = service.WriteError(nctx, service.AllProducers(), err)
				}
			}
			ct, err := service.ContentType(req)
			if err != nil {
				writeError(err)
				return
			}
			if !e.Acceptable(ct) {
				writeError(nerrors.UnsupportedMediaType.Build("Nirvana:Service:NoExecutorForContentType", "unsupported media type").Error())
				return
			}
			accept, err := service.AcceptTypes(req)
			if err != nil {
				writeError(err)
				return
			}
			producible := e.Producible(accept)
			for _, value := range accept {
				if value == definition.MIMEAll {
					producible = true
				}
			}
			if !producible {
				writeError(nerrors.NotAcceptable.Build("Nirvana:Service:NoExecutorToProduce", "not acceptable").Error())
				return
			}
			// Nirvana middleware chains contain a mutable index and must be per request.
			if err := executor.NewMiddlewareExecutor(middleware, e).Execute(nctx); err != nil {
				writeError(err)
			}
			if nctx.ResponseWriter().HeaderWritable() {
				writeError(service.InvalidService.Error())
			}
		}
		batch = append(batch, compiled{op: op, prepare: prepare, run: run})
	}
	for _, c := range batch {
		// Huma's AddOperation may transform shared registry schemas. Build each
		// operation's schemas immediately before it is added so the audit snapshot
		// includes legitimate per-operation transformer changes.
		c.prepare()
		api.OpenAPI().AddOperation(c.op)
		api.Adapter().Handle(c.op, api.Middlewares().Handler(c.run))
	}
	return nil
}

type bufferedHumaWriter struct {
	ctx    huma.Context
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *bufferedHumaWriter) Header() http.Header { return w.header }
func (w *bufferedHumaWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *bufferedHumaWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}

func (w *bufferedHumaWriter) commit() {
	for name, values := range w.header {
		for _, value := range values {
			w.ctx.AppendHeader(name, value)
		}
	}
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	w.ctx.SetStatus(status)
	_, _ = w.ctx.BodyWriter().Write(w.body.Bytes())
}

func unwrap(ctx huma.Context) (*http.Request, http.ResponseWriter) {
	u := ctx.URL()
	version := ctx.Version()
	req := &http.Request{Method: ctx.Method(), URL: &u, Header: make(http.Header), Host: ctx.Host(), RemoteAddr: ctx.RemoteAddr(), TLS: ctx.TLS(), Proto: version.Proto, ProtoMajor: version.ProtoMajor, ProtoMinor: version.ProtoMinor, Body: io.NopCloser(ctx.BodyReader())}
	ctx.EachHeader(func(name, value string) { req.Header.Add(name, value) })
	req = req.WithContext(ctx.Context())
	w := &bufferedHumaWriter{ctx: ctx, header: make(http.Header)}
	return req, w
}

// Marker adds a demo-only backend header for observing operation ownership.
func Marker(name string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Demo-Backend", name)
		h.ServeHTTP(w, r)
	})
}
