package router

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/caicloud/nirvana/definition"
)

// InputStruct represents a generated input struct for Huma.
type InputStruct struct {
	Name       string
	Fields     []InputField
	TagString  string
	ReflectTyp reflect.Type
}

// InputField represents a field in the input struct.
type InputField struct {
	Name        string
	Type        reflect.Type
	Tag         string
	Default     interface{}
	Optional    bool
	Description string
}

// GenerateInputStruct generates a Go struct definition from parameters.
func GenerateInputStruct(def *NirvanaDefinition, structName string) *InputStruct {
	if len(def.Parameters) == 0 {
		return nil
	}

	fields := make([]InputField, 0, len(def.Parameters))
	tags := make([]string, 0, len(def.Parameters))

	for _, p := range def.Parameters {
		field := InputField{
			Name:        structFieldName(p.Name),
			Type:        p.Type,
			Default:     p.Default,
			Optional:    p.Optional,
			Description: p.Description,
		}

		// Generate struct tag
		tag := generateStructTag(p)
		field.Tag = tag
		tags = append(tags, tag)

		fields = append(fields, field)
	}

	return &InputStruct{
		Name:       structName,
		Fields:     fields,
		TagString:  strings.Join(tags, " "),
		ReflectTyp: nil, // Will be set when the struct is actually used
	}
}

// generateStructTag generates a struct tag for a parameter.
func generateStructTag(p NirvanaParameter) string {
	var tagParts []string

	switch p.Source {
	case SourcePath:
		tagParts = append(tagParts, "path:\""+p.Name+"\"")
	case SourceQuery:
		tagParts = append(tagParts, "query:\""+p.Name+"\"")
	case SourceHeader:
		tagParts = append(tagParts, "header:\""+p.Name+"\"")
	case SourceForm:
		tagParts = append(tagParts, "form:\""+p.Name+"\"")
	case SourceBody:
		tagParts = append(tagParts, "body")
	case SourceAuto:
		// Auto-generated from parameter definition
		tagParts = append(tagParts, "auto:\""+p.Name+"\"")
	}

	if p.Optional {
		tagParts = append(tagParts, "optional")
	}

	if p.Description != "" {
		tagParts = append(tagParts, "description:\""+p.Description+"\"")
	}

	return strings.Join(tagParts, " ")
}

// structFieldName converts a parameter name to a valid Go struct field name.
func structFieldName(name string) string {
	if name == "" {
		return "Input"
	}
	// Capitalize first letter
	return strings.ToUpper(name[:1]) + name[1:]
}

// ConvertParametersFromNirvana converts Nirvana parameters to Huma-compatible format.
func ConvertParametersFromNirvana(params []definition.Parameter) ([]NirvanaParameter, error) {
	result := make([]NirvanaParameter, len(params))

	for i, p := range params {
		result[i] = NirvanaParameter{
			Name:        p.Name,
			Source:      ParameterSource(fmt.Sprintf("%v", p.Source)),
			Type:        inferParameterType(p),
			Default:     p.Default,
			Optional:    p.Optional,
			Description: p.Description,
			Example:     nil, // definition.Parameter may not have Example in this version
		}
	}

	return result, nil
}

// inferParameterType infers the Go type from a Nirvana parameter.
func inferParameterType(p definition.Parameter) reflect.Type {
	// Try to infer from the operator's Out type if available
	if len(p.Operators) > 0 {
		for _, op := range p.Operators {
			if out := op.Out(); out != nil && out.Kind() != reflect.Interface {
				return out
			}
		}
	}

	// Default type inference based on source
	switch fmt.Sprintf("%v", p.Source) {
	case "path", "query", "header", "form":
		return reflect.TypeOf("")
	case "body":
		// For body, we'd typically use a map or the actual struct type
		// Return empty interface for now, actual type should be inferred from Operators
		return reflect.TypeOf((*interface{})(nil)).Elem()
	default:
		return reflect.TypeOf("")
	}
}

// ValidateParameters validates that parameters can be converted.
func ValidateParameters(params []NirvanaParameter) error {
	for _, p := range params {
		if p.Name == "" && p.Source != SourceBody {
			return &ValidationError{
				Field:   "Name",
				Message: "parameter name is required for non-body parameters",
			}
		}
	}
	return nil
}

// ValidationError represents a parameter validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
