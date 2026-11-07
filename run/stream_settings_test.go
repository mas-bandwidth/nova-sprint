package run

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestStreamSettingsColdHistoryAndIncrementalFold(t *testing.T) {
	ctx := context.Background()
	s := store.NewFake()
	applyOK(t, s, op("stream", store.PutStream{Name: "s"}, store.PutPolicy{Kind: "data", Name: "stream:s", Body: `{"repo":"one","base":"main","pr":true,"mode":"auto"}`}))
	steps := &tickSteps{}
	e, err := NewEngine(ctx, s, &tickGuard{}, steps, &tickEdges{})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(ctx, nil); err != nil {
		t.Fatal(err)
	}
	first := steps.inputs[0].Streams["s"]
	if first.Repo != "one" || !first.PR || first.Mode != "auto" {
		t.Fatalf("initial: %+v", first)
	}
	steps.inputs[0].Streams["s"] = StreamSettings{Repo: "mutation"}
	applyOK(t, s, op("update", store.PutPolicy{Kind: "data", Name: "stream:s", Body: `{"repo":"two","base":"next","pr":false,"mode":"manual"}`}))
	if err = e.Tick(ctx, nil); err != nil {
		t.Fatal(err)
	}
	second := steps.inputs[1].Streams["s"]
	if second.Repo != "two" || second.PR || second.Mode != "manual" {
		t.Fatalf("updated: %+v", second)
	}
}
func TestStreamSettingsMalformedFailsClosed(t *testing.T) {
	ctx := context.Background()
	s := store.NewFake()
	applyOK(t, s, op("stream", store.PutStream{Name: "s"}, store.PutPolicy{Kind: "data", Name: "stream:s", Body: `{bad`}))
	steps := &tickSteps{}
	e, err := NewEngine(ctx, s, &tickGuard{}, steps, &tickEdges{})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(ctx, nil); err == nil || len(steps.inputs) != 0 {
		t.Fatalf("err=%v calls=%d", err, len(steps.inputs))
	}
}
