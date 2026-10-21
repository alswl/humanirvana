package router_test

import (
	"testing"

	"github.com/alswl/humanirvana/internal/router"
)

func TestConvertPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty path", "", "/"},
		{"simple path", "/users", "/users"},
		{"path with param", "/users/{id}", "/users/{id}"},
		{"path with multiple params", "/users/{id}/posts/{postId}", "/users/{id}/posts/{postId}"},
		{"path without leading slash", "users", "/users"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := router.ConvertPath(tt.input)
			if result != tt.expected {
				t.Errorf("ConvertPath(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExtractPathParams(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"no params", "/users", nil},
		{"single param", "/users/{id}", []string{"id"}},
		{"multiple params", "/users/{id}/posts/{postId}", []string{"id", "postId"}},
		{"nested params", "/api/{version}/users/{userId}", []string{"version", "userId"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := router.ExtractPathParams(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("ExtractPathParams(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestValidatePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"valid path", "/users/{id}", false},
		{"valid empty", "/", false},
		{"unbalanced braces", "/users/{id", true},
		{"empty param name", "/users/{}", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := router.ValidatePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"double slash", "/users//posts", "/users/posts"},
		{"trailing slash", "/users/", "/users"},
		{"no leading slash", "users", "/users"},
		{"already normalized", "/users/{id}", "/users/{id}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := router.NormalizePath(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizePath(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestJoinPath(t *testing.T) {
	tests := []struct {
		name     string
		segments []string
		expected string
	}{
		{"single segment", []string{"users"}, "/users"},
		{"two segments", []string{"api", "v1"}, "/api/v1"},
		{"three segments", []string{"api", "v1", "users"}, "/api/v1/users"},
		{"with empty", []string{"", "users"}, "/users"},
		{"leading slash", []string{"/users"}, "/users"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := router.JoinPath(tt.segments...)
			if result != tt.expected {
				t.Errorf("JoinPath(%v) = %q, want %q", tt.segments, result, tt.expected)
			}
		})
	}
}

func TestParameterSource(t *testing.T) {
	tests := []struct {
		source   router.ParameterSource
		expected string
	}{
		{router.SourcePath, "path"},
		{router.SourceQuery, "query"},
		{router.SourceHeader, "header"},
		{router.SourceForm, "form"},
		{router.SourceBody, "body"},
		{router.SourceAuto, "auto"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.source) != tt.expected {
				t.Errorf("ParameterSource = %q, want %q", tt.source, tt.expected)
			}
		})
	}
}

func TestResultDestination(t *testing.T) {
	tests := []struct {
		dest     router.ResultDestination
		expected string
	}{
		{router.DestMeta, "meta"},
		{router.DestData, "data"},
		{router.DestError, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.dest) != tt.expected {
				t.Errorf("ResultDestination = %q, want %q", tt.dest, tt.expected)
			}
		})
	}
}
