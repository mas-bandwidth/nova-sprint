package verbs

import (
	"context"
	"errors"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/policy"
)

type modelKeyProbe struct {
	calls int
	fail  bool
	got   policy.Provider
}

func (p *modelKeyProbe) Run(_ context.Context, provider policy.Provider) error {
	p.calls++
	p.got = provider
	if p.fail {
		return errors.New("not reachable")
	}
	return nil
}

// A provider key is asked for once, saved under the configured name, and
// probed with the configured provider.
func TestServiceKeyPromptOnceAndRechecksProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	terminal := &terminalFake{data: []byte("fixture")}
	sink := &sinkFake{}
	probe := &modelKeyProbe{}
	s := ServiceKey{Terminal: terminal, Sink: sink, Probe: probe}
	ok, err := s.Verify(ctx, "acme", acmeProvider)
	if err != nil || ok || probe.calls != 0 {
		t.Fatal(ok, err)
	}
	if err = s.Act(ctx, "acme", acmeProvider); err != nil {
		t.Fatal(err)
	}
	if err = s.Act(ctx, "acme", acmeProvider); err != nil || terminal.asks != 1 || sink.name != "ACME_API_KEY" {
		t.Fatal(err, terminal.asks, sink.name)
	}
	ok, err = s.Verify(ctx, "acme", acmeProvider)
	if err != nil || !ok || probe.calls != 1 || probe.got != acmeProvider {
		t.Fatal(ok, err, probe.calls, probe.got)
	}
	probe.fail = true
	ok, err = s.Verify(ctx, "acme", acmeProvider)
	if err == nil || ok {
		t.Fatal(ok, err)
	}
}
