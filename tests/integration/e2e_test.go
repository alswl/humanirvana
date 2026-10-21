package demo_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestConfig holds configuration for E2E tests.
type TestConfig struct {
	NirvanaURL string
	HumaURL    string
	Timeout    time.Duration
}

// DefaultConfig returns default test configuration.
func DefaultConfig() *TestConfig {
	return &TestConfig{
		NirvanaURL: os.Getenv("HUMANIRVANA_NIRVANA_URL"),
		HumaURL:    os.Getenv("HUMANIRVANA_HUMA_URL"),
		Timeout:    10 * time.Second,
	}
}

// User represents a user for testing.
type User struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	CreatedAt string `json:"created_at,omitempty"`
}

// UserListWrapper is the list response shape (Huma: {items, total_count}; Nirvana may use items).
type UserListWrapper struct {
	Items      []User `json:"items"`
	TotalCount int    `json:"total_count"`
}

// CreateUserRequest represents a request to create a user.
type CreateUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// TestUserComparison compares users between Nirvana and Huma APIs.
func TestUserComparison(t *testing.T) {
	config := DefaultConfig()
	if config.HumaURL == "" || config.NirvanaURL == "" {
		t.Skip("external integration fixtures required: set HUMANIRVANA_HUMA_URL and HUMANIRVANA_NIRVANA_URL")
	}
	baseURL := config.HumaURL
	baseURLNirvana := config.NirvanaURL

	client := &http.Client{Timeout: config.Timeout}

	// Clean up any existing users
	t.Cleanup(func() {
		// Reset in-memory stores by restarting servers (not implemented in this test)
	})

	// Test 1: Health check
	t.Run("HealthCheck", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/health")
		if err != nil {
			t.Skipf("Huma server not available: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	})

	// Test 2: Create user on Huma
	t.Run("CreateUser", func(t *testing.T) {
		req := CreateUserRequest{
			Name:  "Test User",
			Email: "test@example.com",
		}
		body, _ := json.Marshal(req)

		resp, err := client.Post(baseURL+"/users", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("Failed to create user: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected status 201 or 200, got %d: %s", resp.StatusCode, string(bodyBytes))
		}

		// Huma returns User at top level (Body field serialized as response body)
		var user User
		if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if user.Name != req.Name {
			t.Errorf("Expected name %s, got %s", req.Name, user.Name)
		}
		if user.Email != req.Email {
			t.Errorf("Expected email %s, got %s", req.Email, user.Email)
		}
		if user.ID == "" {
			t.Error("Expected non-empty ID")
		}
	})

	// Test 3: List users
	t.Run("ListUsers", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/users")
		if err != nil {
			t.Fatalf("Failed to list users: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var listResp UserListWrapper
		if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if len(listResp.Items) == 0 {
			t.Error("Expected at least one user")
		}
	})

	// Test 4: Get user by ID
	t.Run("GetUser", func(t *testing.T) {
		// First create a user
		req := CreateUserRequest{
			Name:  "Get Test",
			Email: "gettest@example.com",
		}
		body, _ := json.Marshal(req)

		createResp, err := client.Post(baseURL+"/users", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("Failed to create user: %v", err)
		}
		defer createResp.Body.Close()

		var created User
		if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
			t.Fatalf("Failed to decode created user: %v", err)
		}

		// Then get it
		resp, err := client.Get(fmt.Sprintf("%s/users/%s", baseURL, created.ID))
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var user User
		if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if user.ID != created.ID {
			t.Errorf("Expected ID %s, got %s", created.ID, user.ID)
		}
	})

	// Test 5: Delete user
	t.Run("DeleteUser", func(t *testing.T) {
		// First create a user
		req := CreateUserRequest{
			Name:  "Delete Test",
			Email: "deletetest@example.com",
		}
		body, _ := json.Marshal(req)

		createResp, err := client.Post(baseURL+"/users", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("Failed to create user: %v", err)
		}
		defer createResp.Body.Close()

		var created User
		if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
			t.Fatalf("Failed to decode created user: %v", err)
		}

		// Delete it
		reqDel, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/users/%s", baseURL, created.ID), nil)
		if err != nil {
			t.Fatalf("Failed to create delete request: %v", err)
		}

		resp, err := client.Do(reqDel)
		if err != nil {
			t.Fatalf("Failed to delete user: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 204 or 200, got %d", resp.StatusCode)
		}

		// Verify deletion
		getResp, err := client.Get(fmt.Sprintf("%s/users/%s", baseURL, created.ID))
		if err != nil {
			t.Fatalf("Failed to get user after deletion: %v", err)
		}
		defer getResp.Body.Close()

		if getResp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected 404 after deletion, got %d", getResp.StatusCode)
		}
	})

	// Test 6: Nirvana vs Huma behavior comparison
	t.Run("NirvanaHumaComparison", func(t *testing.T) {
		// This test compares behavior between Nirvana and Huma servers
		// It requires both servers to be running

		// Create user on Nirvana
		nirvanaReq := CreateUserRequest{
			Name:  "Comparison Test",
			Email: "comparison@example.com",
		}
		body, _ := json.Marshal(nirvanaReq)

		nirvanaResp, err := client.Post(baseURLNirvana+"/users", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Skipf("Nirvana server not available: %v", err)
		}
		defer nirvanaResp.Body.Close()

		if nirvanaResp.StatusCode != http.StatusCreated {
			bodyBytes, _ := io.ReadAll(nirvanaResp.Body)
			t.Fatalf("Nirvana: Expected status 201, got %d: %s", nirvanaResp.StatusCode, string(bodyBytes))
		}

		var nirvanaUser User
		if err := json.NewDecoder(nirvanaResp.Body).Decode(&nirvanaUser); err != nil {
			t.Fatalf("Failed to decode Nirvana user: %v", err)
		}

		// Create same user on Huma
		humaResp, err := client.Post(baseURL+"/users", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("Huma: Failed to create user: %v", err)
		}
		defer humaResp.Body.Close()

		var humaUser User
		if err := json.NewDecoder(humaResp.Body).Decode(&humaUser); err != nil {
			t.Fatalf("Failed to decode Huma user: %v", err)
		}

		// Compare core fields (ID may differ due to different counters)
		if nirvanaUser.Name != humaUser.Name {
			t.Errorf("Name mismatch: Nirvana=%s, Huma=%s", nirvanaUser.Name, humaUser.Name)
		}
		if nirvanaUser.Email != humaUser.Email {
			t.Errorf("Email mismatch: Nirvana=%s, Huma=%s", nirvanaUser.Email, humaUser.Email)
		}
	})
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
