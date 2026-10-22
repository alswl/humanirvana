package showcase

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// compareRequest is one step of a behavior comparison script. Steps run in
// order against isolated equivalent stores, so later steps see earlier writes.
type compareRequest struct {
	name        string
	method      string
	path        string
	headers     map[string]string
	body        string
	contentType string
	multipart   map[string]string // field -> value; "file" is sent as a file part
}

type observed struct {
	Status      int
	ContentType string
	Headers     map[string]string
	Body        any
	Trace       []string
	Writes      [3]int // task writes, message writes, handler calls
}

// Headers whose values are part of the compared contract.
var comparedHeaders = []string{"X-Project-ID", "X-Project-Name", "X-Showcase-Modifier", "Allow"}

// Fields that are expected to differ between runs and are normalized away.
// deadline is a wall-clock timestamp; sequence comes from a process-global
// request-scope counter shared by both arms.
var nondeterministicFields = map[string]bool{"deadline": true, "sequence": true}

func runCompareStep(t *testing.T, app *App, arm string, h http.Handler, step compareRequest) observed {
	t.Helper()
	var reader io.Reader = strings.NewReader(step.body)
	contentType := step.contentType
	if step.multipart != nil {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		for k, v := range step.multipart {
			if k == "file" {
				part, err := mw.CreateFormFile("file", "brief.txt")
				if err != nil {
					t.Fatal(err)
				}
				_, _ = part.Write([]byte(v))
				continue
			}
			_ = mw.WriteField(k, v)
		}
		_ = mw.Close()
		reader, contentType = &buf, mw.FormDataContentType()
	}
	req := httptest.NewRequest(step.method, step.path, reader)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range step.headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	out := observed{Status: w.Code, ContentType: w.Header().Get("Content-Type"), Headers: map[string]string{}}
	for _, k := range comparedHeaders {
		if v := w.Header().Get(k); v != "" {
			out.Headers[k] = v
		}
	}
	raw := w.Body.Bytes()
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err == nil {
		out.Body = normalize(decoded)
	} else {
		out.Body = string(raw)
	}
	out.Trace = app.Trace(arm)
	out.Writes[0], out.Writes[1], out.Writes[2] = app.SideEffects(arm)
	return out
}

func normalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			if nondeterministicFields[k] {
				x[k] = "<normalized>"
				continue
			}
			x[k] = normalize(child)
		}
	case []any:
		for i := range x {
			x[i] = normalize(x[i])
		}
	}
	return v
}

// compareArms runs the script on fresh Original and adapted apps and returns
// one line per differing step.
func compareArms(t *testing.T, script []compareRequest) []string {
	t.Helper()
	original, err := New()
	if err != nil {
		t.Fatal(err)
	}
	adapted, err := New()
	if err != nil {
		t.Fatal(err)
	}
	var diffs []string
	for _, step := range script {
		o := runCompareStep(t, original, "original", original.Original, step)
		a := runCompareStep(t, adapted, "adapted", adapted.Build, step)
		t.Logf("%-34s %d %s trace=%v effects=%v", step.name, o.Status, o.ContentType, o.Trace, o.Writes)
		if !reflect.DeepEqual(o, a) {
			ob, _ := json.Marshal(o)
			ab, _ := json.Marshal(a)
			diffs = append(diffs, fmt.Sprintf("%s: original=%s adapted=%s", step.name, ob, ab))
		}
	}
	return diffs
}

var (
	compareToken = map[string]string{"Authorization": "Bearer showcase-token"}
	compareAllow = map[string]string{"Authorization": "Bearer showcase-token", "X-Demo-Policy": "allow"}
	compareJSON  = "application/json"
)

