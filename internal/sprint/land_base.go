package sprint

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// The base's health is one fact (docs/SPEC-SPRINT.md section 8, v11-base-red-auto-resume-now;
// the coordinator, 2026-10-04: the base went red for a few minutes, every stream that tried to
// land stopped with a judgment of its own, and eight were resumed by hand once it was fixed).
// The first stream a red base stops carries the one judgment, naming the failing tests; every
// other stream that meets the same red base is refused under it and never stopped, so it
// raises no judgment of its own and lands at its first pass on a green base (the lander's
// refusal, LandBaseRefused). Each land pass re-checks the tip of a base that stopped streams
// (cmd/nova-sprint, land.go, baseRecheck), and a green tip is the fact BaseGreen: every stream
// stopped on that base is marked (FieldBaseGatePassed), and the tick's base-gate rule resumes
// each of them (rules.go, TickRuleResume), the judgment answered by rule in the log.

// FieldBaseGatePassed is the base commit the lander found green again, on the control card of
// a stream stopped on its base's red (cause base); the base-gate rule resumes the stream.
// FieldBaseGatePassedStop is the stop it was found for (the card's since): a mark left from
// an earlier stop (a hand resume does not clear it) is no mark (basePassed).
const (
	FieldBaseGatePassed     = "base_gate_passed"
	FieldBaseGatePassedStop = "base_gate_passed_stop"
)

// basePassed is the base commit found green for the stream's current stop, "" when none was.
func basePassed(ctl *Card) string {
	if ctl.F(FieldBaseGatePassedStop) != ctl.F("since") {
		return ""
	}
	return ctl.F(FieldBaseGatePassed)
}

// LandBaseRefused is the lander's refusal on a red base: the merge step's count
// (MergeReq.BaseRefused, BaseRed with Base), with the base's one judgment. A stream that meets
// a base whose red already stopped another stream is refused under that stream's judgment and
// never stopped; the stream that stops keeps the base on its control card, and its judgment
// names the failing tests and the rule that resumes it. A hand merge --base-red is the merge
// step's own and stops as it always did.
func LandBaseRefused(s *Snapshot, r MergeReq) Plan { return Lawful(landBaseRefused(s, r)) }

func landBaseRefused(s *Snapshot, r MergeReq) Plan {
	ctl := s.StreamCtl(r.Stream)
	if ctl == nil || r.Base == "" || r.BaseRed == "" && r.BaseRefused == "" {
		return mergeStep(s, r)
	}
	if st := ctl.F("state"); st != StreamStopped && st != StreamLanded {
		if holder := baseRedHolder(s, r.Base, r.Stream); holder != "" {
			var p Plan
			p.on(s)
			return baseRedRefused(p, s, ctl, r, holder)
		}
	}
	p := mergeStep(s, r)
	for i := range p.Units {
		u := &p.Units[i]
		for j := range u.Notes {
			if u.Key != ctl.ID || u.Notes[j].Kind != Judgment || u.Notes[j].Type != NBaseRed {
				continue
			}
			// the base is kept on the stop: the land pass re-checks its tip, and the streams
			// that meet it red after this one are refused under this judgment
			set := map[string]string{"state": StreamStopped, "since": stamp(s.Now), "cause": "base", FieldBaseGateBase: r.Base}
			u.Changes = []Change{change(Merge, setEntry(ctl, set, "card", "other", FieldBaseGateRefused, FieldBaseGateFirst, FieldBaseGatePassed, FieldBaseGatePassedStop))}
			u.Notes[j].What = cutText(baseRedSaid(r.Base, u.Notes[j].What, r.BaseRed+" "+r.BaseRefused), MaxCardTextBytes)
		}
	}
	return p
}

// failingTestRE is a failing test as go test prints it.
var failingTestRE = regexp.MustCompile(`--- FAIL: (Test[^\s(|]*)`)

