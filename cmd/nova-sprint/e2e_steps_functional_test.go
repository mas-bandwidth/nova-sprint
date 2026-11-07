package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// coordinatorHooks puts carol on the bus, then hooks her in as coordinator.
func (x *e2e) coordinatorHooks() {
	x.tr.note("the coordinator hooks in: refused off the bus; on the bus first, then the sprint")
	if code, out := x.c.run(false, nil, "hook", "--as", "carol", "--role", "coordinator"); code != 1 || !strings.Contains(out, "SPRINT REFUSED hook: carol is not hooked in to the bus; hook in first: nova-bus hook --as carol") {
		x.t.Fatalf("a hook off the bus: exit %d\n%s", code, out)
	}
	busHook(x.t, x.tr, x.busEnv, "carol", x.inbox("carol"))
	x.c.must([]string{"hook", "--as", "carol", "--role", "coordinator"}, "HOOKED carol role=coordinator")
	x.tr.note("a fresh store is stopped, never started; the coordinator starts it")
	x.c.must([]string{"show", "--as", "carol"}, `"StopReason": "never started"`)
	x.c.must([]string{"start", "--reason", "e2e", "--as", "carol"}, "STARTED applied at seq")
	x.tr.note("the coordinator sets the commit identity every worker commits as (the e2e's git identity)")
	x.c.must([]string{"identity", "--name", "e2e", "--email", "e2e@sprint.test", "--reason", "e2e", "--as", "carol"}, "IDENTITY e2e <e2e@sprint.test> applied at seq")
}

// streamAndCard adds the stream (the local origin, --forge github) and one
// card that passes the brief lint.
func (x *e2e) streamAndCard() {
	x.tr.note("the stream: the local origin, landed through the forge")
	x.c.must([]string{"stream", "add", "s", "--repo", x.origin, "--forge", "github", "--as", "carol"}, "STREAM s", "forge=github")
	brief := filepath.Join(x.dir, "e2e1.md")
	text := "tier: pro\nPATHS: e2e.txt\nTEST: test -f e2e.txt\nDONE-WHEN: test -f e2e.txt\n\nWrite e2e.txt containing the word done.\n"
	if err := os.WriteFile(brief, []byte(text), 0o644); err != nil {
		x.t.Fatal(err)
	}
	x.tr.note("one card, e2e1, from a brief that passes the lint")
	x.c.must([]string{"add", "--stream", "s", "--brief-file", brief, "--as", "carol"}, "ADD e2e1 kind=producer", "ADDED 1 cards")
}

// dealt waits for row's first ready consumer of kind and returns its
// id and gen, recording the next that found it.
func (x *e2e) dealt(row, kind string) (string, int) {
	var id string
	var gen, polls int
	defer func() {
		if id == "" {
			x.c.run(false, nil, "next", "--as", row, "--json")
			x.c.run(false, nil, "show", "e2e1", "--as", "carol")
			snap := x.snapshot()
			x.tr.record("the store's rows and cards when no deal came", 0, fmt.Sprintf("seq=%d stopped=%v\nrows: %+v\ncards: %+v", snap.Seq, snap.Stopped, snap.Rows, snap.Cards))
			x.tr.record("the store's judgements", 0, fmt.Sprintf("%+v", snap.Judgements))
			if evs, err := x.st.Events(context.Background(), 0, 200); err == nil {
				var lines []string
				for _, ev := range evs {
					lines = append(lines, fmt.Sprintf("%d %s %s accepted=%v reason=%q op=%s", ev.Seq, ev.Actor, ev.Kind, ev.Accepted, ev.Reason, ev.Op))
				}
				x.tr.record("the store's events", 0, strings.Join(lines, "\n"))
			}
		}
	}()
	eventually(x.t, 90*time.Second, kind+" dealt to "+row, func() bool {
		polls++
		code, out := x.c.run(true, nil, "next", "--as", row, "--json")
		type dealtCard struct {
			ID, Kind string
			Gen      int
		}
		var v struct {
			Ready   []dealtCard `json:"ready"`
			Working []dealtCard `json:"working"` // the deal puts it in the row's hand, working (defect 52)
		}
		if code != 0 || json.Unmarshal([]byte(out), &v) != nil {
			return false
		}
		for _, c := range append(v.Ready, v.Working...) {
			if c.Kind == kind {
				id, gen = c.ID, c.Gen
				return true
			}
		}
		return false
	})
	x.tr.note("a tick dealt %s %s@%d to %s (next polled %d times)", kind, id, gen, row, polls)
	x.c.must([]string{"next", "--as", row}, "nova-sprint take "+fmt.Sprintf("%s@%d", id, gen))
	return id, gen
}

