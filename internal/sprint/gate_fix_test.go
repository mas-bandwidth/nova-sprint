package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A gate card G and a fixing card F that needs G cannot both be done when their
// DONE-WHEN runs the same command: G is the artifact F makes green, and F waits
// for G (docs/SPEC-SPRINT.md section 2; tla/WaitFor.tla, the gate-artifact edge
// and NoWaitCycle). The add refuses the pair, names both ids, the command key and
// the split, and writes nothing.
func TestGateFixCycleRefusedAtAdd(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.s.Now = time.Unix(1000, 0).UTC()
	gate := "RESULT: g sha=0123456789ab tier: pro\nDONE-WHEN: `make tables-x-fuzz-all`\n\nTHE TASK. Gate the tables.\n"
	fix := "RESULT: f sha=0123456789ab tier: pro\nDEPENDS-ON: g\nDONE-WHEN: `make ELIXIR=elixir tables-x-fuzz-all`\n\nTHE TASK. Fix the tables.\n"
	p := Add(w.s, AddReq{Stream: "s", Cards: []CardAdd{{ID: "g", Brief: gate}, {ID: "f", Brief: fix, Needs: []string{"g"}}}, Who: "coordinator"})
	require.NotEmpty(t, p.Refused, "the gate-fix cycle is refused")
	why := ""
	for _, r := range p.Refused {
		why += r.Why + "\n"
	}
	require.Contains(t, why, "g")
	require.Contains(t, why, "f")
	require.Contains(t, why, "tables-x-fuzz-all")
	require.Contains(t, why, "split g")
	require.Contains(t, why, "nova-sprint twin g --needs f")
	require.Nil(t, w.s.Work.Placed("g"), "nothing was written")
	require.Nil(t, w.s.Work.Placed("f"), "nothing was written")
}

// The same loop closed on the table: F needs G and F's DONE-WHEN runs the gate's
// command; editing G's DONE-WHEN to that command is refused.
func TestGateFixCycleRefusedAtBriefEdit(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.s.Now = time.Unix(1000, 0).UTC()
	w.must(Add(w.s, AddReq{Stream: "s", IDs: []string{"g"}, Brief: "RESULT: g sha=0123456789ab tier: pro\nDONE-WHEN: `make something-else`\n\nTHE TASK. Gate.\n", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "s", IDs: []string{"f"}, Needs: []string{"g"}, Brief: "RESULT: f sha=0123456789ab tier: pro\nDEPENDS-ON: g\nDONE-WHEN: `make tables-x-fuzz-all`\n\nTHE TASK. Fix.\n", Who: "coordinator"}))
	edited := "RESULT: g sha=0123456789ab tier: pro\nDONE-WHEN: `make tables-x-fuzz-all`\n\nTHE TASK. Gate.\n"
	p := Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "g", Brief: edited, Needs: nil}}, Who: "coordinator"})
	require.NotEmpty(t, p.Refused, "the brief edit closes the gate-fix cycle")
	why := ""
	for _, r := range p.Refused {
		why += r.Why + "\n"
	}
	require.Contains(t, why, "g")
	require.Contains(t, why, "f")
	require.Contains(t, why, "tables-x-fuzz-all")
	require.Contains(t, why, "split g")
	require.Contains(t, w.s.Work.Placed("g").F("brief"), "make something-else", "nothing was changed")
}

// The needs are walked transitively over open cards: F2 needs F1 needs G, and
// G's DONE-WHEN is F2's, so F2 can never be done before G and G waits for F2's
// work.
func TestGateFixCycleSeesTransitiveNeeds(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.s.Now = time.Unix(1000, 0).UTC()
	gate := "RESULT: g sha=0123456789ab tier: pro\nDONE-WHEN: `make tables-x-fuzz-all`\n\nTHE TASK. Gate.\n"
	mid := "RESULT: f1 sha=0123456789ab tier: pro\nDEPENDS-ON: g\n\nTHE TASK. Middle.\n"
	top := "RESULT: f2 sha=0123456789ab tier: pro\nDEPENDS-ON: f1\nDONE-WHEN: `make tables-x-fuzz-all`\n\nTHE TASK. Fix.\n"
	p := Add(w.s, AddReq{Stream: "s", Cards: []CardAdd{
		{ID: "g", Brief: gate},
		{ID: "f1", Brief: mid, Needs: []string{"g"}},
		{ID: "f2", Brief: top, Needs: []string{"f1"}},
	}, Who: "coordinator"})
	require.NotEmpty(t, p.Refused, "the transitive gate-fix cycle is refused")
	why := ""
	for _, r := range p.Refused {
		why += r.Why + "\n"
	}
	require.Contains(t, why, "g")
	require.Contains(t, why, "f2")
	require.Contains(t, why, "tables-x-fuzz-all")
}