// fullCompareScript covers every legacy demo route, including error chains:
// filter rejection, auth middleware rejection, operator/validator rejection,
// handler errors, router 404/405 and media admission 415/406.
var fullCompareScript = []compareRequest{
	{name: "policy deny", method: http.MethodGet, path: "/projects/1", headers: compareToken},
	{name: "project read", method: http.MethodGet, path: "/projects/1", headers: compareAllow},
	{name: "project missing", method: http.MethodGet, path: "/projects/9/summary", headers: compareToken},
	{name: "summary no bearer", method: http.MethodGet, path: "/projects/1/summary"},
	{name: "summary meta", method: http.MethodGet, path: "/projects/1/summary", headers: compareToken},
	{name: "representation json", method: http.MethodGet, path: "/projects/1/representation", headers: map[string]string{"Authorization": "Bearer showcase-token", "Accept": "application/json"}},
	{name: "representation xml", method: http.MethodGet, path: "/projects/1/representation", headers: map[string]string{"Authorization": "Bearer showcase-token", "Accept": "application/xml"}},
	{name: "representation unsupported accept", method: http.MethodGet, path: "/projects/1/representation", headers: map[string]string{"Authorization": "Bearer showcase-token", "Accept": "application/cbor"}},
	{name: "create blank title", method: http.MethodPost, path: "/projects/1/tasks", headers: compareToken, contentType: compareJSON, body: `{"title":"  "}`},
	{name: "create reserved title", method: http.MethodPost, path: "/projects/1/tasks", headers: compareToken, contentType: compareJSON, body: `{"title":" INTERNAL: x "}`},
	{name: "create malformed json", method: http.MethodPost, path: "/projects/1/tasks", headers: compareToken, contentType: compareJSON, body: `{"title":`},
	{name: "create unsupported media", method: http.MethodPost, path: "/projects/1/tasks", headers: compareToken, contentType: "application/cbor", body: "x"},
	{name: "create missing project", method: http.MethodPost, path: "/projects/9/tasks", headers: compareToken, contentType: compareJSON, body: `{"title":"orphan task"}`},
	{name: "create empty body", method: http.MethodPost, path: "/projects/1/tasks", headers: compareToken, contentType: compareJSON},
	{name: "create unknown field", method: http.MethodPost, path: "/projects/1/tasks", headers: compareToken, contentType: compareJSON, body: `{"title":"with extra","color":"red"}`},
	{name: "create non-numeric project", method: http.MethodPost, path: "/projects/abc/tasks", headers: compareToken, contentType: compareJSON, body: `{"title":"bad path"}`},
	{name: "create task", method: http.MethodPost, path: "/projects/1/tasks", headers: compareToken, contentType: compareJSON, body: `{"title":"  SHIP DEMO  "}`},
	{name: "list tasks limit", method: http.MethodGet, path: "/projects/1/tasks?limit=1", headers: compareToken},
	{name: "list tasks", method: http.MethodGet, path: "/projects/1/tasks", headers: compareToken},
	{name: "list tasks repeated limit", method: http.MethodGet, path: "/projects/1/tasks?limit=1&limit=5", headers: compareToken},
	{name: "list tasks non-numeric limit", method: http.MethodGet, path: "/projects/1/tasks?limit=many", headers: compareToken},
	{name: "list tasks zero limit", method: http.MethodGet, path: "/projects/1/tasks?limit=0", headers: compareToken},
	{name: "add message", method: http.MethodPost, path: "/tasks/3/messages", headers: compareToken, contentType: compareJSON, body: `{"text":"ready"}`},
	{name: "add blank message", method: http.MethodPost, path: "/tasks/3/messages", headers: compareToken, contentType: compareJSON, body: `{"text":" "}`},
	{name: "read task", method: http.MethodGet, path: "/tasks/3", headers: compareToken},
	{name: "read missing task", method: http.MethodGet, path: "/tasks/99", headers: compareToken},
	{name: "update invalid state", method: http.MethodPut, path: "/tasks/1", headers: compareToken, contentType: compareJSON, body: `{"title":"revised","state":"later"}`},
	{name: "update task", method: http.MethodPut, path: "/tasks/1", headers: compareToken, contentType: compareJSON, body: `{"title":" revised task ","state":"done"}`},
	{name: "text operator", method: http.MethodPut, path: "/tasks/1/text", headers: map[string]string{"Authorization": "Bearer showcase-token", "Accept": "text/plain, application/json"}, contentType: "text/plain", body: "hello"},
	{name: "async create", method: http.MethodPost, path: "/tasks/1/async", headers: map[string]string{"Authorization": "Bearer showcase-token", "Accept": "application/json"}},
	{name: "job read", method: http.MethodGet, path: "/jobs/1", headers: map[string]string{"Accept": "application/json"}},
	{name: "job complete", method: http.MethodPut, path: "/jobs/1/complete", headers: map[string]string{"Accept": "application/json"}, contentType: compareJSON, body: `{}`},
	{name: "job missing", method: http.MethodGet, path: "/jobs/9", headers: map[string]string{"Accept": "application/json"}},
	{name: "delete task", method: http.MethodDelete, path: "/tasks/1", headers: map[string]string{"Authorization": "Bearer showcase-token", "Accept": "application/json"}, contentType: compareJSON, body: `{}`},
	{name: "delete again", method: http.MethodDelete, path: "/tasks/1", headers: map[string]string{"Authorization": "Bearer showcase-token", "Accept": "application/json"}, contentType: compareJSON, body: `{}`},
	{name: "unmigrated legacy", method: http.MethodGet, path: "/legacy/status"},
	{name: "unknown route", method: http.MethodGet, path: "/nope"},
	{name: "method not allowed", method: http.MethodPatch, path: "/legacy/status"},
	{name: "modifier", method: http.MethodGet, path: "/modifier/1"},
	{name: "auto context", method: http.MethodGet, path: "/showcase/projects/1/context", headers: map[string]string{"X-Request-ID": "cmp-1"}},
	{name: "prefab scope", method: http.MethodGet, path: "/showcase/projects/1/scope", headers: map[string]string{"X-Request-ID": "cmp-2"}},
	{name: "multipart upload", method: http.MethodPost, path: "/showcase/projects/1/attachments", multipart: map[string]string{"label": "design", "file": "brief"}},
	{name: "multipart missing project", method: http.MethodPost, path: "/showcase/projects/9/attachments", multipart: map[string]string{"label": "design", "file": "brief"}},
}

func TestBehaviorComparisonOriginalVersusAdaptedFullScript(t *testing.T) {
	if diffs := compareArms(t, fullCompareScript); len(diffs) > 0 {
		t.Fatalf("original and adapted differ on %d of %d steps:\n%s", len(diffs), len(fullCompareScript), strings.Join(diffs, "\n"))
	}
}

func TestBehaviorComparisonDetectsDifference(t *testing.T) {
	// The comparison must not be vacuous: a request policy installed on only
	// one arm has to show up as a difference.
	original, err := New()
	if err != nil {
		t.Fatal(err)
	}
	adapted, err := New()
	if err != nil {
		t.Fatal(err)
	}
	step := compareRequest{name: "policy deny", method: http.MethodGet, path: "/projects/1", headers: compareToken}
	o := runCompareStep(t, original, "original", original.Original, step)
	bypass := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Demo-Policy", "allow")
		adapted.Build.ServeHTTP(w, r)
	})
	a := runCompareStep(t, adapted, "adapted", bypass, step)
	if reflect.DeepEqual(o, a) {
		t.Fatalf("comparison missed a policy difference: %+v", o)
	}
}
