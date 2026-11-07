package verbs

import (
	"context"
	"fmt"

	"github.com/mas-bandwidth/nova-sprint/policy"
)

// ServiceKey verifies a provider's saved key (policy.Provider Key) by a GET
// of its probe endpoint. It is only one part of setup step 6; grants and
// rows must also verify before that step passes.
type ServiceKey struct {
	Terminal SecretTerminal
	Sink     SecretSink
	Probe    interface {
		Run(context.Context, policy.Provider) error
	}
}

func (s ServiceKey) Verify(ctx context.Context, service string, p policy.Provider) (bool, error) {
	if s.Sink == nil || s.Probe == nil {
		return false, fmt.Errorf("service key verification unavailable")
	}
	present, err := s.Sink.Has(ctx, p.Key)
	if err != nil || !present {
		return false, err
	}
	if err = s.Probe.Run(ctx, p); err != nil {
		return false, RefusalOr(err, &OperationError{Problem: "service " + service + " did not pass its authenticated model probe (" + errText(err) + ")", Fix: "check the service key, its probe in the policy's catalog() \"providers\", and network access", Exit: 2})
	}
	return true, nil
}

// Act asks for the provider's key once, at a hidden prompt, when it is not
// saved under its configured name (EnsureKey).
func (s ServiceKey) Act(ctx context.Context, service string, p policy.Provider) error {
	return EnsureKey(ctx, service, p.Key, s.Terminal, s.Sink)
}
