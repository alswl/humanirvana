// Package huma is a demo Huma migrated project for migration testing.
package huma

import (
	"context"
	"fmt"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// User represents a user entity (Huma version).
type User struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	CreatedAt string `json:"created_at"`
}

// UserListResponse represents a list of users.
type UserListResponse struct {
	Body struct {
		Items      []User `json:"items"`
		TotalCount int    `json:"total_count"`
	}
}

// CreateUserRequest represents a request to create a user.
type CreateUserRequest struct {
	Body struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
}

// CreateUserResponse is the response for CreateUser (Huma expects output struct with Body for response body).
type CreateUserResponse struct {
	Body User
}

// HealthResponse represents a health check response.
type HealthResponse struct {
	Body struct {
		Status string `json:"status"`
	}
}

// userStore is an in-memory store for demo purposes.
var userStore = make(map[string]User)
var nextID = 1

// ListUsers handles GET /users
func ListUsers(ctx context.Context, input *struct{}) (*UserListResponse, error) {
	users := make([]User, 0, len(userStore))
	for _, u := range userStore {
		users = append(users, u)
	}
	resp := &UserListResponse{}
	resp.Body.Items = users
	resp.Body.TotalCount = len(users)
	return resp, nil
}

// CreateUser handles POST /users
func CreateUser(ctx context.Context, input *CreateUserRequest) (*CreateUserResponse, error) {
	id := fmt.Sprintf("%d", nextID)
	nextID++
	now := time.Now().Format(time.RFC3339)
	user := User{
		ID:        id,
		Name:      input.Body.Name,
		Email:     input.Body.Email,
		CreatedAt: now,
	}
	userStore[id] = user
	return &CreateUserResponse{Body: user}, nil
}

// GetUserResponse is the response for GetUser (Huma output with Body).
type GetUserResponse struct {
	Body User
}

// GetUser handles GET /users/{id}
func GetUser(ctx context.Context, input *struct {
	ID string `path:"id"`
}) (*GetUserResponse, error) {
	user, ok := userStore[input.ID]
	if !ok {
		return nil, huma.Error404NotFound("user not found: " + input.ID)
	}
	return &GetUserResponse{Body: user}, nil
}

// DeleteUser handles DELETE /users/{id}
func DeleteUser(ctx context.Context, input *struct {
	ID string `path:"id"`
}) error {
	if _, ok := userStore[input.ID]; !ok {
		return huma.Error404NotFound("user not found: " + input.ID)
	}
	delete(userStore, input.ID)
	return nil
}

// Health handles GET /health
func Health(ctx context.Context, input *struct{}) (*HealthResponse, error) {
	resp := &HealthResponse{}
	resp.Body.Status = "healthy"
	return resp, nil
}
