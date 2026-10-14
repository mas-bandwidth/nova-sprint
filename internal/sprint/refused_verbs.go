package sprint

import (
	"fmt"
	"maps"
	"slices"
	"time"
)

// The doctor names the symptom (a member down) and not the cause (its beats refused): the
// server refuses a beat and only the member's own log held why. The owner, 2026-10-11
// ~18:35Z: "Run the doctors. If the doctors don't catch it, update them so they do." These
// are the records and the pure grouping the two checks read (docs/SPEC-SPRINT.md, "The
// doctor's refused-verb and beat-form checks"). The records are the server's own refused-verb
// record, keyed by epoch, never a member's log.

// RefusedVerb is one verb the sprint's server answered with a non-zero code, as the doctor
// reads it: the epoch it was refused under, when it was refused, the verb's own words
// (Verb), the member that sent it, the card it named (Card, empty when it named none), what
// it sent (Argv) and the server's refusal text (Why, the verb's own first stderr line).
type RefusedVerb struct {
	Epoch  uint64
	At     time.Time
	Verb   string
	Member string
	Card   string
	Argv   string
	Why    string
}

// RefusedRecord is the server's own refused-verb record for one epoch: the epoch it was
// refused under and the verbs it refused, oldest first. The record is keyed by epoch: one of
// another epoch is not the sprint's current one and is not counted. It is the source the two
// refused-verb checks read, never a member's log.
type RefusedRecord struct {
	Epoch uint64        `json:"epoch"`
	Verbs []RefusedVerb `json:"verbs,omitempty"`
}

// PruneRefused is the record's verbs within RefusedWindow of now: the record keeps the recent
// window and drops the rest, so it stays bounded as the server appends.
func PruneRefused(recs []RefusedVerb, now time.Time) []RefusedVerb {
	return RefusedIn(recs, now, RefusedWindow)
}

// The bounds and the window the refused-verb checks judge by.
const (
	// RefusedWindow is the window the refused verbs are counted in.
	RefusedWindow = 10 * time.Minute
	// RefusedBeatBound is how many refused `fleet beat` verbs in RefusedWindow make one member
	// a FAIL of its own, before and independent of the member-down stop.
	RefusedBeatBound = 3
	// RefusedVerbBound is how many refused verbs of one verb word in RefusedWindow make one
	// member a FAIL, for every verb and not only beat.
	RefusedVerbBound = 10
	// RefusedBeatVerb is the verb word a refused beat is named by.
	RefusedBeatVerb = "fleet beat"
)

// RefusedIn is the records within window of now: the window's other edge is the window's
// length before now, so a record exactly a window old is out.
func RefusedIn(recs []RefusedVerb, now time.Time, window time.Duration) []RefusedVerb {
	var out []RefusedVerb
	for _, r := range recs {
		if !r.At.IsZero() && r.At.After(now.Add(-window)) && !r.At.After(now) {
			out = append(out, r)
		}
	}
	return out
}

// RefusedBeats is the refused beat verbs in the window, by member, newest first. It is the
// member-beat check's source: a member with RefusedBeatBound or more is one FAIL.
func RefusedBeats(recs []RefusedVerb, now time.Time) map[string][]RefusedVerb {
	out := map[string][]RefusedVerb{}
	for _, r := range RefusedIn(recs, now, RefusedWindow) {
		if r.Verb != RefusedBeatVerb {
			continue
		}
		out[r.Member] = append(out[r.Member], r)
	}
	for m := range out {
		slices.SortStableFunc(out[m], func(a, b RefusedVerb) int { return b.At.Compare(a.At) })
	}
	return out
}

// RefusedVerbGroups is every refused verb in the window, by member then by verb word, newest
// first. It is the refused-verbs check's source: a member and verb with RefusedVerbBound or
// more is one FAIL, for every verb and not only beat.
func RefusedVerbGroups(recs []RefusedVerb, now time.Time) map[string]map[string][]RefusedVerb {
	out := map[string]map[string][]RefusedVerb{}
	for _, r := range RefusedIn(recs, now, RefusedWindow) {
		if out[r.Member] == nil {
			out[r.Member] = map[string][]RefusedVerb{}
		}
		out[r.Member][r.Verb] = append(out[r.Member][r.Verb], r)
	}
	for m := range out {
		for v := range out[m] {
			slices.SortStableFunc(out[m][v], func(a, b RefusedVerb) int { return b.At.Compare(a.At) })
		}
	}
	return out
}

// MemberBeatFault is the member-beat check's one FAIL line for a member whose beats the
// server refused RefusedBeatBound or more times in RefusedWindow: the count, the refusal of
// the last refused beat and what it sent, with the remedy. It is named before and independent
// of the member-down stop ("DOCTOR FAIL member-beat ...").
func MemberBeatFault(member string, recs []RefusedVerb) string {
	last := recs[0]
	return fmt.Sprintf("member-beat %s: %d beats refused in %s, last: %s, argv: %s remedy=%s",
		member, len(recs), RefusedWindow, last.Why, last.Argv, MemberBeatRemedy)
}

// MemberBeatRemedy is the member-beat fault's remedy: the fix card once the forms agree, else
// the adopt of a build whose beat the server accepts.
const MemberBeatRemedy = "adopt a nova-sprint whose fleet beat the server accepts (nova-sprint adopt), or fix the caller's beat form"

// RefusedVerbFault is the refused-verbs check's one FAIL line for a member and verb word
// refused RefusedVerbBound or more times in RefusedWindow: the count and the refusal of the
// last, for every verb and not only beat.
func RefusedVerbFault(member, verb string, recs []RefusedVerb) string {
	last := recs[0]
	return fmt.Sprintf("refused-verbs %s: %s x %d in %s, last: %s",
		member, verb, len(recs), RefusedWindow, last.Why)
}

// RefusedBeatLines is every member-beat FAIL line for the refused beats in the window, by
// member, each at RefusedBeatBound or more; a member under the bound is no line. The lines
// are sorted by member so the same record is always the same words.
func RefusedBeatLines(recs []RefusedVerb, now time.Time) []string {
	beats := RefusedBeats(recs, now)
	members := make([]string, 0, len(beats))
	for m, rs := range beats {
		if len(rs) >= RefusedBeatBound {
			members = append(members, m)
		}
	}
	slices.Sort(members)
	var out []string
	for _, m := range members {
		out = append(out, MemberBeatFault(m, beats[m]))
	}
	return out
}

// RefusedVerbLines is every refused-verbs FAIL line for the refused verbs in the window, by
// member then verb, each at RefusedVerbBound or more; a pair under the bound is no line. The
// lines are sorted by member then verb.
func RefusedVerbLines(recs []RefusedVerb, now time.Time) []string {
	groups := RefusedVerbGroups(recs, now)
	members := slices.Sorted(maps.Keys(groups))
	var out []string
	for _, m := range members {
		verbs := slices.Sorted(maps.Keys(groups[m]))
		for _, v := range verbs {
			if len(groups[m][v]) >= RefusedVerbBound {
				out = append(out, RefusedVerbFault(m, v, groups[m][v]))
			}
		}
	}
	return out
}

// RefusedBeatOK is a member's one OK line when none of its beats were refused in the window.
func RefusedBeatOK(member string) string {
	return fmt.Sprintf("DOCTOR OK member-beat %s", member)
}
