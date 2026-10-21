package humanirvana

import (
	"fmt"
	"github.com/caicloud/nirvana/definition"
	"github.com/caicloud/nirvana/service"
)

// RegisterLegacy adds existing descriptors directly to the application's
// Nirvana builder. Their handler, middleware, binding, and response behavior
// remain on Nirvana's original execution path.
func RegisterLegacy(builder service.Builder, descriptors []definition.Descriptor) error {
	if builder == nil {
		return fmt.Errorf("runtime: nil Nirvana builder")
	}
	for _, descriptor := range descriptors {
		if err := builder.AddDescriptor(descriptor); err != nil {
			return fmt.Errorf("runtime: register legacy descriptor %q: %w", descriptor.Path, err)
		}
	}
	return nil
}
