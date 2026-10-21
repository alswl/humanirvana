package router_test

import (
	"testing"

	"github.com/alswl/humanirvana/internal/router"
)

// TestConvertPath tests the path conversion logic.
func TestPathUtilitiesConvertPath(t *testing.T) {
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

// TestExtractPathParams tests parameter extraction.
func TestPathUtilitiesExtractPathParams(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"no params", "/users", nil},
		{"single param", "/users/{id}", []string{"id"}},
		{"multiple params", "/users/{id}/posts/{postId}", []string{"id", "postId"}},
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

// TestValidatePath tests path validation.
func TestPathUtilitiesValidatePath(t *testing.T) {
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

// TestNormalizePath tests path normalization.
func TestPathUtilitiesNormalizePath(t *testing.T) {
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

// TestJoinPath tests path joining.
func TestPathUtilitiesJoinPath(t *testing.T) {
	tests := []struct {
		name     string
		segments []string
		expected string
	}{
		{"single segment", []string{"users"}, "/users"},
		{"two segments", []string{"api", "v1"}, "/api/v1"},
		{"three segments", []string{"api", "v1", "users"}, "/api/v1/users"},
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
