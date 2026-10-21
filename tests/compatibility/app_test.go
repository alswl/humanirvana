package demo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func newApp(t *testing.T) *App {
	t.Helper()
	app, err := New()
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func request(h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("X-Request-ID", "test-request")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestBehaviorParity(t *testing.T) {
	cases := []struct {
		name, method, path, body, token string
		status, writes                  int
	}{
		{"create", "POST", "/orders", `{"item":"book","quantity":2}`, "demo-token", 201, 1},
		{"auth_short_circuit", "POST", "/orders", `{"item":"book","quantity":2}`, "", 401, 0},
		{"invalid_token", "POST", "/orders", `{"item":"book","quantity":2}`, "wrong", 401, 0},
		{"operator_rejects_quantity", "POST", "/orders", `{"item":"book","quantity":0}`, "demo-token", 400, 0},
		{"operator_rejects_item", "POST", "/orders", `{"item":" ","quantity":1}`, "demo-token", 400, 0},
		{"legacy_malformed_json", "POST", "/orders", `{`, "demo-token", 500, 0},
		{"business_conflict", "POST", "/orders", `{"item":"sold-out","quantity":1}`, "demo-token", 409, 0},
		{"get_seeded_order", "GET", "/orders/1", "", "demo-token", 200, 0},
		{"missing_order", "GET", "/orders/99", "", "demo-token", 404, 0},
		{"invalid_path_integer", "GET", "/orders/nope", "", "demo-token", 400, 0},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			app := newApp(t)
			old := request(app.Original, tt.method, tt.path, tt.body, tt.token)
			migrated := request(app.Migrated, tt.method, tt.path, tt.body, tt.token)
			for name, w := range map[string]*httptest.ResponseRecorder{"original": old, "migrated": migrated} {
				if w.Code != tt.status {
					t.Fatalf("%s: status=%d want=%d body=%s", name, w.Code, tt.status, w.Body.String())
				}
				if got := app.Writes(name); got != tt.writes {
					t.Errorf("%s writes=%d want=%d", name, got, tt.writes)
				}
			}
			if !reflect.DeepEqual(app.Trace("original"), app.Trace("migrated")) {
				t.Errorf("execution order differs: %v vs %v", app.Trace("original"), app.Trace("migrated"))
			}
			for _, header := range []string{"Content-Type", "X-Request-ID"} {
				if old.Result().Header.Get(header) != migrated.Result().Header.Get(header) {
					t.Errorf("%s differs: %q vs %q", header, old.Result().Header.Get(header), migrated.Result().Header.Get(header))
				}
			}
			var a, b any
			if err := json.Unmarshal(old.Body.Bytes(), &a); err != nil {
				a = old.Body.String()
			}
			if err := json.Unmarshal(migrated.Body.Bytes(), &b); err != nil {
				b = migrated.Body.String()
			}
			if !reflect.DeepEqual(a, b) {
				t.Errorf("response differs: original=%s migrated=%s", old.Body.String(), migrated.Body.String())
			}
			if tt.name == "create" {
				if !strings.Contains(migrated.Body.String(), `"item":"book"`) {
					t.Fatal("handler did not return created order")
				}
				if trace := strings.Join(app.Trace("migrated"), ","); trace != "audit.before,auth,validate,handler,audit.after" {
					t.Errorf("execution trace=%q", trace)
				}
			}
			if tt.name == "auth_short_circuit" && strings.Contains(strings.Join(app.Trace("migrated"), ","), "handler") {
				t.Fatal("unauthenticated handler executed")
			}
		})
	}
}

func TestSwitchAndRollback(t *testing.T) {
	app := newApp(t)
	for _, mode := range []string{"original", "migrated", "rollback"} {
		entry, err := app.Entry(mode)
		if err != nil {
			t.Fatal(err)
		}
		expected := "original"
		if mode == "migrated" {
			expected = "migrated"
		}
		w := request(entry, "GET", "/orders/1", "", "demo-token")
		if w.Code != 200 || w.Header().Get("X-Demo-Backend") != expected {
			t.Fatalf("mode=%s status=%d backend=%s", mode, w.Code, w.Header().Get("X-Demo-Backend"))
		}
		legacy := request(entry, "GET", "/legacy", "", "")
		if legacy.Code != 200 || legacy.Header().Get("X-Demo-Backend") != "original" {
			t.Fatal("unmigrated operation changed owner")
		}
	}
	if _, err := app.Entry("invalid"); err == nil {
		t.Fatal("invalid ownership accepted")
	}
}

func TestWriteFailureNeverFallsBack(t *testing.T) {
	app := newApp(t)
	entry, err := app.Entry("migrated")
	if err != nil {
		t.Fatal(err)
	}
	w := request(entry, "POST", "/orders", `{"item":"sold-out","quantity":1}`, "demo-token")
	if w.Code != 409 || w.Header().Get("X-Demo-Backend") != "migrated" {
		t.Fatalf("status=%d backend=%s", w.Code, w.Header().Get("X-Demo-Backend"))
	}
	if app.CreateCalls("original") != 0 || app.CreateCalls("migrated") != 1 {
		t.Fatalf("failed request replayed: original calls=%d migrated calls=%d", app.CreateCalls("original"), app.CreateCalls("migrated"))
	}
	if app.Writes("original") != 0 || app.Writes("migrated") != 0 {
		t.Fatal("failed write changed state")
	}
}

func TestCancelledContext(t *testing.T) {
	app := newApp(t)
	for name, h := range map[string]http.Handler{"original": app.Original, "migrated": app.Migrated} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r := httptest.NewRequest("POST", "/orders", strings.NewReader(`{"item":"book","quantity":1}`)).WithContext(ctx)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer demo-token")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 408 || app.Writes(name) != 0 {
			t.Fatalf("%s cancellation: status=%d writes=%d", name, w.Code, app.Writes(name))
		}
	}
}

func TestConcurrentRequests(t *testing.T) {
	app := newApp(t)
	for name, h := range map[string]http.Handler{"original": app.Original, "migrated": app.Migrated} {
		t.Run(name, func(t *testing.T) {
			var wg sync.WaitGroup
			for i := 0; i < 12; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					w := request(h, "POST", "/orders", `{"item":"book","quantity":1}`, "demo-token")
					if w.Code != 201 {
						t.Errorf("status=%d body=%s", w.Code, w.Body.String())
					}
				}()
			}
			wg.Wait()
			if app.Writes(name) != 12 {
				t.Errorf("writes=%d want=12", app.Writes(name))
			}
		})
	}
}

func TestUnmigratedDelegatesOriginalApplication(t *testing.T) {
	app := newApp(t)
	original := app.Original
	calls := 0
	app.Original = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; original.ServeHTTP(w, r) })
	entry, err := app.Entry("migrated")
	if err != nil {
		t.Fatal(err)
	}
	response := request(entry, "GET", "/legacy", "", "")
	if calls != 1 || response.Code != 200 {
		t.Fatalf("unmigrated operation bypassed original application: calls=%d status=%d", calls, response.Code)
	}
}
