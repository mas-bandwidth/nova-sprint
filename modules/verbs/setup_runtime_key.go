package verbs

import (
	"context"
	"fmt"
)

// ServiceKey verifies an existing saved key by a model-list request. It is only
// one part of setup step 8 (services); grants and rows must also verify before that step passes.
// A service is a nova-config provider record: its key is the secret it
// names, probed at its balance_url and models_url.
type ServiceKey struct {
	Terminal SecretTerminal
	Sink     SecretSink
	Probe    interface {
		Run(context.Context, FleetProvider) error
	}
}

func (s ServiceKey) Verify(ctx context.Context, p FleetProvider) (bool, error) {
	if _, err := probeEndpoints(p); err != nil {
		return false, err
	}
	if s.Sink == nil || s.Probe == nil {
		return false, fmt.Errorf("service key verification unavailable")
	}
	present, err := s.Sink.Has(ctx, p.Key)
	if err != nil || !present {
		return false, err
	}
	if err = s.Probe.Run(ctx, p); err != nil {
		return false, RefusalOr(err, &OperationError{Problem: "service " + p.Name + " did not pass its authenticated model probe (" + errText(err) + ")", Fix: "check the service key and network access", Exit: 2})
	}
	return true, nil
}
func (s ServiceKey) Act(ctx context.Context, p FleetProvider) error {
	if _, err := probeEndpoints(p); err != nil {
		return err
	}
	return EnsureKey(ctx, p.Name, p.Key, s.Terminal, s.Sink)
}
