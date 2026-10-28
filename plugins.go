package humanirvana

import (
	"errors"
	"fmt"
	"sync"

	"github.com/caicloud/nirvana"
	"github.com/caicloud/nirvana/service"
)

// InstallPlugins installs the named Nirvana plugins in order on an application-owned
// builder. All names must be registered, unique, and have non-nil config.Config(name)
// values. Validation completes before installation; unlisted configuration is ignored.
// The builder and config must be non-nil even for an empty list.
//
// cleanup is always non-nil. It uninstalls successful installations in reverse order,
// attempts every uninstall, and caches the joined errors. Concurrent or repeated calls
// wait for and return the same result, without retrying failed uninstalls.
// An install error triggers cleanup before returning; err includes both that error and
// all rollback errors. cleanup then returns only the cached rollback errors.
//
// A plugin whose Install fails is NOT uninstalled: Nirvana does not guarantee this is
// safe. That plugin or its caller owns partial-failure recovery. Uninstall need not undo
// builder mutations, so discard the builder and config after failure or cleanup.
// The caller must keep the config alive and must not mutate it until cleanup finishes,
// stop users of plugin resources before cleanup, and avoid managing these installations
// through another lifecycle. Register installers before calling this function. Plugin
// panics and incorrect plugin-specific configuration types are not recovered.
func InstallPlugins(builder service.Builder, config *nirvana.Config, names []string) (cleanup func() error, err error) {
	type plugin struct {
		name      string
		installer nirvana.ConfigInstaller
	}
	var installed []plugin
	var once sync.Once
	var cleanupErr error
	cleanup = func() error {
		once.Do(func() {
			var errs []error
			for i := len(installed) - 1; i >= 0; i-- {
				p := installed[i]
				if err := p.installer.Uninstall(builder, config); err != nil {
					errs = append(errs, fmt.Errorf("humanirvana: uninstall plugin %q: %w", p.name, err))
				}
			}
			cleanupErr = errors.Join(errs...)
		})
		return cleanupErr
	}
	if builder == nil || config == nil {
		return cleanup, fmt.Errorf("humanirvana: plugin builder and config must be non-nil")
	}
	installers := make([]nirvana.ConfigInstaller, len(names))
	seen := make(map[string]bool, len(names))
	for i, name := range names {
		if seen[name] {
			return cleanup, fmt.Errorf("humanirvana: duplicate plugin %q", name)
		}
		seen[name] = true
		installers[i] = nirvana.ConfigInstallerFor(name)
		if installers[i] == nil {
			return cleanup, fmt.Errorf("humanirvana: unknown plugin %q", name)
		}
		if config.Config(name) == nil {
			return cleanup, fmt.Errorf("humanirvana: missing configuration for plugin %q", name)
		}
	}
	for i, installer := range installers {
		if err := installer.Install(builder, config); err != nil {
			return cleanup, errors.Join(fmt.Errorf("humanirvana: install plugin %q: %w", names[i], err), cleanup())
		}
		installed = append(installed, plugin{names[i], installer})
	}
	return cleanup, nil
}