// workerTakes hooks kim (on the bus first), waits for the deal, and takes
// it; the receipt carries model, deadline and forge.
func (x *e2e) workerTakes() (string, int, int) {
	x.tr.note("a worker row hooks in: kim, a friend in nova-config")
	busHook(x.t, x.tr, x.busEnv, "kim", x.inbox("kim"))
	x.c.must([]string{"hook", "--as", "kim", "--role", "worker"}, "HOOKED kim role=worker")
	id, gen := x.dealt("kim", "work")
	out := x.c.must([]string{"take", fmt.Sprintf("%s@%d", id, gen), "--as", "kim", "--json"})
	var r struct {
		Consumer, Branch, Model, Forge string
		BaseSHA                        string `json:"base_sha"`
		Deadline, Gen, Epoch           int
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		x.t.Fatalf("take receipt: %v\n%s", err, out)
	}
	if r.Model != "m-kim" || r.Deadline <= 0 || r.Forge != "github" || r.Branch != "sprint/s/e2e1" {
		x.t.Fatalf("the take receipt lacks model, deadline or forge: %+v", r)
	}
	x.tr.note("receipt: model=%s deadline=%ds forge=%s branch=%s", r.Model, r.Deadline, r.Forge, r.Branch)
	x.work = r.BaseSHA
	return r.Consumer, r.Gen, r.Epoch
}

