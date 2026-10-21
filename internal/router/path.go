package router

import (
	"strings"
)

// ConvertPath converts a Nirvana path to a Huma-compatible path.
// Nirvana and Huma both use {param} path parameters.
// This function normalizes the path format.
func ConvertPath(nirvanaPath string) string {
	if nirvanaPath == "" {
		return "/"
	}

	path := nirvanaPath

	// Ensure path starts with /
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	return path
}

// ConvertPathToHuma converts Nirvana path format to Huma format.
// Nirvana: /users/{id}
// Huma: /users/{id}
func ConvertPathToHuma(nirvanaPath string) string {
	return ConvertPath(nirvanaPath)
}

// ConvertPathToEcho converts Nirvana path format to Echo format.
// Nirvana: /users/{id}
// Echo: /users/:id
func ConvertPathToEcho(nirvanaPath string) string {
	path := ConvertPath(nirvanaPath)
	// Convert {param} to :param for Echo
	path = strings.ReplaceAll(path, "{", ":")
	path = strings.ReplaceAll(path, "}", "")
	return path
}

// ExtractPathParams extracts parameter names from a Nirvana path.
// Nirvana: /users/{id}/posts/{postId} -> ["id", "postId"]
func ExtractPathParams(nirvanaPath string) []string {
	params := make([]string, 0)

	start := -1
	for i, ch := range nirvanaPath {
		if ch == '{' {
			start = i + 1
		} else if ch == '}' && start > 0 {
			params = append(params, nirvanaPath[start:i])
			start = -1
		}
	}

	return params
}

// ValidatePath validates a path is in correct format.
func ValidatePath(path string) error {
	// Check for balanced braces
	openCount := strings.Count(path, "{")
	closeCount := strings.Count(path, "}")

	if openCount != closeCount {
		return &ValidationError{
			Field:   "Path",
			Message: "unbalanced braces in path",
		}
	}

	// Check for empty param names
	for _, param := range ExtractPathParams(path) {
		if param == "" {
			return &ValidationError{
				Field:   "Path",
				Message: "empty parameter name in path",
			}
		}
	}

	return nil
}

// JoinPath joins path segments.
func JoinPath(segments ...string) string {
	result := ""

	for _, seg := range segments {
		if seg == "" {
			continue
		}

		// Remove leading/trailing slashes
		seg = strings.Trim(seg, "/")

		if result == "" {
			result = "/" + seg
		} else {
			result = result + "/" + seg
		}
	}

	return result
}

// NormalizePath normalizes a path for consistent handling.
func NormalizePath(path string) string {
	// Remove duplicate slashes
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}

	// Remove trailing slash (except for root)
	if path != "/" && strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	}

	return ConvertPath(path)
}
