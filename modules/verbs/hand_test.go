package verbs

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
)

// TestClosingARowReturnsItsHand: every verb that makes a row held or
// unhooked returns its whole hand to the queue in the same op, taken and
// untaken, branches kept (Glenn, 2026-11-10: "The cards should be
// redistributed when anybody goes to held or down automatically" ...
// "Aurora is down, but she still has cards. This is forbidden.").
func TestClosingARowReturnsItsHand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, as, why string
		args          []string
		row, c, p     string
		pState        string
	}{
		{"the coordinator unhooks a row", "carol", "unhooked", []string{"unhook", "alice"}, "alice", "p1.w1", "p1", "ready"},
		{"a row unhooks itself", "bob", "unhooked", []string{"unhook"}, "bob", "p2.r1", "p2", "review"},
		{"the coordinator holds a row", "carol", "held", []string{"hold", "alice", "--reason", "rest"}, "alice", "p1.w1", "p1", "ready"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			if r := w.do(tc.as, tc.args...); r.Exit != 0 {
				t.Fatalf("exit %d: %s", r.Exit, r.Output)
			}
			s := w.snap(t)
			c := s.Cards[tc.c]
			if c.State != "fail" || c.Result != "" || c.Reason != card.ReleasedPrefix+card.Returned(tc.row, tc.why) {
				t.Fatalf("%s %+v, want returned from %s", tc.c, c, tc.row)
			}
			if p := s.Cards[tc.p]; p.State != tc.pState || p.Branch == "" {
				t.Fatalf("%s %+v, want %s with its branch", tc.p, p, tc.pState)
			}
		})
	}
}