// workerFinishes does the work on the dealt branch, pushes it, and
// finishes at the pushed head.
func (x *e2e) workerFinishes(id string, gen, epoch int) string {
	t, env := x.t, x.busEnv
	x.tr.note("kim does the work: a commit on sprint/s/e2e1 from the dealt base, pushed to origin")
	clone := filepath.Join(x.dir, "kim")
	sh(t, x.tr, env, "git", "clone", "-q", x.origin, clone)
	sh(t, x.tr, env, "git", "-C", clone, "checkout", "-q", "-b", "sprint/s/e2e1", x.work)
	if err := os.WriteFile(filepath.Join(clone, "e2e.txt"), []byte("done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sh(t, x.tr, env, "git", "-C", clone, "add", "e2e.txt")
	sh(t, x.tr, env, "git", "-C", clone, "commit", "-q", "-m", "e2e1: write e2e.txt")
	sh(t, x.tr, env, "git", "-C", clone, "push", "-q", "origin", "sprint/s/e2e1")
	head := sh(t, x.tr, env, "git", "-C", clone, "rev-parse", "HEAD")
	x.c.must([]string{"finish", fmt.Sprintf("%s@%d", id, gen), "--head", head, "--as", "kim", "--epoch", fmt.Sprint(epoch)}, "FINISHED "+id, "head="+head)
	return head
}

// readerReads hooks bench1, waits for the read, takes it and answers yes.
func (x *e2e) readerReads(head string) {
	x.tr.note("a second row hooks in: bench1, a bench in nova-config; the read is asked")
	busHook(x.t, x.tr, x.busEnv, "bench1", x.inbox("bench1"))
	x.c.must([]string{"hook", "--as", "bench1", "--role", "worker"}, "HOOKED bench1 role=worker")
	id, gen := x.dealt("bench1", "read")
	out := x.c.must([]string{"take", fmt.Sprintf("%s@%d", id, gen), "--as", "bench1", "--json"}, `"head":"`+head+`"`)
	gen2, epoch := field(x.t, out, "gen"), field(x.t, out, "epoch")
	x.c.must([]string{"read", id + "@" + gen2, "yes", "--as", "bench1", "--epoch", epoch}, "READ "+id, "result=ok")
}

// lands waits for the landing: the forge gated and merged it, the
// producer is landed, and origin's main holds the head.
func (x *e2e) lands(head string) {
	x.tr.note("the landing request goes out to the git edge and the forge")
	eventually(x.t, 120*time.Second, "e2e1 landed", func() bool { return x.snapshot().Cards["e2e1"].State == "landed" })
	calls := x.fg.Calls()
	x.tr.record("forge calls (fake forge, run side)", 0, strings.Join(calls, "\n"))
	joined := strings.Join(calls, "\n")
	// rung 1: one head on the base tip opens its PR from its own card branch
	for _, want := range []string{"OpenPR sprint/s/e2e1 -> main", "Checks ", "Merge #"} {
		if !strings.Contains(joined, want) {
			x.t.Fatalf("the forge never saw %q:\n%s", want, joined)
		}
	}
	x.tr.note("the landing result came back landed")
	x.c.must([]string{"show", "e2e1", "--as", "carol"}, `"state": "landed"`)
	cmd := exec.Command("git", "--git-dir", x.origin, "merge-base", "--is-ancestor", head, "refs/heads/main")
	err := cmd.Run()
	x.tr.record("git --git-dir origin.git merge-base --is-ancestor <head> refs/heads/main", exitOf(err), "")
	if err != nil {
		x.t.Fatalf("origin's main does not hold the head: %v", err)
	}
}

func exitOf(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}

// boardShowsLanded reads /api/sprint until it counts the landing: the
// board counts producers per stream (no card ids), so landed=1 of all=1
// and stream s's merge cell merged=1 are e2e1.
func (x *e2e) boardShowsLanded() {
	var raw string
	var merged any
	defer func() {
		if x.t.Failed() {
			x.tr.record("GET http://<board>/api/sprint (the last body)", 0, raw)
		}
	}()
	eventually(x.t, 30*time.Second, "the board showing e2e1 landed", func() bool {
		raw = x.boardJSON()
		landed, ok := boardField(raw, "landed")
		all, _ := boardField(raw, "all")
		tables, _ := boardField(raw, "tables")
		if m, ok := tables.(map[string]any); ok {
			if mg, ok := m["merge"].(map[string]any); ok {
				if s, ok := mg["s"].(map[string]any); ok {
					merged = s["merged"]
				}
			}
		}
		return ok && fmtAny(landed) == "1" && fmtAny(all) == "1" && fmtAny(merged) == "1"
	})
	all, _ := boardField(raw, "all")
	machine, _ := boardField(raw, "machine")
	x.tr.record("GET http://<board>/api/sprint", 0, fmt.Sprintf("landed=1 all=%v merge.s.merged=%v machine=%q", all, merged, machine))
}

// stopStopsTheTick stops the sprint: no tick op lands after the stop, and
// the board's machine line says STOPPED.
func (x *e2e) stopStopsTheTick() {
	x.tr.note("stop: the tick stops")
	x.c.must([]string{"stop", "--reason", "e2e done", "--as", "carol"}, "STOPPED applied at seq")
	at := x.snapshot().Seq
	time.Sleep(3500 * time.Millisecond) // three ticks or more
	evs, err := x.st.Events(context.Background(), at, 1000)
	if err != nil {
		x.t.Fatal(err)
	}
	var after []string
	for _, ev := range evs {
		if ev.Accepted && ev.Kind != "push-cursor" {
			after = append(after, fmt.Sprintf("%d %s %s", ev.Seq, ev.Actor, ev.Kind))
		}
	}
	x.tr.record(fmt.Sprintf("store events after the stop's seq %d, other than push cursors", at), len(after), strings.Join(after, "\n"))
	if len(after) > 0 {
		x.t.Fatalf("ops after the stop: %v", after)
	}
	var machine any
	eventually(x.t, 10*time.Second, "the board's machine line saying STOPPED", func() bool {
		machine, _ = boardField(x.boardJSON(), "machine")
		return strings.Contains(fmtAny(machine), "STOPPED")
	})
	x.tr.record("GET http://<board>/api/sprint", 0, fmt.Sprintf("machine=%q", machine))
}
