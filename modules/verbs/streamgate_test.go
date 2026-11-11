package verbs

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// A stream's gates (2026-11-10): commands every card of the stream passes
// at its finished head beside its own DONE-WHENs, declared on the stream so
// no brief has to remember them. stream add and stream set take --gate
// (repeatable, the whole list; --gate none clears it), each one command as
// a DONE-WHEN is; show, check and take carry them.

const genGate = "make generated-current"

func withGates(gates ...string) func(*store.Snapshot) {
	return func(s *store.Snapshot) {
		st := s.Streams["s"]
		st.Gates = gates
		s.Streams["s"] = st
	}
}

func TestStreamGatesSetAndCleared(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	want(t, w.do("carol", "stream", "add", "t", "--repo", "o/r", "--gate", genGate, "--gate", "sh -c 'go vet ./... | tee vet.log'"), 0,
		"STREAM t repo=o/r base=main mode=auto forge=none applied at seq", "GATE "+genGate+"\nGATE sh -c 'go vet ./... | tee vet.log'\n")
	if got := w.snap(t).Streams["t"].Gates; !slices.Equal(got, []string{genGate, "sh -c 'go vet ./... | tee vet.log'"}) {
		t.Fatalf("gates %q", got)
	}
	want(t, w.do("carol", "stream", "set", "t", "--gate", "make check"), 0, "STREAM t mode=auto forge=none applied at seq", "GATE make check\n")
	want(t, w.do("carol", "stream", "set", "t", "--gate", "make check"), 0, "STREAM t mode=auto forge=none unchanged", "GATE make check\n")
	want(t, w.do("carol", "show", "t"), 0, "\"gates\": [\n    \"make check\"\n  ]")
	want(t, w.do("carol", "stream", "set", "t", "--mode", "hand"), 0, "STREAM t mode=hand forge=none applied", "GATE make check\n")
	want(t, w.do("carol", "stream", "set", "t", "--gate", "none"), 0, "STREAM t mode=hand forge=none applied", "GATES none\n")
	if got := w.snap(t).Streams["t"]; got.Gates != nil || got.Mode != "hand" {
		t.Fatalf("stream %+v, want no gates, mode kept", got)
	}
	if out := w.do("carol", "show", "t").Output; strings.Contains(out, "gates") {
		t.Fatalf("show of a stream with no gates names gates:\n%s", out)
	}
}

// A gate that is not one command is refused at stream add and stream set,
// in the words add refuses a bad DONE-WHEN with, and nothing is written.
func TestStreamGateRefusals(t *testing.T) {
	t.Parallel()
	runCases(t, []tcase{
		{name: "add a pipe", as: "carol", args: []string{"stream", "add", "t", "--repo", "o/r", "--gate", "make gen | tee out"}, exit: 1,
			want: []string{"stream t: gate make gen | tee out is not one command", "sh -c '...'", "nova-sprint stream add t --gate <command> again"}},
		{name: "set a sentence", as: "carol", args: []string{"stream", "set", "s", "--gate", "Verify the generated tree is current"}, exit: 1,
			want: []string{"stream s: gate Verify the generated tree is current is not one command", "nova-sprint stream set s --gate <command> again"}},
		{name: "none with another", as: "carol", args: []string{"stream", "set", "s", "--gate", "none", "--gate", genGate}, exit: 2,
			want: []string{"--gate none clears the gates and is given alone"}},
		{name: "set of nothing", as: "carol", args: []string{"stream", "set", "s"}, exit: 2, want: []string{"stream set changes --mode, --forge, --gate"}},
	})
	w := newWorld(t)
	w.do("carol", "stream", "set", "s", "--gate", "make gen | tee out")
	if got := w.snap(t).Streams["s"].Gates; got != nil {
		t.Fatalf("a refused gate was written: %q", got)
	}
}

// check prints each gate of each stream, and a stored gate that does not
// parse (a restore, an older build) is red naming the fix.
func TestCheckPrintsTheStreamGates(t *testing.T) {
	t.Parallel()
	s := sprint()
	withGates(genGate)(&s)
	w := worldFrom(t, s)
	healthy(t, w)
	want(t, w.do("", "check"), 0, "CHECK stream s: o/r reachable, base main at bbbbbbbbbbbb\nCHECK stream s gate: "+genGate+" (every card of the stream passes it at its finished head)\n")
	s = sprint()
	withGates("make gen | tee out")(&s)
	w = worldFrom(t, s)
	healthy(t, w)
	want(t, w.do("", "check"), 1, "CHECK RED stream s: gate make gen | tee out is not one command", "nova-sprint stream set s --gate <command> again")
}

// take's receipt carries the stream's gates for the bench worker, and its
// text gives a friend row each as a STREAM GATE line and the rule for one
// red at the base too; a stream with none adds nothing.
func TestTakeCarriesTheStreamGates(t *testing.T) {
	t.Parallel()
	s := sprint()
	withGates(genGate)(&s)
	w := worldFrom(t, s)
	var got takeReceipt
	if err := json.Unmarshal([]byte(w.ok(t, "alice", "take", "p1.w1@1", "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.StreamGates, []string{genGate}) {
		t.Fatalf("receipt stream_gates %q", got.StreamGates)
	}
	want(t, w.do("alice", "take", "p1.w1@1"), 0, "\nSTREAM GATE: "+genGate+"\n", "Run every stream gate", "--reason \"stream gate <command> is red at base")
	want(t, w.do("bob", "take", "p2.r1@1"), 0, "\nSTREAM GATE: "+genGate+"\n", "Name such a gate in the yes's --finding")
	plain := newWorld(t)
	if err := json.Unmarshal([]byte(plain.ok(t, "alice", "take", "p1.w1@1", "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if got.StreamGates == nil || len(got.StreamGates) != 0 {
		t.Fatalf("receipt stream_gates %#v, want [] for a stream with none", got.StreamGates)
	}
	if out := plain.do("alice", "take", "p1.w1@1").Output; strings.Contains(out, "STREAM GATE") || strings.Contains(out, "stream gate") {
		t.Fatalf("take of a stream with no gates names them:\n%s", out)
	}
}
