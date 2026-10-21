package showcase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"reflect"
	"strings"
	"testing"

	"github.com/alswl/humanirvana"
	"github.com/caicloud/nirvana/definition"
	nrest "github.com/caicloud/nirvana/service/rest"
)

type response struct {
	status      int
	contentType string
	requestID   string
	projectID   string
	projectName string
	body        any
}

func TestOriginalOnlyAutoContextAndMultipartAttachment(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatal(err)
	}

	probeReq := httptest.NewRequest(http.MethodGet, "/showcase/projects/1/context", nil)
	probeReq.Header.Set("X-Request-ID", "ctx-demo-17")
	probeW := httptest.NewRecorder()
	app.Original.ServeHTTP(probeW, probeReq)
	if probeW.Code != http.StatusOK || !bytes.Contains(probeW.Body.Bytes(), []byte(`"request_id":"ctx-demo-17"`)) || !bytes.Contains(probeW.Body.Bytes(), []byte(`"context_alive":true`)) || !bytes.Contains(probeW.Body.Bytes(), []byte(`"has_deadline":true`)) {
		t.Fatalf("Auto/context status=%d body=%s", probeW.Code, probeW.Body.String())
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("label", "design"); err != nil {
		t.Fatal(err)
	}
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="file"; filename="brief.txt"`)
	h.Set("Content-Type", "text/plain")
	part, err := mw.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("brief"))
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/showcase/projects/1/attachments", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	app.Original.ServeHTTP(w, req)
	if w.Code != http.StatusCreated || !bytes.Contains(w.Body.Bytes(), []byte(`"size":5`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"label":"design"`)) {
		t.Fatalf("multipart upload status=%d body=%s", w.Code, w.Body.String())
	}
	app.originalStore.mu.Lock()
	defer app.originalStore.mu.Unlock()
	if len(app.originalStore.attachments) != 1 {
		t.Fatalf("attachments=%d", len(app.originalStore.attachments))
	}
}

func TestOriginalOnlyFeatureIsNotSilentlyExposedByBuild(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/showcase/projects/1/context", "/showcase/projects/1/attachments", "/showcase/projects/1/scope"} {
		w := httptest.NewRecorder()
		app.Build.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("Build unexpectedly exposed %s: %d %s", path, w.Code, w.Body.String())
		}
	}
}

