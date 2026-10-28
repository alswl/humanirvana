package humanirvana_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/alswl/humanirvana"
	"github.com/caicloud/nirvana"
	"github.com/caicloud/nirvana/plugins/healthcheck"
	"github.com/caicloud/nirvana/service"
	"github.com/caicloud/nirvana/service/rest"
)

type pluginError struct{ message string }

func (e *pluginError) Error() string { return e.message }

type testInstaller struct {
	name                     string
	events                   *[]string
	builder                  service.Builder
	config                   *nirvana.Config
	installErr, uninstallErr error
}

func (p *testInstaller) Name() string { return p.name }
func (p *testInstaller) Install(b service.Builder, c *nirvana.Config) error {
	if b != p.builder || c != p.config {
		panic("wrong builder or config")
	}
	*p.events = append(*p.events, "install "+p.name)
	return p.installErr
}
func (p *testInstaller) Uninstall(b service.Builder, c *nirvana.Config) error {
	if b != p.builder || c != p.config {
		panic("wrong builder or config")
	}
	*p.events = append(*p.events, "uninstall "+p.name)
	return p.uninstallErr
}

// Nirvana's registry is process-global and has no unregister operation.
var pluginSequence int

func plugins(t *testing.T, events *[]string) (service.Builder, *nirvana.Config, []string, []*testInstaller) {
	t.Helper()
	b, c := rest.NewBuilder(), nirvana.NewConfig()
	var names []string
	var ps []*testInstaller
	for i := 0; i < 3; i++ {
		pluginSequence++
		name := fmt.Sprintf("humanirvana-test-%d", pluginSequence)
		p := &testInstaller{name: name, events: events, builder: b, config: c}
		nirvana.RegisterConfigInstaller(p)
		c.Set(name, struct{}{})
		names = append(names, name)
		ps = append(ps, p)
	}
	return b, c, names, ps
}
func TestPluginLifecycle(t *testing.T) {
	for _, fail := range []int{-1, 0, 2} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			var events []string
			b, c, names, ps := plugins(t, &events)
			installErr := &pluginError{"install failed"}
			if fail >= 0 {
				ps[fail].installErr = installErr
			}
			cleanup, err := humanirvana.InstallPlugins(b, c, names)
			if (err != nil) != (fail >= 0) {
				t.Fatalf("unexpected install result: %v", err)
			}
			if fail >= 0 {
				var target *pluginError
				if !errors.Is(err, installErr) || !errors.As(err, &target) || target != installErr {
					t.Fatalf("lost install error: %v", err)
				}
			}
			var want []string
			count := len(ps)
			if fail >= 0 {
				count = fail + 1
			}
			for i := 0; i < count; i++ {
				want = append(want, "install "+names[i])
			}
			if fail >= 0 {
				for i := fail - 1; i >= 0; i-- {
					want = append(want, "uninstall "+names[i])
				}
			}
			if !reflect.DeepEqual(events, want) {
				t.Fatalf("before cleanup: %v, want %v", events, want)
			}
			var wg sync.WaitGroup
			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := cleanup(); err != nil {
						t.Errorf("cleanup: %v", err)
					}
				}()
			}
			wg.Wait()
			if fail < 0 {
				for i := len(ps) - 1; i >= 0; i-- {
					want = append(want, "uninstall "+names[i])
				}
			}
			if !reflect.DeepEqual(events, want) {
				t.Fatalf("events: %v, want %v", events, want)
			}
		})
	}
}
func TestPluginCleanupErrors(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprint(rollback), func(t *testing.T) {
			var events []string
			b, c, names, ps := plugins(t, &events)
			first, second := errors.New("first cleanup"), &pluginError{"second cleanup"}
			ps[0].uninstallErr, ps[1].uninstallErr = first, second
			installErr := errors.New("install")
			if rollback {
				ps[2].installErr = installErr
			}
			cleanup, err := humanirvana.InstallPlugins(b, c, names)
			if rollback && !errors.Is(err, installErr) {
				t.Fatalf("lost install error: %v", err)
			}
			if !rollback && err != nil {
				t.Fatal(err)
			}
			cleanupErr := cleanup()
			if !rollback {
				err = cleanupErr
			}
			for _, e := range []error{first, second} {
				if !errors.Is(err, e) || !errors.Is(cleanupErr, e) {
					t.Fatalf("lost cleanup error: %v / %v", err, cleanupErr)
				}
			}
			var target *pluginError
			if !errors.As(err, &target) || target != second {
				t.Fatalf("lost typed cleanup error: %v", err)
			}
			before := len(events)
			if cleanup() != cleanupErr || len(events) != before {
				t.Fatal("cleanup not cached")
			}
			if events[len(events)-2] != "uninstall "+names[1] || events[len(events)-1] != "uninstall "+names[0] {
				t.Fatalf("cleanup stopped early: %v", events)
			}
		})
	}
}
func TestPluginValidation(t *testing.T) {
	for _, test := range []string{"missing", "unknown", "duplicate", "nil builder", "nil config", "empty", "extra config"} {
		t.Run(test, func(t *testing.T) {
			var events []string
			b, c, names, _ := plugins(t, &events)
			want := ""
			switch test {
			case "missing":
				c.Set(names[1], nil)
				want = "missing configuration"
			case "unknown":
				names[1] = "unregistered-plugin"
				want = "unknown plugin"
			case "duplicate":
				names[2] = names[0]
				want = "duplicate plugin"
			case "nil builder":
				b = nil
				want = "non-nil"
			case "nil config":
				c = nil
				want = "non-nil"
			case "empty":
				names = nil
			case "extra config":
				c.Set("unregistered-extra", true)
				names = nil
			}
			cleanup, err := humanirvana.InstallPlugins(b, c, names)
			if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
				t.Fatalf("unexpected error: %v", err)
			}
			if cleanup == nil {
				t.Fatal("nil cleanup")
			}
			if err := cleanup(); err != nil {
				t.Fatal(err)
			}
			if len(events) != 0 {
				t.Fatalf("unexpected side effects: %v", events)
			}
		})
	}
}

func TestNirvanaPluginWithAdapter(t *testing.T) {
	builder := rest.NewBuilder()
	builder.SetModifier(service.FirstContextParameter())
	config := nirvana.NewConfig().Configure(healthcheck.Path("/healthz"))
	cleanup, err := humanirvana.InstallPlugins(builder, config, []string{healthcheck.ExternalConfigName})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cleanup(); err != nil {
			t.Error(err)
		}
	})
	adapter := humanirvana.NewAdapter(builder)
	handler, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Bind(handler); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	adapter.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("healthcheck: %d %s", response.Code, response.Body.String())
	}
}
