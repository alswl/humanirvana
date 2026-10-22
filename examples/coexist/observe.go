package showcase

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

// ObservePrefix is reserved for the opt-in demo observation endpoints.
const ObservePrefix = "/_demo/"

type storeObservation struct {
	TaskWrites    int      `json:"task_writes"`
	MessageWrites int      `json:"message_writes"`
	HandlerCalls  int      `json:"handler_calls"`
	Trace         []string `json:"trace"`
}

type observation struct {
	Original     storeObservation `json:"original"`
	Adapted      storeObservation `json:"adapted"`
	CutoverOwner string           `json:"cutover_owner,omitempty"`
}

// Observe wraps a demo entry point with test-only endpoints so black-box
// tests can read side effects and switch cutover ownership in one process.
// cutover may be nil; owner is its current owner. It is demo wiring only and
// not part of the humanirvana library.
func (a *App) Observe(next http.Handler, cutover *RouteCutover, owner string) http.Handler {
	var mu sync.Mutex
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, ObservePrefix) {
			next.ServeHTTP(w, r)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case ObservePrefix + "observe":
			if r.Method != http.MethodGet {
				writeObserveError(w, http.StatusMethodNotAllowed, "use GET")
				return
			}
		case ObservePrefix + "cutover":
			if cutover == nil {
				writeObserveError(w, http.StatusNotFound, "cutover is not enabled for this arm")
				return
			}
			if r.Method != http.MethodPost {
				writeObserveError(w, http.StatusMethodNotAllowed, "use POST ?owner=original|adapted")
				return
			}
			requested := r.URL.Query().Get("owner")
			if err := cutover.SetOwner(requested); err != nil {
				writeObserveError(w, http.StatusBadRequest, err.Error())
				return
			}
			owner = requested
		default:
			writeObserveError(w, http.StatusNotFound, "unknown demo endpoint")
			return
		}
		out := observation{Original: a.observeStore("original"), Adapted: a.observeStore("adapted")}
		if cutover != nil {
			out.CutoverOwner = owner
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
}

func (a *App) observeStore(arm string) storeObservation {
	tasks, messages, calls := a.SideEffects(arm)
	trace := a.Trace(arm)
	if trace == nil {
		trace = []string{}
	}
	return storeObservation{TaskWrites: tasks, MessageWrites: messages, HandlerCalls: calls, Trace: trace}
}

func writeObserveError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
}