func TestPrefabRequestScopeAndSingletonInOriginal(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatal(err)
	}
	first := call(t, app.Original, http.MethodGet, "/showcase/projects/1/scope", "", "")
	second := call(t, app.Original, http.MethodGet, "/showcase/projects/1/scope", "", "")
	if first.status != http.StatusOK || second.status != http.StatusOK {
		t.Fatalf("responses=%+v %+v", first, second)
	}
	var a, b struct {
		Request struct {
			RequestID string  `json:"request_id"`
			Sequence  float64 `json:"sequence"`
		} `json:"request_scope"`
		Config struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"config"`
	}
	ja, _ := json.Marshal(first.body)
	jb, _ := json.Marshal(second.body)
	if err := json.Unmarshal(ja, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(jb, &b); err != nil {
		t.Fatal(err)
	}
	if a.Request.RequestID != "showcase-test" || b.Request.Sequence <= a.Request.Sequence || a.Config.Name != "showcase" || a.Config.Version != "v1" || b.Config != a.Config {
		t.Fatalf("first=%s second=%s", ja, jb)
	}
}

func TestLegacyBuilderKeepsNirvanaParameterSources(t *testing.T) {
	cases := []struct {
		source definition.Source
		name   string
		fn     any
	}{
		{definition.Auto, "auto", func(_ context.Context, in struct {
			Value string `source:"Header,X-Value"`
		}) (string, error) {
			return in.Value, nil
		}},
		{definition.Form, "form", func(_ context.Context, value string) (string, error) { return value, nil }},
		{definition.File, "file", func(_ context.Context, value multipart.File) (string, error) { return "", nil }},
		{definition.Prefab, "showcase-config-singleton", func(_ context.Context, value *showcaseConfig) (string, error) { return value.Name, nil }},
	}
	for _, tc := range cases {
		d := definition.Descriptor{Path: "/" + tc.name, Definitions: []definition.Definition{{Method: definition.Get, Function: tc.fn, Parameters: []definition.Parameter{{Source: definition.Prefab, Name: "context"}, {Source: tc.source, Name: tc.name}}, Results: definition.DataErrorResults("unsupported"), Consumes: []string{definition.MIMENone}, Produces: []string{definition.MIMEJSON}, ErrorProduces: []string{definition.MIMEJSON}}}}
		builder := nrest.NewBuilder()
		if err := humanirvana.RegisterLegacy(builder, []definition.Descriptor{d}); err != nil {
			t.Fatalf("source=%s register: %v", tc.source, err)
		}
		if _, err := builder.Build(); err != nil {
			t.Fatalf("source=%s build: %v", tc.source, err)
		}
	}
}

func call(t *testing.T, h http.Handler, method, path, token, body string) response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Request-ID", "showcase-test")
	req.Header.Set("X-Demo-Policy", "allow")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var decoded any
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &decoded); err != nil {
			decoded = w.Body.String()
		}
	}
	return response{status: w.Code, contentType: w.Header().Get("Content-Type"), requestID: w.Header().Get("X-Request-ID"), projectID: w.Header().Get("X-Project-ID"), projectName: w.Header().Get("X-Project-Name"), body: decoded}
}

func TestNirvanaFilterShortCircuitsBeforeRouting(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/projects/1", nil)
	w := httptest.NewRecorder()
	app.Original.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "demo request policy denied") {
		t.Fatalf("filter status=%d body=%s", w.Code, w.Body.String())
	}
	calls, _, _ := app.SideEffects("original")
	if calls != 0 {
		t.Fatalf("filtered request reached handler: calls=%d", calls)
	}
}

func TestDefinitionModifierChangesRuntimeMetadata(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	app.Original.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/modifier/1", nil))
	if w.Code != http.StatusOK || w.Header().Get("X-Showcase-Modifier") != "project-1" {
		t.Fatalf("modifier status=%d header=%q body=%s", w.Code, w.Header().Get("X-Showcase-Modifier"), w.Body.String())
	}
}

func TestTaskUpdateDeleteSemanticsAndOriginalBuildParity(t *testing.T) {
	for _, arm := range []string{"original", "build"} {
		app, err := New()
		if err != nil {
			t.Fatal(err)
		}
		h, err := app.Handler(arm)
		if err != nil {
			t.Fatal(err)
		}
		updated := call(t, h, http.MethodPut, "/tasks/1", "Bearer showcase-token", `{"title":"  revised task  ","state":"done"}`)
		if updated.status != http.StatusOK {
			t.Fatalf("%s update=%+v", arm, updated)
		}
		b, _ := json.Marshal(updated.body)
		if !bytes.Contains(b, []byte(`"title":"revised task"`)) || !bytes.Contains(b, []byte(`"state":"done"`)) {
			t.Fatalf("%s updated body=%s", arm, b)
		}
		invalid := call(t, h, http.MethodPut, "/tasks/1", "Bearer showcase-token", `{"title":"x","state":"impossible"}`)
		missing := call(t, h, http.MethodPut, "/tasks/99", "Bearer showcase-token", `{"title":"valid title","state":"open"}`)
		if invalid.status < 400 || missing.status < 400 {
			t.Fatalf("%s invalid=%+v missing=%+v", arm, invalid, missing)
		}
		deleted := call(t, h, http.MethodDelete, "/tasks/1", "Bearer showcase-token", `{}`)
		absent := call(t, h, http.MethodGet, "/tasks/1", "Bearer showcase-token", "")
		if deleted.status != http.StatusNoContent || absent.status < 400 {
			t.Fatalf("%s delete=%+v absent=%+v", arm, deleted, absent)
		}
		deletedAgain := call(t, h, http.MethodDelete, "/tasks/1", "Bearer showcase-token", `{}`)
		if deletedAgain.status < 400 {
			t.Fatalf("%s repeated delete=%+v", arm, deletedAgain)
		}
		writes, _, calls := app.SideEffects(arm)
		if writes != 2 || calls != 5 {
			t.Fatalf("%s writes=%d calls=%d", arm, writes, calls)
		}
	}
}

func TestAsyncCreateJobStateTransitionOriginalAndBuild(t *testing.T) {
	for _, arm := range []string{"original", "build"} {
		app, err := New()
		if err != nil {
			t.Fatal(err)
		}
		h, err := app.Handler(arm)
		if err != nil {
			t.Fatal(err)
		}
		queued := call(t, h, http.MethodPost, "/tasks/1/async", "Bearer showcase-token", "")
		if queued.status != http.StatusAccepted {
			t.Fatalf("%s async create=%+v", arm, queued)
		}
		var j taskJob
		b, _ := json.Marshal(queued.body)
		if err := json.Unmarshal(b, &j); err != nil || j.State != "queued" || j.ID == "" {
			t.Fatalf("%s queued job=%s err=%v", arm, b, err)
		}
		before := call(t, h, http.MethodGet, "/jobs/"+j.ID, "", "")
		if before.status != http.StatusOK || !strings.Contains(fmt.Sprint(before.body), "queued") {
			t.Fatalf("%s before=%+v", arm, before)
		}
		completed := call(t, h, http.MethodPut, "/jobs/"+j.ID+"/complete", "", `{}`)
		after := call(t, h, http.MethodGet, "/jobs/"+j.ID, "", "")
		if completed.status != http.StatusOK || !strings.Contains(fmt.Sprint(after.body), "completed") {
			t.Fatalf("%s complete=%+v after=%+v", arm, completed, after)
		}
	}
}

func TestTextPlainConsumerProducerAndResultOperatorBothArms(t *testing.T) {
	for _, arm := range []string{"original", "build"} {
		app, err := New()
		if err != nil {
			t.Fatal(err)
		}
		h, err := app.Handler(arm)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPut, "/tasks/1/text", strings.NewReader("hello"))
		req.Header.Set("Authorization", "Bearer showcase-token")
		req.Header.Set("Content-Type", "text/plain")
		req.Header.Set("Accept", "text/plain, application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") || w.Body.String() != "result:HELLO" {
			t.Fatalf("%s text status=%d type=%q body=%q", arm, w.Code, w.Header().Get("Content-Type"), w.Body.String())
		}
	}
}

func TestMetaDestinationSetsHeadersInOriginalAndBuild(t *testing.T) {
	for _, arm := range []string{"original", "build"} {
		app, err := New()
		if err != nil {
			t.Fatal(err)
		}
		h, err := app.Handler(arm)
		if err != nil {
			t.Fatal(err)
		}
		got := call(t, h, http.MethodGet, "/projects/1/summary", "Bearer showcase-token", "")
		if got.status != http.StatusOK || got.projectID != "1" || got.projectName != "demo project" {
			t.Fatalf("arm=%s summary=%+v", arm, got)
		}
	}
}

func TestAcceptNegotiatesJSONAndXMLInOriginalAndBuild(t *testing.T) {
	for _, arm := range []string{"original", "build"} {
		app, err := New()
		if err != nil {
			t.Fatal(err)
		}
		h, err := app.Handler(arm)
		if err != nil {
			t.Fatal(err)
		}
		for _, accept := range []string{"application/json", "application/xml"} {
			req := httptest.NewRequest(http.MethodGet, "/projects/1/representation", nil)
			req.Header.Set("Authorization", "Bearer showcase-token")
			req.Header.Set("Accept", accept)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), accept) || !strings.Contains(w.Body.String(), "demo project") {
				t.Fatalf("arm=%s accept=%s status=%d content-type=%q body=%s", arm, accept, w.Code, w.Header().Get("Content-Type"), w.Body.String())
			}
		}
	}
}

func TestPerOperationCutoverRollbackHasSingleOwner(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatal(err)
	}
	route := NewRouteCutover(app, http.MethodPost, "/projects/{projectID}/tasks")
	create := func(title string) response {
		return call(t, route, http.MethodPost, "/projects/1/tasks", "Bearer showcase-token", `{"title":"`+title+`"}`)
	}
	originalFirst := create("before cutover")
	if err := route.SetOwner("adapted"); err != nil {
		t.Fatal(err)
	}
	adapted := create("during cutover")
	if err := route.SetOwner("original"); err != nil {
		t.Fatal(err)
	}
	originalAgain := create("after rollback")
	if originalFirst.status != http.StatusCreated || adapted.status != http.StatusCreated || originalAgain.status != http.StatusCreated {
		t.Fatalf("cutover statuses: %d %d %d", originalFirst.status, adapted.status, originalAgain.status)
	}
	var first, middle, last struct {
		ID int `json:"id"`
	}
	decode := func(v any, out any) {
		b, _ := json.Marshal(v)
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatal(err)
		}
	}
	decode(originalFirst.body, &first)
	decode(adapted.body, &middle)
	decode(originalAgain.body, &last)
	if first.ID != 2 || middle.ID != 2 || last.ID != 3 {
		t.Fatalf("stores were not isolated across one-owner cutover: ids=%d/%d/%d", first.ID, middle.ID, last.ID)
	}
	failed := create(" INTERNAL: rejected ")
	if failed.status < 400 {
		t.Fatalf("expected rejected adapted/original request, got %+v", failed)
	}
	legacy := call(t, route, http.MethodGet, "/legacy/status", "", "")
	if legacy.status != http.StatusOK {
		t.Fatalf("unmigrated original route was lost: %+v", legacy)
	}
	oTasks, oMessages, oCalls := app.SideEffects("original")
	bTasks, bMessages, bCalls := app.SideEffects("adapted")
	if oTasks != 2 || oMessages != 0 || oCalls != 2 || bTasks != 1 || bMessages != 0 || bCalls != 1 {
		t.Fatalf("duplicate or replayed effects original=(%d,%d,%d) adapted=(%d,%d,%d)", oTasks, oMessages, oCalls, bTasks, bMessages, bCalls)
	}
}

func TestOriginalAndBuildSharedHandlerDomainParityAndSingleWrites(t *testing.T) {
	apps := make([]*App, 2)
	arms := []string{"original", "build"}
	var got [2][]response
	for i, arm := range arms {
		app, err := New()
		if err != nil {
			t.Fatal(err)
		}
		apps[i] = app
		h, err := app.Handler(arm)
		if err != nil {
			t.Fatal(err)
		}
		got[i] = append(got[i], call(t, h, http.MethodGet, "/projects/1", "Bearer showcase-token", ""))
		got[i] = append(got[i], call(t, h, http.MethodPost, "/projects/1/tasks", "Bearer showcase-token", `{"title":"  SHIP DEMO  "}`))
		if trace := apps[i].Trace(arm); !reflect.DeepEqual(trace, []string{"audit.before", "auth", "handler", "audit.after"}) {
			t.Fatalf("arm=%s middleware trace=%v", arm, trace)
		}
		got[i] = append(got[i], call(t, h, http.MethodPost, "/tasks/2/messages", "Bearer showcase-token", `{"text":"ready"}`))
		got[i] = append(got[i], call(t, h, http.MethodGet, "/tasks/2", "Bearer showcase-token", ""))
		got[i] = append(got[i], call(t, h, http.MethodGet, "/projects/1/tasks?limit=1", "Bearer showcase-token", ""))
		got[i] = append(got[i], call(t, h, http.MethodGet, "/legacy/status", "", ""))
	}
	for i := range got[0] {
		if !reflect.DeepEqual(got[0][i], got[1][i]) {
			t.Fatalf("step %d original=%+v Build=%+v", i, got[0][i], got[1][i])
		}
		if got[0][i].status < 200 || got[0][i].status >= 300 {
			t.Fatalf("step %d unexpected status: %+v", i, got[0][i])
		}
	}
	created, _ := json.Marshal(got[0][1].body)
	if !bytes.Contains(created, []byte(`"title":"ship demo"`)) {
		t.Fatalf("converter did not normalize title: %s", created)
	}
	for i, arm := range arms {
		tasks, msgs, calls := apps[i].SideEffects(arm)
		if tasks != 1 || msgs != 1 || calls != 5 {
			t.Fatalf("arm=%s effects tasks=%d messages=%d handler_calls=%d", arm, tasks, msgs, calls)
		}
	}
}

func TestAuthAndValidationFailuresDoNotWriteOrReplay(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatal(err)
	}
	h, err := app.Handler("build")
	if err != nil {
		t.Fatal(err)
	}
	unauthorized := call(t, h, http.MethodPost, "/projects/1/tasks", "", `{"title":"no auth"}`)
	invalid := call(t, h, http.MethodPost, "/projects/1/tasks", "Bearer showcase-token", `{"title":"   "}`)
	reserved := call(t, h, http.MethodPost, "/projects/1/tasks", "Bearer showcase-token", `{"title":" INTERNAL: blocked "}`)
	if unauthorized.status < 400 || invalid.status < 400 || reserved.status < 400 {
		t.Fatalf("expected rejected requests: unauthorized=%+v invalid=%+v reserved=%+v", unauthorized, invalid, reserved)
	}
	tasks, msgs, calls := app.SideEffects("build")
	if tasks != 0 || msgs != 0 || calls != 0 {
		t.Fatalf("failed writes replayed or persisted: tasks=%d messages=%d calls=%d", tasks, msgs, calls)
	}
	legacy := call(t, h, http.MethodGet, "/legacy/status", "", "")
	if legacy.status != http.StatusOK {
		t.Fatalf("unmigrated route lost: %+v", legacy)
	}
}

func TestNativeEnhancementIsAdditiveAndDocumented(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	app.Build.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/native/health", nil))
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(`"contract":"native"`)) {
		t.Fatalf("native health status=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	app.Build.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/native/async/job-7", nil))
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(`"state":"completed"`)) {
		t.Fatalf("native status status=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	app.Build.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	data, err := io.ReadAll(w.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	paths := doc["paths"].(map[string]any)
	for _, p := range []string{"/native/health", "/native/version", "/native/async/{id}"} {
		if paths[p] == nil {
			t.Fatalf("OpenAPI missing %s", p)
		}
	}
	if paths["/projects/{projectID}/tasks"] != nil {
		t.Fatal("legacy Nirvana route unexpectedly projected into Huma OpenAPI")
	}
	w = httptest.NewRecorder()
	app.Build.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/native/version", nil))
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(`"version":"1.0.0"`)) {
		t.Fatalf("native version status=%d body=%s", w.Code, w.Body.String())
	}
}
