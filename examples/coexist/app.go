// Package showcase is a synthetic service used to compare the original Nirvana
// runtime with direct Huma registration plus the legacy adapter entry.
package showcase

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alswl/humanirvana"
	"github.com/caicloud/nirvana/definition"
	"github.com/caicloud/nirvana/operators/converter"
	"github.com/caicloud/nirvana/operators/validator"
	"github.com/caicloud/nirvana/service"
	nrest "github.com/caicloud/nirvana/service/rest"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type Project struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Tasks []Task `json:"tasks"`
}
type Task struct {
	ID        int       `json:"id"`
	ProjectID int       `json:"project_id"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	Messages  []Message `json:"messages"`
}
type Message struct {
	ID     int    `json:"id"`
	TaskID int    `json:"task_id"`
	Text   string `json:"text"`
}
type CreateTask struct {
	Title string `json:"title" validate:"required,min=3"`
}
type CreateMessage struct {
	Text string `json:"text"`
}
type UpdateTask struct {
	Title string `json:"title" validate:"required,min=3"`
	State string `json:"state" validate:"required,oneof=open done"`
}
type taskJob struct {
	ID     string `json:"id"`
	TaskID int    `json:"task_id"`
	State  string `json:"state"`
}
type taskQuery struct {
	Limit int `json:"limit"`
}

type contextProbe struct {
	RequestID string `source:"Header,X-Request-ID" json:"request_id"`
	ProjectID int    `source:"Path,projectID" json:"project_id"`
}

type attachment struct {
	Name  string `json:"name"`
	Size  int    `json:"size"`
	Label string `json:"label"`
}

type requestScope struct {
	RequestID string `json:"request_id"`
	Sequence  uint64 `json:"sequence"`
}
type showcaseConfig struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

var prefabSequence atomic.Uint64
var singletonConfig = &showcaseConfig{Name: "showcase", Version: "v1"}

type requestScopePrefab struct{}

func (requestScopePrefab) Name() string       { return "showcase-request-scope" }
func (requestScopePrefab) Type() reflect.Type { return reflect.TypeOf((*requestScope)(nil)) }
func (requestScopePrefab) Make(ctx context.Context) (interface{}, error) {
	id := ""
	if h := service.HTTPContextFrom(ctx); h != nil {
		id = h.Request().Header.Get("X-Request-ID")
	}
	return &requestScope{RequestID: id, Sequence: prefabSequence.Add(1)}, nil
}

type configPrefab struct{}

func (configPrefab) Name() string                              { return "showcase-config-singleton" }
func (configPrefab) Type() reflect.Type                        { return reflect.TypeOf((*showcaseConfig)(nil)) }
func (configPrefab) Make(context.Context) (interface{}, error) { return singletonConfig, nil }
func init() {
	_ = service.RegisterPrefab(requestScopePrefab{})
	_ = service.RegisterPrefab(configPrefab{})
}

type showcaseError struct {
	status  int
	message string
}

func (e showcaseError) Error() string        { return e.message }
func (e showcaseError) Code() int            { return e.status }
func (e showcaseError) Message() interface{} { return map[string]string{"message": e.message} }

type store struct {
	mu                                      sync.Mutex
	projects                                map[int]Project
	tasks                                   map[int]Task
	messages                                map[int]Message
	nextTask, nextMessage                   int
	taskWrites, messageWrites, handlerCalls int
	trace                                   []string
	attachments                             []attachment
	jobs                                    map[string]taskJob
	nextJob                                 int
}

func newStore() *store {
	return &store{projects: map[int]Project{1: {ID: 1, Name: "demo project"}}, tasks: map[int]Task{1: {ID: 1, ProjectID: 1, Title: "first task", State: "open"}}, messages: map[int]Message{}, jobs: map[string]taskJob{}, nextTask: 2, nextMessage: 1, nextJob: 1}
}

type traceKey struct{}

func record(ctx context.Context, v string) {
	if p, ok := ctx.Value(traceKey{}).(*[]string); ok {
		*p = append(*p, v)
	}
}
func (s *store) audit(ctx context.Context, next definition.Chain) error {
	events := []string{}
	ctx = context.WithValue(ctx, traceKey{}, &events)
	record(ctx, "audit.before")
	defer func() {
		record(ctx, "audit.after")
		s.mu.Lock()
		s.trace = append([]string(nil), events...)
		s.mu.Unlock()
	}()
	return next.Continue(ctx)
}
func (s *store) recordHuma(event string) {
	s.mu.Lock()
	s.trace = append(s.trace, event)
	s.mu.Unlock()
}

// startHumaTrace keeps only the current Huma request, like audit does for Nirvana.
func (s *store) startHumaTrace(event string) {
	s.mu.Lock()
	s.trace = []string{event}
	s.mu.Unlock()
}
func (s *store) authorize(ctx context.Context, next definition.Chain) error {
	record(ctx, "auth")
	if service.HTTPContextFrom(ctx).Request().Header.Get("Authorization") != "Bearer showcase-token" {
		return showcaseError{status: http.StatusUnauthorized, message: "authorization required"}
	}
	return next.Continue(ctx)
}
func (s *store) project(_ context.Context, id int) (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlerCalls++
	p, ok := s.projects[id]
	if !ok {
		return nil, fmt.Errorf("project %d not found", id)
	}
	return &p, nil
}
func (s *store) projectModifierMeta(_ context.Context, id int) (map[string]string, error) {
	return map[string]string{"X-Showcase-Modifier": fmt.Sprintf("project-%d", id)}, nil
}
func (s *store) createTask(ctx context.Context, projectID int, in CreateTask) (*Task, error) {
	record(ctx, "handler")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlerCalls++
	if _, ok := s.projects[projectID]; !ok {
		return nil, fmt.Errorf("project %d not found", projectID)
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, fmt.Errorf("task title is required")
	}
	t := Task{ID: s.nextTask, ProjectID: projectID, Title: title, State: "open"}
	s.nextTask++
	s.tasks[t.ID] = t
	s.taskWrites++
	return &t, nil
}
func (s *store) task(_ context.Context, id int) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlerCalls++
	t, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task %d not found", id)
	}
	return &t, nil
}
func (s *store) addMessage(ctx context.Context, taskID int, in CreateMessage) (*Message, error) {
	record(ctx, "handler")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlerCalls++
	if _, ok := s.tasks[taskID]; !ok {
		return nil, fmt.Errorf("task %d not found", taskID)
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return nil, fmt.Errorf("message text is required")
	}
	m := Message{ID: s.nextMessage, TaskID: taskID, Text: text}
	s.nextMessage++
	s.messages[m.ID] = m
	t := s.tasks[taskID]
	t.Messages = append(t.Messages, m)
	s.tasks[taskID] = t
	s.messageWrites++
	return &m, nil
}

func (s *store) updateTask(ctx context.Context, id int, in UpdateTask) (*Task, error) {
	record(ctx, "handler")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlerCalls++
	t, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task %d not found", id)
	}
	t.Title = in.Title
	t.State = in.State
	s.tasks[id] = t
	s.taskWrites++
	return &t, nil
}
func (s *store) deleteTask(ctx context.Context, id int) (*Task, error) {
	record(ctx, "handler")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlerCalls++
	t, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task %d not found", id)
	}
	delete(s.tasks, id)
	s.taskWrites++
	return &t, nil
}
func (s *store) updateTaskText(ctx context.Context, id int, text string) (string, error) {
	record(ctx, "handler")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlerCalls++
	t, ok := s.tasks[id]
	if !ok {
		return "", fmt.Errorf("task %d not found", id)
	}
	t.Title = text
	s.tasks[id] = t
	s.taskWrites++
	return text, nil
}

type taskTextResultOperator struct{}

func (taskTextResultOperator) Kind() string      { return "converter" }
func (taskTextResultOperator) In() reflect.Type  { return reflect.TypeOf("") }
func (taskTextResultOperator) Out() reflect.Type { return reflect.TypeOf("") }
func (taskTextResultOperator) Operate(_ context.Context, _ string, v interface{}) (interface{}, error) {
	return "result:" + strings.ToUpper(v.(string)), nil
}
func (s *store) enqueueTask(ctx context.Context, id int) (*taskJob, error) {
	record(ctx, "handler")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlerCalls++
	if _, ok := s.tasks[id]; !ok {
		return nil, fmt.Errorf("task %d not found", id)
	}
	jid := fmt.Sprint(s.nextJob)
	s.nextJob++
	j := taskJob{ID: jid, TaskID: id, State: "queued"}
	s.jobs[jid] = j
	return &j, nil
}
func (s *store) job(_ context.Context, id string) (*taskJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, fmt.Errorf("job %s not found", id)
	}
	return &j, nil
}
func (s *store) completeJob(_ context.Context, id string) (*taskJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, fmt.Errorf("job %s not found", id)
	}
	j.State = "completed"
	s.jobs[id] = j
	return &j, nil
}
func (s *store) listTasks(_ context.Context, projectID, limit int) ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlerCalls++
	if _, ok := s.projects[projectID]; !ok {
		return nil, fmt.Errorf("project %d not found", projectID)
	}
	ids := make([]int, 0)
	for id, t := range s.tasks {
		if t.ProjectID == projectID {
			ids = append(ids, id)
		}
	} // deterministic fixture IDs are sequential
	if limit <= 0 {
		limit = 20
	}
	out := make([]Task, 0, len(ids))
	for id := 1; id < s.nextTask && len(out) < limit; id++ {
		if t, ok := s.tasks[id]; ok && t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	return out, nil
}

func operations(s *store) []legacyOperation {
	jsonDef := func(method definition.Method, fn any, resultName string, params ...definition.Parameter) definition.Definition {
		return definition.Definition{Method: method, Function: fn, Parameters: params, Results: definition.DataErrorResults(resultName), Consumes: []string{definition.MIMEJSON}, Produces: []string{definition.MIMEJSON}, ErrorProduces: []string{definition.MIMEJSON}}
	}
	return []legacyOperation{
		{path: "/projects/{projectID}", d: jsonDef(definition.Get, s.project, "Project", definition.Parameter{Source: definition.Path, Name: "projectID"})},
		{path: "/projects/{projectID}/summary", d: definition.Definition{Method: definition.Get, Function: s.projectSummary, Parameters: []definition.Parameter{{Source: definition.Path, Name: "projectID"}}, Results: []definition.Result{definition.DataResultFor("Project summary"), definition.MetaResultFor("Project response metadata"), definition.ErrorResult()}, Produces: []string{definition.MIMEJSON}, ErrorProduces: []string{definition.MIMEJSON}}},
		{path: "/projects/{projectID}/representation", d: definition.Definition{Method: definition.Get, Function: s.projectRepresentation, Parameters: []definition.Parameter{{Source: definition.Path, Name: "projectID"}}, Results: definition.DataErrorResults("Project representation"), Produces: []string{definition.MIMEJSON, definition.MIMEXML}, ErrorProduces: []string{definition.MIMEJSON, definition.MIMEXML}}},
		{path: "/projects/{projectID}/tasks", d: jsonDef(definition.Create, s.createTask, "Task", definition.Parameter{Source: definition.Path, Name: "projectID"}, definition.Parameter{Source: definition.Body, Name: "task", Operators: []definition.Operator{
			converter.For(func(_ context.Context, _ string, in CreateTask) (CreateTask, error) {
				in.Title = strings.ToLower(strings.TrimSpace(in.Title))
				return in, nil
			}),
			validator.Struct(CreateTask{}),
			definition.OperatorFunc("validator", func(_ context.Context, _ string, in CreateTask) (CreateTask, error) {
				if strings.HasPrefix(in.Title, "internal:") {
					return in, fmt.Errorf("internal task titles are reserved")
				}
				return in, nil
			}),
		}})},
		{path: "/projects/{projectID}/tasks", d: jsonDef(definition.Get, s.listTasks, "Tasks", definition.Parameter{Source: definition.Path, Name: "projectID"}, definition.Parameter{Source: definition.Query, Name: "limit", Optional: true})},
		{path: "/tasks/{taskID}", d: jsonDef(definition.Get, s.task, "Task", definition.Parameter{Source: definition.Path, Name: "taskID"})},
		{path: "/tasks/{taskID}", d: jsonDef(definition.Update, s.updateTask, "Task", definition.Parameter{Source: definition.Path, Name: "taskID"}, definition.Parameter{Source: definition.Body, Name: "task", Operators: []definition.Operator{converter.For(func(_ context.Context, _ string, in UpdateTask) (UpdateTask, error) {
			in.Title = strings.TrimSpace(in.Title)
			return in, nil
		}), validator.Struct(UpdateTask{})}})},
		{path: "/tasks/{taskID}", d: definition.Definition{Method: definition.Delete, Function: s.deleteTask, Parameters: []definition.Parameter{{Source: definition.Path, Name: "taskID"}}, Results: definition.DataErrorResults("Deleted task"), Consumes: []string{definition.MIMEJSON}, Produces: []string{definition.MIMEJSON}, ErrorProduces: []string{definition.MIMEJSON}}},
		{path: "/tasks/{taskID}/messages", d: jsonDef(definition.Create, s.addMessage, "Message", definition.Parameter{Source: definition.Path, Name: "taskID"}, definition.Parameter{Source: definition.Body, Name: "message"})},
		{path: "/tasks/{taskID}/text", d: definition.Definition{Method: definition.Update, Function: s.updateTaskText, Parameters: []definition.Parameter{{Source: definition.Path, Name: "taskID"}, {Source: definition.Body, Name: "text"}}, Results: []definition.Result{{Destination: definition.Data, Description: "Task text", Operators: []definition.Operator{taskTextResultOperator{}}}, definition.ErrorResult()}, Consumes: []string{"text/plain"}, Produces: []string{"text/plain"}, ErrorProduces: []string{definition.MIMEJSON, "text/plain"}}},
		{path: "/tasks/{taskID}/async", d: definition.Definition{Method: definition.AsyncCreate, Function: s.enqueueTask, Parameters: []definition.Parameter{{Source: definition.Path, Name: "taskID"}}, Results: definition.DataErrorResults("Task job"), Consumes: []string{definition.MIMENone}, Produces: []string{definition.MIMEJSON}, ErrorProduces: []string{definition.MIMEJSON}}},
		{path: "/jobs/{jobID}/complete", d: jsonDef(definition.Update, s.completeJob, "Task job", definition.Parameter{Source: definition.Path, Name: "jobID"})},
		{path: "/jobs/{jobID}", d: jsonDef(definition.Get, s.job, "Task job", definition.Parameter{Source: definition.Path, Name: "jobID"})},
	}
}

func (s *store) probeContext(ctx context.Context, in contextProbe) (map[string]any, error) {
	deadline, hasDeadline := ctx.Deadline()
	return map[string]any{"project_id": in.ProjectID, "request_id": in.RequestID, "context_alive": ctx.Err() == nil, "has_deadline": hasDeadline, "deadline": deadline.UTC().Format(time.RFC3339Nano)}, nil
}

func (s *store) scopeInfo(_ context.Context, projectID int, request *requestScope, config *showcaseConfig) (map[string]any, error) {
	return map[string]any{"project_id": projectID, "request_scope": request, "config": config}, nil
}

func (s *store) projectSummary(ctx context.Context, id int) (map[string]any, map[string]string, error) {
	p, err := s.project(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	return map[string]any{"id": p.ID, "name": p.Name}, map[string]string{"X-Project-ID": fmt.Sprint(p.ID), "X-Project-Name": p.Name}, nil
}

func (s *store) projectRepresentation(ctx context.Context, id int) (*Project, error) {
	p, err := s.project(ctx, id)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *store) uploadAttachment(_ context.Context, projectID int, label string, file multipart.File) (attachment, error) {
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return attachment{}, err
	}
	item := attachment{Name: "stored-in-memory", Size: len(data), Label: label}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[projectID]; !ok {
		return attachment{}, fmt.Errorf("project %d not found", projectID)
	}
	s.attachments = append(s.attachments, item)
	return item, nil
}

type legacyOperation struct {
	path string
	d    definition.Definition
}

// App owns independent state for the original service and Build legacy route.
type App struct {
	Original, Build           http.Handler
	originalStore, buildStore *store
	Native, Composed          http.Handler
	routes                    int
}

func New() (*App, error) {
	a := &App{originalStore: newStore(), buildStore: newStore()}
	originalOps := operations(a.originalStore)
	buildOps := operations(a.buildStore)
	// Descriptor inheritance: the project subtree gets audit then auth exactly once.
	makeDescriptors := func(ops []legacyOperation, s *store) []definition.Descriptor {
		definitions := func(path string) []definition.Definition {
			result := definitionsAt(ops, path)
			for i := range result {
				if len(result[i].Parameters) == 0 || result[i].Parameters[0].Source != definition.Prefab || result[i].Parameters[0].Name != "context" {
					result[i].Parameters = append([]definition.Parameter{{Source: definition.Prefab, Name: "context"}}, result[i].Parameters...)
				}
			}
			return result
		}
		project := definition.Descriptor{Path: "/projects", Middlewares: []definition.Middleware{s.audit, s.authorize}, Children: []definition.Descriptor{
			{Path: "/{projectID}", Definitions: definitions("/projects/{projectID}")},
			{Path: "/{projectID}/summary", Definitions: definitions("/projects/{projectID}/summary")},
			{Path: "/{projectID}/representation", Definitions: definitions("/projects/{projectID}/representation")},
			{Path: "/{projectID}/tasks", Definitions: definitions("/projects/{projectID}/tasks")},
		}}
		tasks := definition.Descriptor{Path: "/tasks", Middlewares: []definition.Middleware{s.audit, s.authorize}, Children: []definition.Descriptor{
			{Path: "/{taskID}", Definitions: definitions("/tasks/{taskID}")},
			{Path: "/{taskID}/messages", Definitions: definitions("/tasks/{taskID}/messages")},
			{Path: "/{taskID}/text", Definitions: definitions("/tasks/{taskID}/text")},
			{Path: "/{taskID}/async", Definitions: definitions("/tasks/{taskID}/async")},
		}}
		jobs := definition.Descriptor{Path: "/jobs", Children: []definition.Descriptor{{Path: "/{jobID}/complete", Definitions: definitions("/jobs/{jobID}/complete")}, {Path: "/{jobID}", Definitions: definitions("/jobs/{jobID}")}}}
		unmigrated := definition.Descriptor{Path: "/legacy/status", Definitions: []definition.Definition{{Method: definition.Get, Function: func(context.Context) (map[string]string, error) {
			return map[string]string{"status": "original-only"}, nil
		}, Parameters: []definition.Parameter{{Source: definition.Prefab, Name: "context"}}, Results: definition.DataErrorResults("LegacyStatus"), Produces: []string{definition.MIMEJSON}, ErrorProduces: []string{definition.MIMEJSON}}}}
		return []definition.Descriptor{project, tasks, jobs, unmigrated}
	}
	builder := nrest.NewBuilder()
	builder.SetModifier(service.DefinitionModifiers{service.FirstContextParameter(), service.ConsumeNoneForHTTPGet(), service.ConsumeNoneForHTTPDelete(), service.ProduceNoneForHTTPDelete()}.Combine())
	builder.AddFilter(service.ParseRequestForm())
	builder.AddFilter(demoRequestPolicy)
	for _, d := range makeDescriptors(originalOps, a.originalStore) {
		if err := builder.AddDescriptor(d); err != nil {
			return nil, err
		}
	}
	modifierHandler, err := buildModifierHandler(a.originalStore, addDescriptors)
	if err != nil {
		return nil, err
	}
	if err := builder.AddDescriptor(extraDescriptor(a.originalStore)); err != nil {
		return nil, err
	}
	original, err := builder.Build()
	if err != nil {
		return nil, err
	}
	buildBuilder := nrest.NewBuilder()
	buildBuilder.AddFilter(service.ParseRequestForm())
	buildBuilder.AddFilter(demoRequestPolicy)
	buildBuilder.SetModifier(service.DefinitionModifiers{service.FirstContextParameter(), service.ConsumeNoneForHTTPGet(), service.ConsumeNoneForHTTPDelete(), service.ProduceNoneForHTTPDelete()}.Combine())
	buildAdapter := humanirvana.NewAdapter(buildBuilder)
	buildAPI := huma.NewAPI(huma.DefaultConfig("Nirvana Showcase", "1.0.0"), buildAdapter)
	if err := humanirvana.RegisterLegacy(buildBuilder, append(makeDescriptors(buildOps, a.buildStore), extraDescriptor(a.buildStore))); err != nil {
		return nil, err
	}
	if err := registerNative(buildAPI, a.buildStore); err != nil {
		return nil, err
	}
	built, err := buildBuilder.Build()
	if err != nil {
		return nil, err
	}
	if err := buildAdapter.Bind(built); err != nil {
		return nil, err
	}
	buildModifier, err := buildModifierHandler(a.buildStore, humanirvana.RegisterLegacy)
	if err != nil {
		return nil, err
	}
	a.Original = withModifierRoute(original, modifierHandler)
	a.Build = withModifierRoute(buildAdapter, buildModifier)
	a.Native = nativeService(a.buildStore)
	a.Composed = a.Build
	a.routes = 5
	return a, nil
}

// buildModifierHandler shows a DefinitionModifier on its own builder, so the
// modifier does not change the shared project/task routes.
func buildModifierHandler(s *store, register func(service.Builder, []definition.Descriptor) error) (http.Handler, error) {
	modifierDescriptor := definition.Descriptor{Path: "/modifier/{projectID}", Definitions: []definition.Definition{{Method: definition.Get, Function: s.projectModifierMeta, Parameters: []definition.Parameter{{Source: definition.Path, Name: "projectID"}}, Results: definition.DataErrorResults("Modifier metadata"), Produces: []string{definition.MIMEJSON}, ErrorProduces: []string{definition.MIMEJSON}}}}
	customModifier := func(d *definition.Definition) {
		d.Summary = "Modifier changed summary"
		d.Tags = []string{"modifier-demo"}
		d.Function = s.projectModifierMeta
		d.Results = []definition.Result{definition.MetaResultFor("Modifier metadata"), definition.ErrorResult()}
	}
	modifierBuilder := nrest.NewBuilder()
	modifierBuilder.SetModifier(service.DefinitionModifiers{service.FirstContextParameter(), service.ConsumeNoneForHTTPGet(), customModifier}.Combine())
	if err := register(modifierBuilder, []definition.Descriptor{modifierDescriptor}); err != nil {
		return nil, err
	}
	return modifierBuilder.Build()
}

// addDescriptors is the original application's plain Nirvana registration.
func addDescriptors(builder service.Builder, descriptors []definition.Descriptor) error {
	for _, d := range descriptors {
		if err := builder.AddDescriptor(d); err != nil {
			return err
		}
	}
	return nil
}

func withModifierRoute(next, modifier http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/modifier/") {
			modifier.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// extraDescriptor covers Auto, Prefab, Form and File parameter sources and a
// node middleware that derives a deadline context.
func extraDescriptor(s *store) definition.Descriptor {
	return definition.Descriptor{Path: "/showcase", Children: []definition.Descriptor{
		{Path: "/projects/{projectID}/scope", Definitions: []definition.Definition{{Method: definition.Get, Function: s.scopeInfo, Parameters: []definition.Parameter{{Source: definition.Path, Name: "projectID"}, {Source: definition.Prefab, Name: "showcase-request-scope"}, {Source: definition.Prefab, Name: "showcase-config-singleton"}}, Results: definition.DataErrorResults("ScopeInfo"), Produces: []string{definition.MIMEJSON}, ErrorProduces: []string{definition.MIMEJSON}}}},
		{Path: "/projects/{projectID}/context", Middlewares: []definition.Middleware{func(ctx context.Context, next definition.Chain) error {
			child, cancel := context.WithTimeout(ctx, time.Minute)
			defer cancel()
			return next.Continue(child)
		}}, Definitions: []definition.Definition{{Method: definition.Get, Function: s.probeContext, Parameters: []definition.Parameter{{Source: definition.Auto, Name: "probe"}}, Results: definition.DataErrorResults("ContextProbe"), Produces: []string{definition.MIMEJSON}, ErrorProduces: []string{definition.MIMEJSON}}}},
		{Path: "/projects/{projectID}/attachments", Definitions: []definition.Definition{{Method: definition.Create, Function: s.uploadAttachment, Parameters: []definition.Parameter{{Source: definition.Path, Name: "projectID"}, definition.FormParameterFor("label", "Attachment label"), definition.FileParameterFor("file", "Attachment file")}, Results: definition.DataErrorResults("Attachment"), Consumes: []string{definition.MIMEFormData}, Produces: []string{definition.MIMEJSON}, ErrorProduces: []string{definition.MIMEJSON}}}},
	}}
}

func definitionsAt(ops []legacyOperation, path string) []definition.Definition {
	var out []definition.Definition
	for _, op := range ops {
		if op.path == path {
			out = append(out, op.d)
		}
	}
	return out
}

type nativeID struct {
	ID string `path:"id"`
}
type nativeOut struct {
	Body map[string]any `json:"body"`
}

func registerNative(api huma.API, s *store) error {
	group := huma.NewGroup(api, "/native")
	group.UseSimpleModifier(func(op *huma.Operation) { op.Tags = []string{"native-enhancement"} })
	group.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		s.startHumaTrace("native.before")
		next(ctx)
		s.recordHuma("native.after")
	})
	huma.Register[struct{}, nativeOut](group, huma.Operation{OperationID: "native-health", Method: http.MethodGet, Path: "/health"}, func(context.Context, *struct{}) (*nativeOut, error) {
		s.recordHuma("handler")
		s.mu.Lock()
		s.handlerCalls++
		s.mu.Unlock()
		return &nativeOut{Body: map[string]any{"status": "ok", "contract": "native"}}, nil
	})
	huma.Register[struct{}, nativeOut](group, huma.Operation{OperationID: "native-version", Method: http.MethodGet, Path: "/version"}, func(context.Context, *struct{}) (*nativeOut, error) {
		s.recordHuma("handler")
		return &nativeOut{Body: map[string]any{"name": "Nirvana Showcase", "version": "1.0.0", "contract": "native"}}, nil
	})
	huma.Register[nativeID, nativeOut](group, huma.Operation{OperationID: "native-async-status", Method: http.MethodGet, Path: "/async/{id}"}, func(_ context.Context, in *nativeID) (*nativeOut, error) {
		s.recordHuma("handler")
		return &nativeOut{Body: map[string]any{"id": in.ID, "state": "completed", "contract": "native"}}, nil
	})
	return nil
}

func nativeService(s *store) http.Handler {
	router := http.NewServeMux()
	api := humago.New(router, huma.DefaultConfig("Nirvana Showcase Native", "1.0.0"))
	if err := registerNative(api, s); err != nil {
		panic(err)
	}
	return router
}

func (a *App) Handler(arm string) (http.Handler, error) {
	var base http.Handler
	switch arm {
	case "original":
		base = a.Original
	case "build", "adapted":
		base = a.Build
	case "native", "composed":
		if arm == "native" {
			base = a.Native
		} else {
			base = a.Composed
		}
	default:
		return nil, fmt.Errorf("unknown arm %q", arm)
	}
	return base, nil
}
func (a *App) SideEffects(arm string) (taskWrites, messageWrites, calls int) {
	s := a.buildStore
	if arm == "original" {
		s = a.originalStore
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.taskWrites, s.messageWrites, s.handlerCalls
}
func (a *App) Trace(arm string) []string {
	s := a.buildStore
	if arm == "original" {
		s = a.originalStore
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.trace...)
}

// NativeOnly creates a standard-library Huma reference service.
func NativeOnly() http.Handler {
	return nativeService(newStore())
}

// Both application entry points retain the same outer request policy.
func demoRequestPolicy(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path == "/projects/1" && r.Header.Get("X-Demo-Policy") != "allow" {
		http.Error(w, "demo request policy denied", http.StatusForbidden)
		return false
	}
	return true
}
