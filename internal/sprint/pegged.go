package sprint

import (
	"fmt"
	"strings"
	"time"
)

// A member pegged with no card working (docs/SPEC-SPRINT.md section 5, the fleet). Found
// 2026-10-12: a member beat at 100% load for 13 hours with no card working (busy loops a
// finished card left behind), the fleet table showed it up at 100.0%, and no judgment was
// raised: the seat learned of it only when the owner asked. The owner's rule: failure
// pushes the seat; every anomaly is a pushed judgment.
//
// A member up whose rows hold no working card and whose beats say HeavyLoadPercent or more
// (the load at which a last beat enters its loaded interval, presence.go) without a break
// for the pegged bound (nova-sprint set --pegged-bound, default PeggedBoundDefault) raises
// one judgment, NMemberPegged, to the coordinator, through the same notify every kept
// condition of the tick goes through: once an episode, never again while it stands (its
// line is the facts at the raise), closed with one cleared note when its load falls under
// HeavyLoadPercent, a card starts working there, or it is no longer up (held or down). A
// second episode raises again. The episode's start is the fleet property
// PropPeggedSince(<member>), written by the presence part at the first tick that sees the
// member pegged and removed at the first that does not. The model is tla/MemberPegged.tla
// (OneJudgmentPerPeggedEpisode).

// NMemberPegged is the judgment of a member up at HeavyLoadPercent or more for the pegged
// bound with no card working.
const NMemberPegged = "member-pegged"

// PropPeggedBound is the work table's property: how long a member with no card working may
// beat at HeavyLoadPercent or more before it is a judgment, a duration.
const PropPeggedBound = "pegged_bound"

// PeggedBoundDefault is the pegged bound when the coordinator set none.
const PeggedBoundDefault = 10 * time.Minute

// PropPeggedSince is the fleet property recording when the member's current pegged episode
// began (a stamp), absent while it is not pegged. A property name is letters, digits, _ .
// and -, so the member's name follows a dot.
func PropPeggedSince(member string) string { return "pegged_since." + member }

// PeggedBound is the pegged bound: the sprint's setting, else PeggedBoundDefault.
func (s *Snapshot) PeggedBound() time.Duration {
	if s.Work != nil {
		if v, ok := s.Work.Prop(PropPeggedBound); ok {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				return d
			}
		}
	}
	return PeggedBoundDefault
}

// PeggedNow says the member is pegged at now on its last beat: up on its control card and
// by its beat (held is not up, nor a member whose beats lapsed), no card working on its
// row, and its beat's load (the highest of LoadWindow) at HeavyLoadPercent or more.
func PeggedNow(s *Snapshot, member string, b Beat) bool {
	ctl := s.MemberCtl(member)
	if ctl == nil || ctl.F("status") != Up || MemberStatus(ctl, b, s.Now) != Up {
		return false
	}
	return b.Load >= HeavyLoadPercent && s.Fleet.Count(member, Working) == 0
}

// peggedWhat is the judgment's line: the member, its load, how long, and since when.
func peggedWhat(member string, load float64, d time.Duration, since time.Time) string {
	return fmt.Sprintf("member %s at %.1f%% for %s with no card working: a leftover process or outside load; read it on the machine (ps) and kill by PID or hold the member; pegged since %s",
		member, load, d.Round(time.Second), stamp(since))
}

// peggedDecisions are the judgment's: hold the member, acknowledge it (seen: quiet for the
// episode), or wait 15m (quiet for that running time, raised again if it still stands).
func peggedDecisions(member string) []string {
	return []string{"fleet down " + member, "ack", "wait 15m"}
}

