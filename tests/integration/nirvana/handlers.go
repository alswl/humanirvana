package nirvana

import (
	"context"
	"fmt"
	"time"

	"github.com/caicloud/nirvana/definition"
)

// userStore is an in-memory store for demo purposes.
var userStore = make(map[string]User)
var nextID = 1

// ListUsers handles GET /users
func ListUsers(ctx context.Context) ([]User, error) {
	users := make([]User, 0, len(userStore))
	for _, u := range userStore {
		users = append(users, u)
	}
	return users, nil
}

// CreateUser handles POST /users
func CreateUser(ctx context.Context, req CreateUserRequest) (*User, error) {
	id := fmt.Sprintf("%d", nextID)
	nextID++
	now := time.Now().Format(time.RFC3339)
	user := User{
		ID:        id,
		Name:      req.Name,
		Email:     req.Email,
		CreatedAt: now,
	}
	userStore[id] = user
	return &user, nil
}

// GetUser handles GET /users/{id}
func GetUser(ctx context.Context, id string) (*User, error) {
	user, ok := userStore[id]
	if !ok {
		return nil, fmt.Errorf("user not found: %s", id)
	}
	return &user, nil
}

// DeleteUser handles DELETE /users/{id}
func DeleteUser(ctx context.Context, id string) error {
	if _, ok := userStore[id]; !ok {
		return fmt.Errorf("user not found: %s", id)
	}
	delete(userStore, id)
	return nil
}

// Health handles GET /health
func Health(ctx context.Context) HealthResponse {
	return HealthResponse{Status: "healthy"}
}

// Descriptor contains all API definitions for the demo.
var Descriptor = definition.Descriptor{
	Path: "/",
	Children: []definition.Descriptor{
		{
			Path: "/users",
			Definitions: []definition.Definition{
				{
					Method:      definition.List,
					Function:    ListUsers,
					Description: "List all users",
					Produces:    []string{"application/json"},
					Results:     definition.DataErrorResults("UserListResponse"),
				},
				{
					Method:      definition.Create,
					Function:    CreateUser,
					Description: "Create a new user",
					Consumes:    []string{"application/json"},
					Produces:    []string{"application/json"},
					Parameters: []definition.Parameter{
						{
							Source:      definition.Body,
							Name:        "request",
							Description: "Create user request",
						},
					},
					Results: definition.DataErrorResults("User"),
				},
			},
			Children: []definition.Descriptor{
				{
					Path: "/{id}",
					Definitions: []definition.Definition{
						{
							Method:      definition.Get,
							Function:    GetUser,
							Description: "Get a user by ID",
							Produces:    []string{"application/json"},
							Parameters: []definition.Parameter{
								{
									Source: definition.Path,
									Name:   "id",
								},
							},
							Results: definition.DataErrorResults("User"),
						},
						{
							Method:      definition.Delete,
							Function:    DeleteUser,
							Description: "Delete a user by ID",
							Produces:    []string{"application/json"},
							Parameters: []definition.Parameter{
								{
									Source: definition.Path,
									Name:   "id",
								},
							},
							Results: []definition.Result{definition.ErrorResult()},
						},
					},
				},
			},
		},
		{
			Path: "/health",
			Definitions: []definition.Definition{
				{
					Method:      definition.Get,
					Function:    Health,
					Description: "Health check",
					Produces:    []string{"application/json"},
					Results:     []definition.Result{definition.DataResultFor("HealthResponse")},
				},
			},
		},
	},
}
