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

// BaseLandedKey names the old per-base work-table property. It is updated in
// place on every landing of a base that already holds it and never created, so
// a v1.6.3 binary reads the current tip of every base it knew; a base new under
// v1.7 has no property and reads as unknown provenance under v1.6.3, which is
// v1.6.3's own first-red protection.
func BaseLandedKey(repo, base string) string {
	return fmt.Sprintf("base_landed.%x", sha256.Sum256([]byte(repo+"\x00"+base)))
}

// BaseLandingUnits puts the pushed tip on the landing stream's control row
// (v17-base-landed-tip-on-the-stream-row) and keeps the old per-base work-table
// property current where it already exists, never creating it: a v1.6.3 binary
// reads the current tip of every base it knew, while a base new under v1.7 has
// no property and reads as unknown provenance under v1.6.3, which is v1.6.3's
// own first-red protection. With no control row for stream, only an existing
// property is updated. Every other stream's control row holding the same
// repository and branch has the three fields unset, so exactly one row holds a
// given base.
func BaseLandingUnits(s *Snapshot, p Plan, stream, repo, base, tip string) Plan {
	key := BaseLandedKey(repo, base)
	if was, had := s.Work.Prop(key); had {
		p.Props = append(p.Props, PropWrite{Table: Work, Name: key, Value: tip, Was: was})
	}
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

// BaseMovedOutside compares the tip with durable lander provenance. The tip is
// the lander's when the control row holding the repository and branch names it,
// or the old work-table property names it, so a v1.6.3 binary's landing between
// two v1.7 landings is not mistaken for an outside push. It is moved outside
// only when neither names it; an absent record on both is unknown provenance
// and receives the same first-red protection.
func BaseMovedOutside(s *Snapshot, repo, base, tip string) bool {
	if tip == "" {
		return true
	}
	for _, st := range s.Streams() {
		ctl := s.StreamCtl(st)
		if ctl != nil && ctl.F(FieldBaseLandedRepo) == repo && ctl.F(FieldBaseLandedBase) == base && ctl.F(FieldBaseLandedTip) == tip {
			return false
		}
	}
	last, ok := s.Work.Prop(BaseLandedKey(repo, base))
	return !ok || last != tip
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

// FieldForwardMerge is the newest forward-merge tip a card must be checked against.
// A card is pending when this differs from FieldForwardMergeChecked; Deal refuses it
// until the coordinator has recomputed its behind count and brief-at-base checks.
const (
	FieldForwardMerge        = "forward_merge"
	FieldForwardMergeChecked = "forward_merge_checked"
	FieldForwardMergeBehind  = "forward_merge_behind"
)

// NBaseForwardMerged is the happened note of a recorded forward-merge of a base.
const NBaseForwardMerged = "a base took a forward-merge"

// ForwardMergeCardCheck pins one card's brief at the time the coordinator re-ran its
// brief-at-base checks. The digest lets the store fence a concurrent brief edit without
// retaining the card's whole brief in the operation arguments.
type ForwardMergeCardCheck struct {
	ID, Stream, BriefSHA256 string
}

// ForwardMergeReq records a verified forward-merge. Repo scopes the base identity;
// Base and Merge are the destination branch and commit, and From names the merged branch.
type ForwardMergeReq struct {
	Repo   string
	Base   string
	From   string
	Merge  string
	Who    string
	Reason string
}

// ForwardMergeRecheckReq records successful behind and brief-at-base checks for the exact
// cards the forward-merge marked pending. Brief digests fence edits while checks ran.
type ForwardMergeRecheckReq struct {
	Repo   string
	Base   string
	Merge  string
	Who    string
	Behind int
	Checks []ForwardMergeCardCheck
}

// RecordForwardMerge records a verified move in the durable (repo, base) tip used by
// BaseMovedOutside and marks every waiting, ready and review card pending. The coordinator
// clears that mark only in a separate RecheckForwardMerge step after its checks pass.
func RecordForwardMerge(s *Snapshot, r ForwardMergeReq) Plan { return Lawful(recordForwardMerge(s, r)) }

func recordForwardMerge(s *Snapshot, r ForwardMergeReq) Plan {
	var p Plan
	p.on(s)
	if r.Repo == "" || r.Base == "" || r.From == "" || r.Merge == "" {
		p.refuse(r.Who, "a forward-merge wants its repository, source branch, destination base and merge commit")
		return p
	}
	if tip := forwardMergeRecordedTip(s, r.Repo, r.Base); tip == r.Merge {
		p.refuse(r.Who, "the forward-merge tip is already recorded; resume its card rechecks instead")
		return p
	}
	type hit struct {
		card   *Card
		stream string
	}
	var hits []hit
	var ids []string
	for _, col := range []State{Waiting, Ready, Review} {
		for _, st := range s.Streams() {
			for _, c := range s.Work.Cell(st, col) {
				if c.F("kind") == "sentinel" {
					continue
				}
				repo, base := CardRepoAndBase(s, c)
				if repo != r.Repo || base != r.Base {
					continue
				}
				hits = append(hits, hit{c, st})
				ids = append(ids, c.ID)
			}
		}
	}
	if len(hits) == 0 {
		p.refuse(r.Who, "no waiting, ready or review card names "+r.Repo+" BASE "+r.Base+"; nothing was changed")
		return p
	}
	what := fmt.Sprintf("%s %s took a forward-merge of %s at %s: its waiting, ready and review cards are pending brief and behind rechecks%s",
		r.Repo, r.Base, r.From, r.Merge, cutText(": "+r.Reason, MaxCardTextBytes))
	for _, h := range hits {
		p.Units = append(p.Units, Unit{Key: h.card.ID, Stream: h.stream,
			Changes: []Change{change(Work, setEntry(h.card, map[string]string{FieldForwardMerge: r.Merge},
				FieldForwardMergeChecked, FieldForwardMergeBehind))},
			Moved: fmt.Sprintf("%s: %s BASE %s forward-merged to %s; recheck pending", h.card.ID, r.Repo, r.Base, r.Merge)})
	}
	p.Notes = append(p.Notes, Note{Kind: Happened, Type: NBaseForwardMerged, Stream: hits[0].stream, Primaries: ids, What: what, Who: r.Who, At: s.Now})
	return BaseLandingUnits(s, p, hits[0].stream, r.Repo, r.Base, r.Merge)
}

// RecheckForwardMerge clears pending only after every card marked for this tip has a
// matching current brief witness. A changed brief or card set leaves all marks in place.
func RecheckForwardMerge(s *Snapshot, r ForwardMergeRecheckReq) Plan {
	var p Plan
	p.on(s)
	if r.Repo == "" || r.Base == "" || r.Merge == "" || r.Behind < 0 {
		p.refuse(r.Who, "a forward-merge recheck wants its repository, base, merge tip and non-negative behind count")
		return p
	}
	if forwardMergeRecordedTip(s, r.Repo, r.Base) != r.Merge {
		p.refuse(r.Who, "the recorded base tip moved during the forward-merge recheck; pending marks remain; run the verb again")
		return p
	}
	type hit struct {
		card   *Card
		stream string
	}
	var hits []hit
	for _, col := range []State{Waiting, Ready, Review} {
		for _, st := range s.Streams() {
			for _, c := range s.Work.Cell(st, col) {
				if c.F("kind") == "sentinel" || c.F(FieldForwardMerge) != r.Merge || c.F(FieldForwardMergeChecked) == r.Merge {
					continue
				}
				repo, base := CardRepoAndBase(s, c)
				if repo == r.Repo && base == r.Base {
					hits = append(hits, hit{c, st})
				}
			}
		}
	}
	if len(hits) == 0 {
		p.refuse(r.Who, "no cards remain pending recheck for "+r.Repo+" BASE "+r.Base+" at "+r.Merge)
		return p
	}
	checks := make(map[string]ForwardMergeCardCheck, len(r.Checks))
	for _, check := range r.Checks {
		if check.ID == "" || check.Stream == "" || check.BriefSHA256 == "" || checks[check.ID].ID != "" {
			p.refuse(r.Who, "the forward-merge recheck has an invalid or duplicate card witness; pending marks remain")
			return p
		}
		checks[check.ID] = check
	}
	if len(checks) != len(hits) {
		p.refuse(r.Who, "the pending cards changed during the forward-merge recheck; pending marks remain; run the verb again")
		return p
	}
	for _, h := range hits {
		w := checks[h.card.ID]
		if w.ID == "" || w.Stream != h.stream || w.BriefSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(h.card.F("brief")))) {
			p.refuse(r.Who, "card "+h.card.ID+" changed during the forward-merge recheck; pending marks remain; run the verb again")
			return p
		}
	}
	for _, h := range hits {
		p.Units = append(p.Units, Unit{Key: h.card.ID, Stream: h.stream,
			Changes: []Change{change(Work, setEntry(h.card, map[string]string{
				FieldForwardMergeChecked: r.Merge, FieldForwardMergeBehind: fmt.Sprint(r.Behind)}))},
			Moved: fmt.Sprintf("%s: %s BASE %s rechecked at %s; behind=%d", h.card.ID, r.Repo, r.Base, r.Merge, r.Behind)})
	}
	return p
}

func forwardMergeRecordedTip(s *Snapshot, repo, base string) string {
	if s == nil {
		return ""
	}
	for _, stream := range s.Streams() {
		ctl := s.StreamCtl(stream)
		if ctl != nil && ctl.F(FieldBaseLandedRepo) == repo && ctl.F(FieldBaseLandedBase) == base {
			return ctl.F(FieldBaseLandedTip)
		}
	}
	if tip, ok := s.Work.Prop(BaseLandedKey(repo, base)); ok {
		return tip
	}
	return ""
}