// An add that closes no cycle is admitted even when the table already holds
// one: a pair a gate and its fix made before this rule (the live fuzzer gate
// and its fixes) is not this add's, and refusing it would hold every later add
// fleet-wide.
func TestGateFixUnrelatedAddNotRefused(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.s.Now = time.Unix(1000, 0).UTC()
	gate := "RESULT: g sha=0123456789ab tier: pro\nDONE-WHEN: `make tables-x-fuzz-all`\n\nTHE TASK. Gate.\n"
	fix := "RESULT: f sha=0123456789ab tier: pro\nDEPENDS-ON: g\nDONE-WHEN: `make tables-x-docs`\n\nTHE TASK. Fix.\n"
	w.must(Add(w.s, AddReq{Stream: "s", Cards: []CardAdd{
		{ID: "g", Brief: gate},
		{ID: "f", Brief: fix, Needs: []string{"g"}},
	}, Who: "coordinator"}))
	// the pair is already on the table, admitted before the rule: write the
	// gate's command into the fix's brief off the verbs, as the old table holds it
	c := w.s.Work.Card("f")
	require.NotNil(t, c)
	c.Fields["brief"] = "RESULT: f sha=0123456789ab tier: pro\nDEPENDS-ON: g\nDONE-WHEN: `make tables-x-fuzz-all`\n\nTHE TASK. Fix.\n"
	c.Rev++
	w.s.Work.Put(c)
	unrelated := "RESULT: u sha=0123456789ab tier: pro\nDONE-WHEN: `make tables-x-build`\n\nTHE TASK. Unrelated.\n"
	p := Add(w.s, AddReq{Stream: "s", Cards: []CardAdd{{ID: "u", Brief: unrelated}}, Who: "coordinator"})
	require.Empty(t, p.Refused, "an unrelated add is not the table's cycle: %v", p.Refused)
	w.do(p)
	require.NotNil(t, w.s.Work.Placed("u"), "the unrelated card is admitted")
}

// A dependent that runs another target is no gate-fix cycle: F waits for G and
// can make its own target green once G lands.
func TestNoGateFixWhenTheDependentRunsAnotherTarget(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.s.Now = time.Unix(1000, 0).UTC()
	gate := "RESULT: g sha=0123456789ab tier: pro\nDONE-WHEN: `make tables-x-fuzz-all`\n\nTHE TASK. Gate.\n"
	fix := "RESULT: f sha=0123456789ab tier: pro\nDEPENDS-ON: g\nDONE-WHEN: `make tables-x-docs`\n\nTHE TASK. Fix.\n"
	p := Add(w.s, AddReq{Stream: "s", Cards: []CardAdd{{ID: "g", Brief: gate}, {ID: "f", Brief: fix, Needs: []string{"g"}}}, Who: "coordinator"})
	require.Empty(t, p.Refused, "another target is no gate-fix cycle: %v", p.Refused)
}

// gateFixTestCommandKey pins the key the cycle compares, so a change to the
// parser is seen here: make drops VAR=value and flags, go test keeps its
// packages and -run pattern, and another command is its words with whitespace
// collapsed.
func TestGateFixCommandKey(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ cmd, key string }{
		{"make tables-x-fuzz-all", "tables-x-fuzz-all"},
		{"make ELIXIR=elixir tables-x-fuzz-all", "tables-x-fuzz-all"},
		{"make -j4 ELIXIR=elixir tables-x-fuzz-all", "tables-x-fuzz-all"},
		{"go test ./internal/sprint/ -run 'GateFix'", "./internal/sprint/ -run GateFix"},
		{"SCHEMA_SLOW=1 go test -count=1 -timeout 300s ./internal/sprint/ -run '^TestGateFix$' -v", "./internal/sprint/ -run ^TestGateFix$"},
		{"nova-sprint brief check  a  b", "nova-sprint brief check a b"},
	} {
		require.Equal(t, c.key, gateFixCommandKey(c.cmd), c.cmd)
	}
}

// gateFixTestKeys reads the backticked commands of a brief's DONE-WHEN and TEST
// lines only: prose, and a command outside a backtick, is no key.
func TestGateFixCommandKeysFromBrief(t *testing.T) {
	t.Parallel()
	brief := "RESULT: x sha=0123456789ab tier: pro\nTEST: `go test ./pkg -run TestX`\nDONE-WHEN: `make build` and `make test`\n\nTHE TASK. `make prose`.\n"
	done, test := gateFixBriefKeys(brief)
	require.Equal(t, []string{"build", "test"}, done)
	require.Equal(t, []string{"./pkg -run TestX"}, test)
	require.True(t, strings.Contains(strings.Join(done, " "), "build"))
}
