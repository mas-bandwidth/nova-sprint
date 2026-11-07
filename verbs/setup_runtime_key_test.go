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

func (p *modelKeyProbe) Run(context.Context, string) error {
	p.calls++
	if p.fail {
		return errors.New("not reachable")
	}
	return nil
}
func TestServiceKeyPromptOnceAndRechecksProvider(t *testing.T) {
	ctx := context.Background()
	terminal := &terminalFake{data: []byte("fixture")}
	sink := &sinkFake{}
	probe := &modelKeyProbe{}
	s := ServiceKey{Terminal: terminal, Sink: sink, Probe: probe}
	ok, err := s.Verify(ctx, "deepseek")
	if err != nil || ok || probe.calls != 0 {
		t.Fatal(ok, err)
	}
	if err = s.Act(ctx, "deepseek"); err != nil {
		t.Fatal(err)
	}
	if err = s.Act(ctx, "deepseek"); err != nil || terminal.asks != 1 {
		t.Fatal(err, terminal.asks)
	}
	ok, err = s.Verify(ctx, "deepseek")
	if err != nil || !ok || probe.calls != 1 {
		t.Fatal(ok, err, probe.calls)
	}
	probe.fail = true
	ok, err = s.Verify(ctx, "deepseek")
	if err == nil || ok {
		t.Fatal(ok, err)
	}
	if err = s.Act(ctx, "unknown"); err == nil || terminal.asks != 1 {
		t.Fatal(err, terminal.asks)
	}
}
