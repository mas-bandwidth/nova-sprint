package steps

import (
	"fmt"
	"strings"
)

// Admission at the base as it is now (2026-11-08: 45 waiting cards failed
// admission after their stream's base moved, their PATHS gone, and only
// check run by hand showed it). Layer 6 re-proves each stream's waiting and
// ready producers with check's admission whenever the stream's base moves
// and passes the refused ones in Inputs.Unadmitted. Such a producer is never
// dealt (cands, preempt; undealt does not count it), and each stream with
// one has a single admission judgement naming its cards and the first
// refusal, whose fix is in it; it clears when none fails or waits any more
// (tla/Steps.tla adm: NeverDealtUnadmitted, AdmissionShown).

// admitted says whether producer p passed its stream's last re-proof.
func (t *tick) admitted(p string) bool { _, no := t.in.Unadmitted[p]; return !no }

// unadmittedIn is stream s's waiting and ready producers that failed the
// re-proof, by id.
func (t *tick) unadmittedIn(s string) []string {
	var out []string
	for _, id := range sortedKeys(t.in.Unadmitted) {
		if c, ok := t.v.Cards[id]; ok && c.Kind == "producer" && c.Stream == s && (c.State == "waiting" || c.State == "ready") {
			out = append(out, id)
		}
	}
	return out
}

// unadmitted opens the admission judgement of each stream with a waiting or
// ready producer that failed the re-proof.
func (t *tick) unadmitted() {
	streams := set{}
	for id := range t.in.Unadmitted {
		if c, ok := t.v.Cards[id]; ok {
			streams[c.Stream] = struct{}{}
		}
	}
	for _, s := range sortedKeys(streams) {
		if ids := t.unadmittedIn(s); len(ids) > 0 {
			t.judge("admission", s, fmt.Sprintf("stream %s: %d waiting or ready card(s) fail admission at its base as it is now and are not dealt (%s); the first: %s",
				s, len(ids), firstIDs(ids), t.in.Unadmitted[ids[0]]))
		}
	}
}

// firstIDs is up to five ids, then how many more.
func firstIDs(ids []string) string {
	if len(ids) <= 5 {
		return strings.Join(ids, ", ")
	}
	return strings.Join(ids[:5], ", ") + fmt.Sprintf(" and %d more", len(ids)-5)
}
