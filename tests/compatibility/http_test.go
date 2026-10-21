package demo

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHTTPApplicationFlow(t *testing.T) {
	app := newApp(t)
	client := &http.Client{Timeout: 3 * time.Second}
	var created []any
	for name, h := range map[string]http.Handler{"original": app.Original, "migrated": app.Migrated} {
		server := httptest.NewServer(h)
		defer server.Close()
		r, err := http.NewRequest("POST", server.URL+"/orders", strings.NewReader(`{"item":"book","quantity":2}`))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer demo-token")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Request-ID", "http-test")
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 201 || response.Header.Get("X-Request-ID") != "http-test" || response.Header.Get("X-Demo-Backend") != name {
			t.Fatalf("%s status=%d headers=%v body=%s", name, response.StatusCode, response.Header, body)
		}
		var value any
		if err := json.Unmarshal(body, &value); err != nil {
			t.Fatal(err)
		}
		created = append(created, value)
		get, err := http.NewRequest("GET", server.URL+"/orders/2", nil)
		if err != nil {
			t.Fatal(err)
		}
		get.Header.Set("Authorization", "Bearer demo-token")
		response, err = client.Do(get)
		if err != nil {
			t.Fatal(err)
		}
		body, err = io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		var fetched any
		if err := json.Unmarshal(body, &fetched); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 || !reflect.DeepEqual(value, fetched) || app.Writes(name) != 1 {
			t.Fatalf("%s persisted result mismatch: %s", name, body)
		}
	}
	if !reflect.DeepEqual(created[0], created[1]) {
		t.Fatalf("network behavior differs: %v", created)
	}
	schema := request(app.Migrated, "GET", "/openapi.json", "", "")
	var document map[string]any
	if schema.Code != 200 || json.Unmarshal(schema.Body.Bytes(), &document) != nil {
		t.Fatalf("invalid OpenAPI: %s", schema.Body.String())
	}
	paths := document["paths"].(map[string]any)
	if paths["/orders"] == nil || paths["/orders/{id}"] == nil {
		t.Fatal("missing OpenAPI operation")
	}
}

func TestContentNegotiationParity(t *testing.T) {
	cases := []struct {
		name, contentType, accept string
		status                    int
	}{
		{"json", "application/json", "application/json", 201},
		{"wildcard", "application/json", "*/*", 201},
		{"unsupported_content_type", "text/plain", "application/json", 415},
		{"unsupported_accept", "application/json", "application/xml", 406},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			app := newApp(t)
			responses := []*httptest.ResponseRecorder{}
			for _, h := range []http.Handler{app.Original, app.Migrated} {
				r := httptest.NewRequest("POST", "/orders", strings.NewReader(`{"item":"book","quantity":2}`))
				r.Header.Set("Authorization", "Bearer demo-token")
				r.Header.Set("Content-Type", tt.contentType)
				r.Header.Set("Accept", tt.accept)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				responses = append(responses, w)
				if w.Code != tt.status {
					t.Fatalf("status=%d want=%d body=%s", w.Code, tt.status, w.Body.String())
				}
			}
			if responses[0].Body.String() != responses[1].Body.String() {
				t.Fatalf("negotiation differs: %s / %s", responses[0].Body.String(), responses[1].Body.String())
			}
		})
	}
}