// tickPegged is the presence part's pegged members (TickPresence): the episode stamps
// written and removed, a judgment for each member pegged for the bound, none while its
// episode stands, and one cleared note for each judgment its episode's end closes. With no
// beats read it does nothing: a missing read is no evidence.
func tickPegged(s *Snapshot, r TickReq) (Plan, int) {
	var p Plan
	if r.Beats == nil || s.Fleet == nil {
		return p, 0
	}
	bound := s.PeggedBound()
	var conds []cond
	stands := map[string]bool{}
	for _, m := range s.Members() {
		name := PropPeggedSince(m)
		was, had := s.Fleet.Prop(name)
		b := r.Beats[m]
		if !PeggedNow(s, m, b) {
			if had && was != "" {
				// the stamp's write is the episode's line in the log: a plan of
				// properties alone is no plan to the tick (Plan.Empty), as quietPlan's
				p.Props = append(p.Props, PropWrite{Table: Fleet, Name: name, Value: "", Was: was})
				p.Units = append(p.Units, Unit{Key: CtlID(m), Moved: fmt.Sprintf("%s pegged no more (since %s)", m, was)})
			}
			continue
		}
		since, err := time.Parse(time.RFC3339, was)
		if was == "" || err != nil {
			since = s.Now
			p.Props = append(p.Props, PropWrite{Table: Fleet, Name: name, Value: stamp(s.Now), Was: was, WasAbsent: !had})
			p.Units = append(p.Units, Unit{Key: CtlID(m), Moved: fmt.Sprintf("%s pegged at %.1f%% with no card working since %s: a judgment at %s", m, b.Load, stamp(s.Now), bound)})
		}
		if d := s.Now.Sub(since); d >= bound {
			stands[MemberSubject(m)] = true
			conds = append(conds, cond{typ: NMemberPegged, stream: MemberSubject(m), streamLevel: true,
				what: peggedWhat(m, b.Load, d, since), decisions: peggedDecisions(m)})
		}
	}
	due := notify(&p, s, conds, []string{NMemberPegged}, r)
	for _, o := range p.Closes {
		if stands[o.Note.Stream] {
			continue // a wait run out on an episode that stands is raised again, not cleared
		}
		m := strings.TrimPrefix(o.Note.Stream, MemberSubject(""))
		now := m + " is not up"
		switch b := r.Beats[m]; {
		case s.MemberCtl(m) != nil && s.MemberCtl(m).F("status") == Up && s.Fleet.Count(m, Working) > 0:
			now = fmt.Sprintf("%s works %d cards", m, s.Fleet.Count(m, Working))
		case s.MemberCtl(m) != nil && s.MemberCtl(m).F("status") == Up && MemberStatus(s.MemberCtl(m), b, s.Now) == Up:
			now = fmt.Sprintf("%s beats at %.1f%%, under %.0f%%", m, b.Load, HeavyLoadPercent)
		}
		p.Notes = append(p.Notes, Note{Kind: Happened, Type: NAlarmCleared, Who: r.who(), To: s.Coordinator, At: s.Now,
			What: NMemberPegged + ": " + now})
	}
	return p, due
}

// WithPeggedBound adds the pegged bound to set's plan (nova-sprint set --pegged-bound): a
// duration above zero, or default for PeggedBoundDefault. A set naming nothing else is no
// longer "nothing to set".
func WithPeggedBound(p Plan, s *Snapshot, v string) Plan {
	if v == "" {
		return p
	}
	for _, x := range p.Refused {
		if !strings.HasPrefix(x.Why, "nothing to set") {
			return p
		}
	}
	p.Refused = nil
	if v != ReadTierDefault {
		if d, err := time.ParseDuration(v); err != nil || d <= 0 {
			return Plan{Refused: []Refusal{{Key: "set", Why: "--pegged-bound wants a duration above zero (10m, 1h), or " + ReadTierDefault + " for " + PeggedBoundDefault.String() + "; found " + v}}}
		}
	}
	was, had := s.Work.Prop(PropPeggedBound)
	p.Props = append(p.Props, PropWrite{Table: Work, Name: PropPeggedBound, Value: v, Was: was, WasAbsent: !had})
	word := v
	if v == ReadTierDefault {
		word = fmt.Sprintf("default (%s)", PeggedBoundDefault)
	}
	for i := range p.Units {
		if p.Units[i].Key == "set" {
			if strings.TrimSpace(p.Units[i].Moved) == "sprint" {
				p.Units[i].Moved = "sprint pegged-bound " + word
			} else {
				p.Units[i].Moved += ", pegged-bound " + word
			}
			return p
		}
	}
	p.Units = append(p.Units, Unit{Key: "set", Moved: "sprint pegged-bound " + word})
	return p
}
