package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEveryAckRefusalHasAReason drives every refusal Ack (steps_ack.go) can
// return and asserts each carries a non-empty reason: a failed step prints
// one REFUSED line per refusal, and a refusal with no reason prints as a
// defect (cmd/nova-sprint, refusalLines). The silence refusal's exact words
// are pinned by TestStepsAckCoverSilenceRefusal.
func TestEveryAckRefusalHasAReason(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		plan func(w *world) Plan
		want string
	}{
		{
			name: "not the coordinator",
			plan: func(w *world) Plan {
				return Ack(w.s, AckReq{Notes: []string{"j1"}, Reason: "seen", Who: "m1"})
			},
			want: "coordinator's alone",
		},
		{
			name: "named twice",
			plan: func(w *world) Plan {
				return Ack(w.s, AckReq{Notes: []string{"j1", "j1"}, Reason: "seen", Who: w.s.Coordinator})
			},
			want: "named twice",
		},
		{
			name: "no open judgment",
			plan: func(w *world) Plan {
				return Ack(w.s, AckReq{Notes: []string{"j-missing"}, Reason: "seen", Who: w.s.Coordinator})
			},
			want: "no open judgment",
		},
		{
			name: "the seat's silence clears only by its answers",
			plan: func(w *world) Plan {
				n := Note{ID: "j-seat", Kind: Judgment, Type: NSeatSilent, Stream: "s1", Decisions: []string{"ack"}}
				w.s.Open = append(w.s.Open, Open{Key: OpenKey(n.ID, "s1-1"), Note: n})
				return Ack(w.s, AckReq{Notes: []string{n.ID}, Reason: "seen", Who: w.s.Coordinator})
			},
			want: "cannot be acknowledged",
		},
		{
			name: "ack does not answer the judgment",
			plan: func(w *world) Plan {
				n := Note{ID: "j-drop", Kind: Judgment, Type: "a judgment", Stream: "s1", Decisions: []string{"drop"}, Primaries: []string{"s1-1"}}
				w.s.Open = append(w.s.Open, Open{Key: OpenKey(n.ID, "s1-1"), Note: n})
				return Ack(w.s, AckReq{Notes: []string{n.ID}, Reason: "seen", Who: w.s.Coordinator})
			},
			want: "ack does not answer",
		},
		{
			name: "the landing-stalled judgment offers no ack",
			plan: func(w *world) Plan {
				n := Note{ID: "j-stall", Kind: Judgment, Type: NLandingStall, SprintLevel: true}
				w.s.Open = append(w.s.Open, Open{Key: OpenKey(n.ID, ""), Note: n})
				return Ack(w.s, AckReq{Notes: []string{n.ID}, Reason: "seen", Who: w.s.Coordinator})
			},
			want: "landing stalled",
		},
		{
			name: "the stream is stopped",
			plan: func(w *world) Plan {
				w.s.StreamCtl("s1").Fields["state"] = StreamStopped
				n := Note{ID: "j-stop", Kind: Judgment, Type: "a judgment", Stream: "s1", StreamLevel: true, Decisions: []string{"ack"}}
				w.s.Open = append(w.s.Open, Open{Key: OpenKey(n.ID, StreamSubject("s1")), Note: n})
				return Ack(w.s, AckReq{Notes: []string{n.ID}, Reason: "seen", Who: w.s.Coordinator})
			},
			want: "is stopped",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := setup(t, 1)
			p := tc.plan(w)
			require.NotEmpty(t, p.Refused, "the ack is refused")
			joined := ""
			for _, r := range p.Refused {
				assert.NotEmpty(t, r.Why, "refusal %s carries a reason", r.Key)
				joined += r.Why + "\n"
			}
			assert.Contains(t, joined, tc.want)
		})
	}
}
