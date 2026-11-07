package policy

import (
	"context"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// Trace validation (tla/README.md, "Trace validation"): the policy walk
// (policy/trace_test.go) records, after every step, the step and the code's
// state projected onto the variables of tla/Policy.tla, and
// tla/PolicyTrace.tla checks the recording is a behaviour of the model.
// TraceState is the refinement mapping. Every variable of Policy.tla:
//
//   - stored, active: the store's policy versions and its active one.
//   - prev: LoadVersions' Prev (the version active before the last
//     activation), -1 (None) when there is none.
//   - breaks: the versions, of the stored ones and the built-in (0), that
//     fail on the current snapshot: each run by check on every question the
//     snapshot poses (FullQuestions), on an engine of its own. A version not
//     yet stored is never in breaks: Set checks a file before it stores it,
//     so a file that would fail is a refused step, never a state.
//   - answeredBy: the last tick's Answers.Version, -2 (NoAnswer) for -1, and
//     -1 (None) before the first tick.
//   - judgements: the open policy judgements of each version, by group key.
//   - card: the state of one producer of the store (ready, working, done),
//     moved by the walk's ticks, as layer 3 would: the one card of the model.
//   - everActive, tickBreaks, cardBefore, waitBound, startBound, lastOp: the
//     model's own bookkeeping, not projected; each is fixed by the step the
//     record names (the trace module checks it as it computes it).
type ModelState struct {
	Stored, Breaks []int
	Active, Prev   int
	AnsweredBy     int
	Judgements     []int // by version, 0 (the built-in) to MaxVersion
	Card           string
}

// TraceState projects the store, the engine's built-in and the last tick's
// answers (nil before the first tick) onto Policy.tla, with versions up to
// maxVersion; card names the model's one card. ro is the roster the
// snapshot is read with: breaks is computed on the snapshot and ro, as a
// tick computes it.
func TraceState(ctx context.Context, st store.Store, ro roster.Roster, e *Engine, tick *Answers, card string, maxVersion int) (ModelState, error) {
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return ModelState{}, err
	}
	vs, err := LoadVersions(ctx, st)
	if err != nil || vs.Active == nil {
		return ModelState{}, upFirst("trace", err)
	}
	all, err := versions(ctx, st)
	if err != nil {
		return ModelState{}, err
	}
	m := ModelState{Active: vs.Active.Version, Prev: -1, AnsweredBy: -1, Card: snap.Cards[card].State}
	if vs.Prev != nil {
		m.Prev = vs.Prev.Version
	}
	if tick != nil {
		m.AnsweredBy = map[bool]int{true: -2, false: tick.Version}[tick.Version == -1]
	}
	runs := NewEngine()
	runs.builtin = e.builtin
	for _, src := range append([]source{storedSource(0, e.builtin)}, sources(all)...) {
		if _, fs := runs.checkRun(ctx, src, snap, ro); len(fs) > 0 {
			m.Breaks = append(m.Breaks, src.version)
		}
	}
	for v := range maxVersion + 1 {
		m.Judgements = append(m.Judgements, len(OpenJudgements(snap, v)))
		if slices.ContainsFunc(all, func(pv store.PolicyVersion) bool { return pv.Version == v }) {
			m.Stored = append(m.Stored, v)
		}
	}
	return m, nil
}

func sources(all []store.PolicyVersion) []source {
	out := make([]source, len(all))
	for i, pv := range all {
		out[i] = storedSource(pv.Version, pv.Body)
	}
	return out
}
