// Package router provides route conversion from Nirvana Definition to Huma Operation.
package router

import (
	"fmt"
	"reflect"

	"github.com/caicloud/nirvana/definition"
)

// NirvanaDefinition represents a converted Nirvana API definition to Huma format.
type NirvanaDefinition struct {
	// Path is the URL path pattern (e.g., "/users/{id}")
	Path string

	// Method is the HTTP method (GET, POST, PUT, DELETE, PATCH)
	Method string

	// Summary is a short description
	Summary string

	// Description is a detailed description
	Description string

	// Tags for OpenAPI grouping
	Tags []string

	// Consumes MIME types
	Consumes []string

	// Produces MIME types
	Produces []string

	// Handler is the user-provided function
	Handler interface{}

	// Parameters converted from Nirvana Parameter list
	Parameters []NirvanaParameter

	// Results converted from Nirvana Result list
	Results []NirvanaResult

	// Middleware list for this definition
	Middlewares []Middleware
}

// Middleware represents a middleware for this definition.
type Middleware struct {
	Name     string
	Priority int
}

// NirvanaParameter converts Nirvana Parameter to Huma-compatible input struct.
type NirvanaParameter struct {
	// Name is the parameter name
	Name string

	// Source defines where the value comes from
	Source ParameterSource // Path, Query, Header, Form, Body, Auto

	// Type is the Go type for this parameter
	Type reflect.Type

	// Default is the default value if not provided
	Default interface{}

	// Optional marks the parameter as optional
	Optional bool

	// Description is for documentation
	Description string

	// Example value for OpenAPI
	Example interface{}
}

// ParameterSource defines where a parameter value comes from.
type ParameterSource string

const (
	// SourcePath means value is from URL path
	SourcePath ParameterSource = "path"
	// SourceQuery means value is from URL query string
	SourceQuery ParameterSource = "query"
	// SourceHeader means value is from request header
	SourceHeader ParameterSource = "header"
	// SourceForm means value is from request form
	SourceForm ParameterSource = "form"
	// SourceBody means value is from request body
	SourceBody ParameterSource = "body"
	// SourceAuto means auto-detect from struct tags
	SourceAuto ParameterSource = "auto"
)

// NirvanaResult converts Nirvana Result to Huma response handling.
type NirvanaResult struct {
	// Destination defines where the value goes
	Destination ResultDestination

	// Type is the Go type for this result
	Type reflect.Type

	// Description is for documentation
	Description string
}

// ResultDestination defines where a result value goes.
type ResultDestination string

const (
	// DestMeta means result will be set into the header of response
	DestMeta ResultDestination = "meta"
	// DestData means result will be set into the body of response
	DestData ResultDestination = "data"
	// DestError means the result is an error
	DestError ResultDestination = "error"
)

// ConverterContext holds configuration for conversion.
type ConverterContext struct {
	// PackagePath is the import path of the source package
	PackagePath string

	// PackageName is the Go package name
	PackageName string

	// OpenAPITitle for generated OpenAPI docs
	OpenAPITitle string

	// OpenAPIVersion for generated OpenAPI docs
	OpenAPIVersion string
}

// FromDefinition creates a NirvanaDefinition from a Nirvana Definition.
func FromDefinition(def definition.Definition) *NirvanaDefinition {
	nd := &NirvanaDefinition{
		Path:        "",
		Method:      string(def.Method),
		Summary:     def.Summary,
		Description: def.Description,
		Tags:        def.Tags,
		Consumes:    def.Consumes,
		Produces:    def.Produces,
		Handler:     def.Function,
	}

	// Convert parameters
	for _, p := range def.Parameters {
		nd.Parameters = append(nd.Parameters, convertParameter(p))
	}

	// Convert results
	for _, r := range def.Results {
		nd.Results = append(nd.Results, convertResult(r))
	}

	return nd
}

func convertParameter(p definition.Parameter) NirvanaParameter {
	return NirvanaParameter{
		Name:        p.Name,
		Source:      ParameterSource(fmt.Sprintf("%v", p.Source)),
		Type:        inferType(p),
		Default:     p.Default,
		Optional:    p.Optional,
		Description: p.Description,
		Example:     nil, // definition.Parameter may not have Example in this version
	}
}

func convertResult(r definition.Result) NirvanaResult {
	return NirvanaResult{
		Destination: ResultDestination(r.Destination),
		Type:        inferResultType(r),
		Description: r.Description,
	}
}

func inferType(p definition.Parameter) reflect.Type {
	// Default to string if we can't infer
	return reflect.TypeOf("")
}

func inferResultType(r definition.Result) reflect.Type {
	if r.Destination == definition.Error {
		return reflect.TypeOf((*error)(nil)).Elem()
	}
	return reflect.TypeOf(nil)
}