// FailingTests are the tests a tree gate's finding names as failing, once each, in order.
func FailingTests(why string) []string {
	var out []string
	for _, m := range failingTestRE.FindAllStringSubmatch(why, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

// baseRedHolder is the stream, other than except, stopped on the red of the base and holding
// its one judgment open, "" when none is: the base is already known red.
func baseRedHolder(s *Snapshot, base, except string) string {
	if base == "" {
		return ""
	}
	for _, o := range s.Open {
		if o.Note.Kind != Judgment || o.Note.Type != NBaseRed || o.Note.Stream == except {
			continue
		}
		ctl := s.StreamCtl(o.Note.Stream)
		if ctl != nil && ctl.F("state") == StreamStopped && ctl.F("cause") == "base" && ctl.F(FieldBaseGateBase) == base && basePassed(ctl) == "" {
			return o.Note.Stream
		}
	}
	return ""
}

// baseRedRefused counts a refusal on a base whose red already stopped another stream (holder)
// and never stops this one: the holder's judgment stands for every landing on the base, so
// no second judgment is raised, and the stream lands at its first pass after the base is
// green again.
func baseRedRefused(p Plan, s *Snapshot, ctl *Card, r MergeReq, holder string) Plan {
	n, first := 1, stamp(s.Now)
	if m := ctl.Int(FieldBaseGateRefused); m > 0 && ctl.F(FieldBaseGateBase) == r.Base {
		n = m + 1
		if f := ctl.F(FieldBaseGateFirst); f != "" {
			first = f
		}
	}
	set := map[string]string{FieldBaseGateRefused: itoa(n), FieldBaseGateBase: r.Base, FieldBaseGateFirst: first}
	p.Units = append(p.Units, Unit{Key: ctl.ID, Stream: r.Stream, Changes: []Change{change(Merge, setEntry(ctl, set))},
		Moved: fmt.Sprintf("stream %s: the base %s refused at its tree gate (%d); landing on it is stopped under the judgment on stream %s", r.Stream, r.Base, n, holder)})
	return p
}

// baseRedSaid is the one judgment's text: the base, the failing tests and the finding.
func baseRedSaid(base, what, why string) string {
	if tests := FailingTests(why); len(tests) > 0 {
		what = "failing " + strings.Join(tests, ", ") + "; " + what
	}
	return what + "; every landing on " + orDash(base) + " is refused under this judgment, and its stream resumes by rule when a land pass finds its tip green"
}

// BaseGreenStreams are the streams stopped on a base's red that the lander found green again,
// in stream order: the base-gate rule's to resume.
func BaseGreenStreams(s *Snapshot) []string {
	var out []string
	for _, st := range s.Streams() {
		ctl := s.StreamCtl(st)
		if ctl != nil && ctl.F("state") == StreamStopped && ctl.F("cause") == "base" && basePassed(ctl) != "" {
			out = append(out, st)
		}
	}
	return out
}

// BaseRedStreams are the streams stopped on a base's red and not yet found green: the ones a
// land pass re-checks the base of.
func BaseRedStreams(s *Snapshot) []string {
	var out []string
	for _, st := range s.Streams() {
		ctl := s.StreamCtl(st)
		if ctl != nil && ctl.F("state") == StreamStopped && ctl.F("cause") == "base" && basePassed(ctl) == "" {
			out = append(out, st)
		}
	}
	return out
}

// BaseGreenReq is a land pass's re-check of a base that stopped streams: the base Base passes
// its tree gate at the commit Sha. Stream is the stream the pass re-checked it for.
type BaseGreenReq struct {
	Stream string
	Base   string
	Sha    string
	Who    string
}

// BaseGreen records a green base: every stream stopped on the red of r.Base is marked with the
// commit, for the stop it is in (FieldBaseGatePassedStop), and the tick's base-gate rule
// resumes it; a stream stopped with no base named (a hand merge --base-red) is marked when it
// is r.Stream. No card moves; with nothing stopped on the base it is refused.
func BaseGreen(s *Snapshot, r BaseGreenReq) Plan { return Lawful(baseGreen(s, r)) }

func baseGreen(s *Snapshot, r BaseGreenReq) Plan {
	var p Plan
	p.on(s)
	if r.Base == "" || r.Sha == "" {
		p.refuse(r.Stream, "a green base wants its branch and its commit")
		return p
	}
	for _, st := range s.Streams() {
		ctl := s.StreamCtl(st)
		if ctl == nil || ctl.F("state") != StreamStopped || ctl.F("cause") != "base" || basePassed(ctl) != "" {
			continue
		}
		if b := ctl.F(FieldBaseGateBase); b != r.Base && (b != "" || st != r.Stream) {
			continue
		}
		set := map[string]string{FieldBaseGatePassed: r.Sha, FieldBaseGatePassedStop: ctl.F("since"), FieldBaseGateBase: r.Base}
		p.Units = append(p.Units, Unit{Key: ctl.ID, Stream: st, Changes: []Change{change(Merge, setEntry(ctl, set))},
			Moved: fmt.Sprintf("stream %s: the base %s passes its tree gate again at %s", st, r.Base, r.Sha)})
	}
	if len(p.Units) == 0 {
		p.refuse(r.Stream, "no stream is stopped on the red of the base "+r.Base+"; nothing was changed")
	}
	return p
}

// clearBasePassed has a resume of the stream's control card clear the green base's mark,
// which the merge step's resume leaves.
func clearBasePassed(u *Unit, ctl *Card) {
	for i := range u.Changes {
		if e := &u.Changes[i].Entry; e.ID == ctl.ID && u.Changes[i].Table == Merge {
			e.Unset = append(e.Unset, unsetPresent(ctl, []string{FieldBaseGatePassed, FieldBaseGatePassedStop})...)
		}
	}
}

// The landed tip is a field of the landing stream's control row, never a
// work-table property (v17-base-landed-tip-on-the-stream-row; the coordinator,
// 2026-10-17: from 14:02 every tick drain was refused because the work table's
// one base_landed.<sha256> property per repository and branch ever pushed
// passed the table's property bound). One control row holds a given
// (repository, branch).
const (
	FieldBaseLandedRepo = "base_landed_repo"
	FieldBaseLandedBase = "base_landed_base"
	FieldBaseLandedTip  = "base_landed_tip"
)

// BaseLandedKey names the old per-base work-table property. It is read only,
// never written: a store that already holds tips keeps them, and its record is
// the fallback for a base no control row holds.
func BaseLandedKey(repo, base string) string {
	return fmt.Sprintf("base_landed.%x", sha256.Sum256([]byte(repo+"\x00"+base)))
}

// BaseLandingUnits puts the pushed tip on the landing stream's control row
// (v17-base-landed-tip-on-the-stream-row). With no control row for stream, p is
// unchanged. Every other stream's control row holding the same repository and
// branch has the three fields unset, so exactly one row holds a given base.
func BaseLandingUnits(s *Snapshot, p Plan, stream, repo, base, tip string) Plan {
	ctl := s.StreamCtl(stream)
	if ctl == nil {
		return p
	}
	set := map[string]string{FieldBaseLandedRepo: repo, FieldBaseLandedBase: base, FieldBaseLandedTip: tip}
	merged := false
	for i := range p.Units {
		u := &p.Units[i]
		for j := range u.Changes {
			e := &u.Changes[j].Entry
			if u.Changes[j].Table != Merge || e.ID != ctl.ID {
				continue
			}
			if e.Set == nil {
				e.Set = make(map[string]string, len(set))
			}
			for k, v := range set {
				e.Set[k] = v
			}
			merged = true
		}
	}
	if !merged {
		p.Units = append(p.Units, Unit{Key: ctl.ID, Stream: stream, Changes: []Change{change(Merge, setEntry(ctl, set))}})
	}
	for _, o := range s.Streams() {
		if o == stream {
			continue
		}
		oc := s.StreamCtl(o)
		if oc == nil || oc.F(FieldBaseLandedRepo) != repo || oc.F(FieldBaseLandedBase) != base {
			continue
		}
		p.Units = append(p.Units, Unit{Key: oc.ID, Stream: o,
			Changes: []Change{change(Merge, setEntry(oc, nil, FieldBaseLandedRepo, FieldBaseLandedBase, FieldBaseLandedTip))}})
	}
	return p
}

// BaseMovedOutside compares the tip with durable lander provenance. The control
// row holding the repository and branch decides; when no row holds it, the old
// work-table property is read as today. An absent record is unknown provenance
// and receives the same first-red protection.
func BaseMovedOutside(s *Snapshot, repo, base, tip string) bool {
	for _, st := range s.Streams() {
		ctl := s.StreamCtl(st)
		if ctl != nil && ctl.F(FieldBaseLandedRepo) == repo && ctl.F(FieldBaseLandedBase) == base {
			return tip == "" || ctl.F(FieldBaseLandedTip) != tip
		}
	}
	last, _ := s.Work.Prop(BaseLandedKey(repo, base))
	return last == "" || tip == "" || last != tip
}

// RecordBaseLanding adds the pushed tip to the successful merge report, in the
// same guarded transaction. Failed reports do not certify a new base tip.
func RecordBaseLanding(s *Snapshot, r MergeReq, repo, base, tip string) Plan {
	p := MergeStep(s, r)
	if len(p.Refused) != 0 || repo == "" || base == "" || tip == "" {
		return p
	}
	landed := false
	for _, u := range p.Units {
		if strings.Contains(u.Moved, " merging -> landed") {
			landed = true
		}
	}
	if !landed {
		return p
	}
	return BaseLandingUnits(s, p, r.Stream, repo, base, tip)
}

// OutsideBaseRed opens the existing base-red episode on the first red tree gate
// of a tip moved outside the lander. No card is attempted or counted as refused.
func OutsideBaseRed(s *Snapshot, r BaseGreenReq, why string) Plan {
	return LandBaseRefused(s, MergeReq{Stream: r.Stream, Base: r.Base, Who: r.Who,
		BaseRed: fmt.Sprintf("outside commit %s on %s: %s", r.Sha, r.Base, why)})
}
