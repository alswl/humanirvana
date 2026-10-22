package showcase

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func observeRequest(t *testing.T, server *httptest.Server, method, path, body string, headers map[string]string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, data
}

func readObservation(t *testing.T, server *httptest.Server) observation {
	t.Helper()
	status, data := observeRequest(t, server, http.MethodGet, "/_demo/observe", "", nil)
	if status != http.StatusOK {
		t.Fatalf("observe status=%d body=%s", status, data)
	}
	var out observation
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

var observeToken = map[string]string{"Authorization": "Bearer showcase-token", "Content-Type": "application/json"}

func TestObserveExposesCallsAndMiddlewareOrderOverHTTP(t *testing.T) {
	for _, arm := range []string{"original", "adapted"} {
		app, err := New()
		if err != nil {
			t.Fatal(err)
		}
		handler, err := app.Handler(arm)
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(app.Observe(handler, nil, ""))
		side := func(o observation) storeObservation {
			if arm == "original" {
				return o.Original
			}
			return o.Adapted
		}
		if status, _ := observeRequest(t, server, http.MethodGet, "/projects/1", "", observeToken); status != http.StatusForbidden {
			t.Fatalf("%s policy status=%d", arm, status)
		}
		if got := side(readObservation(t, server)); got.HandlerCalls != 0 || len(got.Trace) != 0 {
			t.Fatalf("%s policy denial reached middleware or handler: %+v", arm, got)
		}
		if status, _ := observeRequest(t, server, http.MethodPost, "/projects/1/tasks", `{"title":"denied"}`, map[string]string{"Content-Type": "application/json"}); status != http.StatusUnauthorized {
			t.Fatalf("%s auth status=%d", arm, status)
		}
		got := side(readObservation(t, server))
		if got.HandlerCalls != 0 || got.TaskWrites != 0 || !reflect.DeepEqual(got.Trace, []string{"audit.before", "auth", "audit.after"}) {
			t.Fatalf("%s auth short-circuit: %+v", arm, got)
		}
		if status, _ := observeRequest(t, server, http.MethodPost, "/projects/1/tasks", `{"title":"observed"}`, observeToken); status != http.StatusCreated {
			t.Fatalf("%s create status=%d", arm, status)
		}
		got = side(readObservation(t, server))
		if got.HandlerCalls != 1 || got.TaskWrites != 1 || !reflect.DeepEqual(got.Trace, []string{"audit.before", "auth", "handler", "audit.after"}) {
			t.Fatalf("%s create chain: %+v", arm, got)
		}
		if status, _ := observeRequest(t, server, http.MethodPost, "/_demo/cutover?owner=adapted", "", nil); status != http.StatusNotFound {
			t.Fatalf("%s cutover endpoint without cutover status=%d", arm, status)
		}
		server.Close()
	}

	app, err := New()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Observe(app.Composed, nil, ""))
	defer server.Close()
	for i := 1; i <= 2; i++ {
		if status, _ := observeRequest(t, server, http.MethodGet, "/native/health", "", nil); status != http.StatusOK {
			t.Fatalf("native health status=%d", status)
		}
		got := readObservation(t, server).Adapted
		if got.HandlerCalls != i || !reflect.DeepEqual(got.Trace, []string{"native.before", "handler", "native.after"}) {
			t.Fatalf("typed Huma request %d: %+v", i, got)
		}
	}
}

func TestObserveSwitchesCutoverInProcessWithoutReplay(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatal(err)
	}
	cutover := NewRouteCutover(app, http.MethodPost, "/projects/{projectID}/tasks")
	server := httptest.NewServer(app.Observe(cutover, cutover, "original"))
	defer server.Close()
	create := func() int {
		status, data := observeRequest(t, server, http.MethodPost, "/projects/1/tasks", `{"title":"cutover"}`, observeToken)
		if status != http.StatusCreated {
			t.Fatalf("create status=%d body=%s", status, data)
		}
		var task Task
		if err := json.Unmarshal(data, &task); err != nil {
			t.Fatal(err)
		}
		return task.ID
	}
	switchOwner := func(owner string, want int) {
		status, data := observeRequest(t, server, http.MethodPost, "/_demo/cutover?owner="+owner, "", nil)
		if status != want {
			t.Fatalf("switch %q status=%d body=%s", owner, status, data)
		}
	}
	ids := []int{create()}
	switchOwner("adapted", http.StatusOK)
	ids = append(ids, create())
	switchOwner("typo", http.StatusBadRequest)
	switchOwner("original", http.StatusOK)
	ids = append(ids, create())
	if !reflect.DeepEqual(ids, []int{2, 2, 3}) {
		t.Fatalf("ids=%v", ids)
	}
	got := readObservation(t, server)
	if got.CutoverOwner != "original" || got.Original.TaskWrites != 2 || got.Original.HandlerCalls != 2 || got.Adapted.TaskWrites != 1 || got.Adapted.HandlerCalls != 1 {
		t.Fatalf("cutover side effects: %+v", got)
	}
	if status, _ := observeRequest(t, server, http.MethodGet, "/_demo/cutover", "", nil); status != http.StatusMethodNotAllowed {
		t.Fatalf("GET cutover status=%d", status)
	}
}

func TestCutoverSwitchDuringConcurrentWritesNeitherDropsNorReplays(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatal(err)
	}
	cutover := NewRouteCutover(app, http.MethodPost, "/projects/{projectID}/tasks")
	const writers, perWriter = 8, 25
	stop := make(chan struct{})
	switched := make(chan struct{})
	go func() {
		defer close(switched)
		owners := []string{"adapted", "original"}
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if err := cutover.SetOwner(owners[i%2]); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	var wg sync.WaitGroup
	statuses := make(chan int, writers*perWriter)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				req := httptest.NewRequest(http.MethodPost, "/projects/1/tasks", strings.NewReader(`{"title":"concurrent"}`))
				req.Header.Set("Authorization", "Bearer showcase-token")
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				cutover.ServeHTTP(rec, req)
				statuses <- rec.Code
			}
		}()
	}
	wg.Wait()
	close(stop)
	<-switched
	close(statuses)
	for code := range statuses {
		if code != http.StatusCreated {
			t.Fatalf("write status=%d", code)
		}
	}
	oTasks, _, oCalls := app.SideEffects("original")
	aTasks, _, aCalls := app.SideEffects("adapted")
	if oTasks+aTasks != writers*perWriter || oCalls+aCalls != writers*perWriter {
		t.Fatalf("writes original=%d adapted=%d calls original=%d adapted=%d, want total %d", oTasks, aTasks, oCalls, aCalls, writers*perWriter)
	}
	if oTasks == 0 || aTasks == 0 {
		t.Logf("switching did not interleave owners this run: original=%d adapted=%d", oTasks, aTasks)
	}
}
