package router

import (
	"reflect"

	"github.com/caicloud/nirvana/definition"
)

// OutputStruct represents a generated output struct for Huma.
type OutputStruct struct {
	Name       string
	BodyField  *OutputField
	MetaFields []OutputField
	ReflectTyp reflect.Type
}

// OutputField represents a field in the output struct.
type OutputField struct {
	Name        string
	Type        reflect.Type
	Tag         string
	IsBody      bool
	Description string
}

// GenerateOutputStruct generates output structs from results.
func GenerateOutputStruct(def *NirvanaDefinition, structName string) (*OutputStruct, error) {
	var bodyField *OutputField
	var metaFields []OutputField

	for _, r := range def.Results {
		field := OutputField{
			Name:        resultFieldName(r),
			Type:        r.Type,
			Description: r.Description,
		}

		switch r.Destination {
		case DestData, DestError:
			field.IsBody = true
			if bodyField != nil {
				return nil, &ValidationError{
					Field:   "Results",
					Message: "multiple body results not supported",
				}
			}
			bodyField = &field
		case DestMeta:
			field.IsBody = false
			field.Tag = "header:\"" + r.Type.Name() + "\""
			metaFields = append(metaFields, field)
		}
	}

	return &OutputStruct{
		Name:       structName,
		BodyField:  bodyField,
		MetaFields: metaFields,
		ReflectTyp: nil,
	}, nil
}

// resultFieldName returns the field name for a result.
func resultFieldName(r NirvanaResult) string {
	switch r.Destination {
	case DestData:
		return "Body"
	case DestError:
		return "Error"
	case DestMeta:
		return "Meta"
	default:
		return "Data"
	}
}

// ConvertResultsFromNirvana converts Nirvana results to Huma-compatible format.
func ConvertResultsFromNirvana(results []definition.Result) ([]NirvanaResult, error) {
	result := make([]NirvanaResult, len(results))

	for i, r := range results {
		result[i] = NirvanaResult{
			Destination: ResultDestination(r.Destination),
			Type:        inferResultGoType(r),
			Description: r.Description,
		}
	}

	return result, nil
}

// inferResultGoType infers the Go type for a result.
func inferResultGoType(r definition.Result) reflect.Type {
	// Check operators for type information
	if len(r.Operators) > 0 {
		for _, op := range r.Operators {
			if out := op.Out(); out != nil && out.Kind() != reflect.Interface {
				return out
			}
		}
	}

	// Default types based on destination
	switch r.Destination {
	case definition.Error:
		return reflect.TypeOf((*error)(nil)).Elem()
	case definition.Data:
		return reflect.TypeOf((*interface{})(nil)).Elem()
	case definition.Meta:
		return reflect.TypeOf(map[string]string{})
	default:
		return reflect.TypeOf((*interface{})(nil)).Elem()
	}
}

// ResponseConfig holds configuration for response generation.
type ResponseConfig struct {
	// SuccessStatus is the HTTP status code for successful responses
	SuccessStatus int

	// ErrorStatus is the HTTP status code for error responses
	ErrorStatus int

	// DefaultContentType is the default Content-Type header
	DefaultContentType string
}

// DefaultResponseConfig returns the default response configuration.
func DefaultResponseConfig() *ResponseConfig {
	return &ResponseConfig{
		SuccessStatus:      200,
		ErrorStatus:        500,
		DefaultContentType: "application/json",
	}
}
