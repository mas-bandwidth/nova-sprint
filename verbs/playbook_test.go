package verbs

import (
	"context"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/docs"
)

func TestColdHelpPlaybookMatchesHookText(t *testing.T) {
	worker, ok := docs.Playbook("worker")
	if !ok {
		t.Fatal("worker missing")
	}
	coordinator, ok := docs.Playbook("coordinator")
	if !ok || !strings.HasPrefix(coordinator, worker) {
		t.Fatal("coordinator must receive worker playbook first")
	}
	for _, role := range []string{"worker", "coordinator"} {
		text, _ := docs.Playbook(role)
		got := (&Engine{}).Execute(context.Background(), []string{"help", "playbook", role})
		if got.Exit != 0 || got.Output != text+"\nnext: nova-sprint\n" {
			t.Fatal(role, got.Exit)
		}
	}
	got := (&Engine{}).Execute(context.Background(), []string{"help", "playbook", "other"})
	if got.Exit != 1 || !strings.Contains(got.Output, "coordinator|worker") {
		t.Fatal(got)
	}
}
