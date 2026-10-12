package main

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
	"github.com/stretchr/testify/require"
)

// The server's result carries the step's outcome, where it is known: a refusal
// is refused, a cut or a store that did not confirm (or a clear that closed
// the step as it finished) is unknown, success is accepted. The table covers
// every path #5625 classifies from the printed words (refuse, the step's cut,
// changed=unknown, "as this step finished", the cut text), now as the typed
// field each sets (docs/SPEC-SPRINT.md, the server; cmd/nova-sprint/main.go
// refuse, verbs.go runStep and report, store/engine.go ErrUnknown and CutError,
// store/epoch.go ClearedError).

func TestResultOutcomeTable(t *testing.T) {
	big := errors.New("a store error")
	tests := []struct {
		name    string
		res     store.Result
		err     error
		outcome string
		changed string
	}{
		{"success moved", store.Result{Moved: []string{"s1-1"}}, nil, "accepted", "yes"},
		{"success nothing moved", store.Result{}, nil, "accepted", "no"},
		{"refused card", store.Result{Refused: []sprint.Refusal{{Key: "s1-1", Why: "no"}}}, nil, "refused", "no"},
		{"store did not confirm", store.Result{}, store.ErrUnknown, "unknown", "unknown"},
		{"cut", store.Result{}, &store.CutError{Op: "o", Table: "work", Cause: big}, "unknown", "unknown"},
		{"cleared finished", store.Result{}, &store.ClearedError{Finished: true}, "unknown", "yes"},
		{"cleared not finished", store.Result{}, &store.ClearedError{}, "refused", "no"},
		{"pending", store.Result{}, &store.PendingError{Op: "o", Why: "busy"}, "refused", "no"},
		{"synced committed", store.Result{}, &store.SyncError{Cause: big}, "accepted", "yes"},
		{"table refusal", store.Result{}, &ntable.Refusal{Code: "NOTABLE"}, "refused", "no"},
		{"store did not answer", store.Result{}, big, "unknown", "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.outcome, resultOutcome(tc.res, tc.err))
			require.Equal(t, tc.changed, resultChanged(tc.res, tc.err))
		})
	}
}

// The server's answers carry the field, and the JSON output does too: a
// worker's accepted verb, a refused one (a pre-write refusal), and the drill
// of a verb that refused before it wrote.
func TestWorkerVerbOutcomeServed(t *testing.T) {
	r := newServerRig(t, twoLanes()...)

	accepted := r.one("take", "--as", "m1", "--limit", "1", "--epoch", "0")
	require.Equal(t, 0, accepted.Code, accepted.Stderr)
	require.Equal(t, "accepted", accepted.Outcome)
	require.Equal(t, "yes", accepted.Changed)

	refused := r.one("finish", "--as", "m1", "s1-1.w1@99", "--epoch", "0")
	require.Equal(t, "refused", refused.Outcome)
	require.Equal(t, "no", refused.Changed)

	// queue is a worker verb that is not a step: it returns from its cmd, so its
	// outcome is set where the queue answers, not by the report path. A served
	// queue that succeeded is accepted; it writes only a reader's own beat
	// (queue --as <reader>), so a member's queue (which writes no beat) is
	// accepted with changed=no (finding 2).
	queued := r.one("queue", "--as", "m1", "--json")
	require.Equal(t, 0, queued.Code, queued.Stderr)
	require.Equal(t, "accepted", queued.Outcome)
	require.Equal(t, "no", queued.Changed)
	var qj struct {
		Outcome string `json:"outcome"`
		Changed string `json:"changed"`
	}
	require.NoError(t, json.Unmarshal([]byte(queued.Stdout), &qj), queued.Stdout)
	require.Equal(t, "accepted", qj.Outcome)
	require.Equal(t, "no", qj.Changed)

	// the three budget verbs the server runs always answer their caller (exit 0):
	// a take/renew/release that succeeds is accepted, never the "refused"/"no" a
	// fresh app carries before its verb runs.
	paid := r.one("budget", "take", "m1/gpt", "--as", "m1", "--holder", "h1")
	require.Equal(t, 0, paid.Code, paid.Stderr)
	require.Equal(t, "accepted", paid.Outcome)
	require.Equal(t, "yes", paid.Changed)
	renewed := r.one("budget", "renew", "m1/gpt", "--as", "m1", "--holder", "h1")
	require.Equal(t, 0, renewed.Code, renewed.Stderr)
	require.Equal(t, "accepted", renewed.Outcome)
	released := r.one("budget", "release", "m1/gpt", "--as", "m1", "--holder", "h1")
	require.Equal(t, 0, released.Code, released.Stderr)
	require.Equal(t, "accepted", released.Outcome)
	require.Equal(t, "yes", released.Changed)
}

