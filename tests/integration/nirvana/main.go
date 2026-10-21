// Package nirvana is a demo Nirvana project for migration testing.
package nirvana

import (
	"github.com/caicloud/nirvana"
)

// User represents a user entity.
type User struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	CreatedAt string `json:"created_at"`
}

// UserListResponse represents a list of users.
type UserListResponse struct {
	Items      []User `json:"items"`
	TotalCount int    `json:"total_count"`
}

// CreateUserRequest represents a request to create a user.
type CreateUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// HealthResponse represents a health check response.
type HealthResponse struct {
	Status string `json:"status"`
}

// NewServer creates a new Nirvana server for demo.
func NewServer(port uint16) nirvana.Server {
	config := nirvana.NewDefaultConfig()
	config.Configure(nirvana.Port(port), nirvana.Descriptor(Descriptor))
	return nirvana.NewServer(config)
}

//go:generate nirvana descriptor $GOFILE
