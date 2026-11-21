package verbs

import (
	"context"
	"errors"
	"testing"
)

type modelKeyProbe struct {
	calls int
	fail  bool
}

func (p *modelKeyProbe) Run(context.Context, FleetProvider) error {
	p.calls++
	if p.fail {
		return errors.New("not reachable")
	}
	return nil
}

// deepseekRecord is a provider record up's services step can set up: a
// key and the endpoints its probe reads.
var deepseekRecord = FleetProvider{Name: "deepseek", API: "openai", Key: "DEEPSEEK_API_KEY", ModelsURL: "https://api.deepseek.com/models", BalanceURL: "https://api.deepseek.com/user/balance", Enabled: true}

// A record with no endpoint to probe is refused before any prompt.
func TestServiceKeyPromptOnceAndRechecksProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	terminal := &terminalFake{data: []byte("fixture")}
	sink := &sinkFake{}
	probe := &modelKeyProbe{}
	s := ServiceKey{Terminal: terminal, Sink: sink, Probe: probe}
	ok, err := s.Verify(ctx, deepseekRecord)
	if err != nil || ok || probe.calls != 0 {
		t.Fatal(ok, err)
	}
	if err = s.Act(ctx, deepseekRecord); err != nil {
		t.Fatal(err)
	}
	if err = s.Act(ctx, deepseekRecord); err != nil || terminal.asks != 1 {
		t.Fatal(err, terminal.asks)
	}
	ok, err = s.Verify(ctx, deepseekRecord)
	if err != nil || !ok || probe.calls != 1 {
		t.Fatal(ok, err, probe.calls)
	}
	probe.fail = true
	ok, err = s.Verify(ctx, deepseekRecord)
	if err == nil || ok {
		t.Fatal(ok, err)
	}
	if err = s.Act(ctx, FleetProvider{Name: "unknown", Key: "UNKNOWN_API_KEY"}); err == nil || terminal.asks != 1 {
		t.Fatal(err, terminal.asks)
	}
}
