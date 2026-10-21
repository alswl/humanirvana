// Package testkit builds native Huma fixtures on the application's Nirvana router.
package testkit

import (
	"fmt"
	"net/http"

	"github.com/alswl/humanirvana"
	"github.com/caicloud/nirvana/definition"
	"github.com/caicloud/nirvana/service/rest"
	"github.com/danielgtaylor/huma/v2"
)

type Registration struct{ Register func(huma.API) error }
type LegacyApplication struct{ Descriptors []definition.Descriptor }
type Spec struct {
	Config huma.Config
	Native []Registration
	Legacy *LegacyApplication
}
type Result struct{ Handler http.Handler }

// NewAPIHandler creates a standard Huma API backed by the same Nirvana builder
// used for legacy descriptors, then builds and binds the application service.
func NewAPIHandler(spec Spec) (*Result, error) {
	builder := rest.NewBuilder()
	adapter := humanirvana.NewAdapter(builder)
	api := huma.NewAPI(spec.Config, adapter)
	if spec.Legacy != nil {
		if err := humanirvana.RegisterLegacy(builder, spec.Legacy.Descriptors); err != nil {
			return nil, err
		}
	}
	for _, registration := range spec.Native {
		if registration.Register == nil {
			continue
		}
		if err := registration.Register(api); err != nil {
			return nil, err
		}
	}
	service, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("build Nirvana service: %w", err)
	}
	if err := adapter.Bind(service); err != nil {
		return nil, err
	}
	return &Result{Handler: adapter}, nil
}

// NewNativeAPIHandler builds a single native Huma registration on Nirvana.
func NewNativeAPIHandler(config huma.Config, register func(huma.API) error) (*Result, error) {
	return NewAPIHandler(Spec{Config: config, Native: []Registration{{Register: register}}})
}