// friend beat is a worker verb the server answers on the beat lane, whose
// result follows its own exit code — but a member also runs it directly, and
// its own --json output must carry the field too, or a worker parsing it reads
// nothing. A beat that ran and wrote is accepted/yes. (The read's finding:
// friend beat's --json carried no outcome at all while every sibling did.)
func TestWorkerVerbOutcomeFriendBeatJSON(t *testing.T) {
	ta, _ := friendCardApp(t, "friend amy", "amy")
	var v map[string]any
	ta.json("friend beat amy", &v)
	require.Equal(t, "accepted", v["outcome"], "%v", v)
	require.Equal(t, "yes", v["changed"], "%v", v)
}

// The refusal paths #5625 names carry the field too: the server refusing a
// worker's malformed verb (serve.go's workerVerb), a report naming no epoch
// (verbs.go runStep's pre-write checks), and a usage refusal (main.go refuse).
// Each is a definite refusal with nothing written, so the field is refused/no
// whatever the words say.
func TestResultOutcomeRefusedPaths(t *testing.T) {
	r := newServerRig(t, twoLanes()...)
	tests := []struct {
		name string
		argv []string
	}{
		{"server runs not this verb", []string{"land", "--as", "m1"}},
		{"report names no epoch", []string{"finish", "--as", "m1", "s1-1.w1"}},
		{"finish names no card", []string{"finish", "--as", "m1"}},
		{"usage refusal", []string{"take", "--as", "m1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := r.one(tc.argv...)
			require.Equal(t, 2, res.Code, res.Stderr)
			require.Equal(t, "refused", res.Outcome)
			require.Equal(t, "no", res.Changed)
		})
	}
}

// A friend beat of a friend the roster lacks is a definite pre-write refusal:
// the store answered and nothing was written, so the field is refused/no — on
// the verb's own --json output and on the served result — not unknown (finding
// 1), agreeing with the sibling friend cards verb.
func TestWorkerVerbOutcomeFriendBeatRefused(t *testing.T) {
	ta, _ := friendApp(t, "amy")
	ta.ok("friend sync --root " + t.TempDir())
	code, _, errs := ta.do("friend beat bob --json")
	require.Equal(t, 1, code, errs)
	require.Contains(t, errs, "no friend bob on the friends table (friends: amy)")
	require.Equal(t, "refused", ta.a.outcome)
	require.Equal(t, "no", ta.a.changed)

	r := newServerRig(t, "nova-sprint init --readers reader-a,reader-b --members m1:2")
	r.a.friends = friendRows("amy")
	r.boss("nova-sprint friend sync --root " + t.TempDir())
	res := r.one("friend", "beat", "bob")
	require.Equal(t, 1, res.Code, res.Stderr)
	require.Contains(t, res.Stderr, "no friend bob on the friends table (friends: amy)")
	require.Equal(t, "refused", res.Outcome)
	require.Equal(t, "no", res.Changed)
}

// lane give reports changed by what it wrote: a give that gave nothing (the
// worker held no lane and waited in no queue) is accepted but changed=no
// (finding 2).
func TestWorkerVerbOutcomeLaneGiveWritesNothing(t *testing.T) {
	r := newServerRig(t, twoLanes()...)
	res := r.one("lane", "give", "go", "--machine", "m1", "--as", "m1")
	require.Equal(t, 0, res.Code, res.Stderr)
	require.Equal(t, "accepted", res.Outcome)
	require.Equal(t, "no", res.Changed)
}

// budget take reports changed by whether it wrote a grant: a take that was not
// granted (the caller was told to wait) is accepted but changed=no, never yes
// (finding 2).
func TestWorkerVerbOutcomeBudgetTakeGrantedNone(t *testing.T) {
	ta := mercuryApp(t)
	ta.ok("routes limit deepseek --concurrent 1 --reason account-limit")
	const ds = "deepseek/deepseek-chat"
	first := ta.ok("budget take " + ds + " --as m1 --holder a")
	require.Contains(t, first, "granted=yes")
	var got struct {
		Granted bool   `json:"granted"`
		Outcome string `json:"outcome"`
		Changed string `json:"changed"`
	}
	ta.json("budget take "+ds+" --as m2 --holder b", &got)
	require.False(t, got.Granted)
	require.Equal(t, "accepted", got.Outcome)
	require.Equal(t, "no", got.Changed)
}

// queue --stream is a read that writes nothing: its outcome is accepted (it ran
// and answered) with changed=no (finding 2).
func TestWorkerVerbOutcomeQueueStreamWritesNothing(t *testing.T) {
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1,m2")
	ta.ok("add --stream s1 --count 4")
	code, out, errs := ta.do("queue --stream s1 --json")
	require.Equal(t, 0, code, "%s%s", out, errs)
	var q struct {
		Outcome string `json:"outcome"`
		Changed string `json:"changed"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &q))
	require.Equal(t, "accepted", q.Outcome)
	require.Equal(t, "no", q.Changed)
}
