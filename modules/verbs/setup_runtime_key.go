package verbs

import (
	"context"
	"fmt"
)

// ServiceKey verifies an existing saved key by a model-list request. It is only
// one part of setup step 8 (services); grants and rows must also verify before that step passes.
type ServiceKey struct {
	Terminal SecretTerminal
	Sink     SecretSink
	Probe    interface {
		Run(context.Context, string) error
	}
}

func (s ServiceKey) Verify(ctx context.Context, service string) (bool, error) {
	if _, ok := modelProbeProviders[service]; !ok {
		return false, fmt.Errorf("model-list probe is unsupported for service %q", service)
	}
	if s.Sink == nil || s.Probe == nil {
		return false, fmt.Errorf("service key verification unavailable")
	}
	name, err := secretName(service)
	if err != nil {
		return false, err
	}
	present, err := s.Sink.Has(ctx, name)
	if err != nil || !present {
		return false, err
	}
	if err = s.Probe.Run(ctx, service); err != nil {
		return false, RefusalOr(err, &OperationError{Problem: "service " + service + " did not pass its authenticated model probe (" + errText(err) + ")", Fix: "check the service key and network access", Exit: 2})
	}
	return true, nil
}
func (s ServiceKey) Act(ctx context.Context, service string) error {
	if _, ok := modelProbeProviders[service]; !ok {
		return fmt.Errorf("model-list probe is unsupported for service %q", service)
	}
	return EnsureKey(ctx, service, s.Terminal, s.Sink)
}
