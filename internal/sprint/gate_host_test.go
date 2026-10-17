package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/card"
)

// TestGateNeedsParsing reads GATE-NEEDS: from the header, comma-separated and trimmed,
// with "none" and "-" dropped.
func TestGateNeedsParsing(t *testing.T) {
	tests := []struct {
		name  string
		brief string
		want  []string
	}{
		{"single need", "CARD: c\nGATE-NEEDS: linux\n", []string{"linux"}},
		{"multiple needs", "CARD: c\nGATE-NEEDS: linux, tla, landlock\n", []string{"linux", "tla", "landlock"}},
		{"the none word", "CARD: c\nGATE-NEEDS: none\n", nil},
		{"absent", "CARD: c\n", nil},
		{"empty value", "CARD: c\nGATE-NEEDS:\n", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, GateNeeds(tc.brief))
		})
	}
}

// TestGateHostLintRefusesHostInstructionsInAStep: the card lint (card.Checks) refuses a
// STEP that tells the worker to ssh to a named host or run the gate "on <host>", with a
// remedy naming GATE-NEEDS; a STEP with no host instruction passes.
func TestGateHostLintRefusesHostInstructionsInAStep(t *testing.T) {
	for _, line := range []string{"STEP: ssh hetzner2", "STEP: run the gate on hetzner2"} {
		findings := card.Checks("c", "CARD: c\n"+line+"\nTASK\nrun the gate\n", card.Options{})
		var got []string
		for _, f := range findings {
			if f.Check == card.GateHostCheck {
				got = append(got, f.Excerpt)
			}
		}
		require.NotEmpty(t, got, line)
		assert.Contains(t, got[0], card.GateHostRemedy, line)
	}
	// A STEP that names no host — "on the member", an ordinary prose "on" — passes:
	// only a host-like token (a fleet host name, or a digit/hyphen token that is not a
	// stop word) is refused.
	for _, line := range []string{"STEP: build", "STEP: run go test -count=1 ./internal/sprint/... on the member this card was dealt to, and read the last line"} {
		for _, f := range card.Checks("c", "CARD: c\n"+line+"\nTASK\nrun the gate\n", card.Options{}) {
			assert.NotEqual(t, card.GateHostCheck, f.Check, line)
		}
	}
}

// TestAGateNeedsLinuxCardIsNeverDealtToADarwinMember: the deal gives a GATE-NEEDS linux
// card only to the member whose facts carry linux, never the darwin member, even though
// the deal's round starts at the darwin member.
func TestAGateNeedsLinuxCardIsNeverDealtToADarwinMember(t *testing.T) {
	w := fleetWorld(t, 1, 0, "m-darwin", "m-linux")
	w.s.MemberCtl("m-darwin").Fields[FieldFacts] = "darwin,go"
	w.s.MemberCtl("m-linux").Fields[FieldFacts] = "linux,go"
	w.s.Work.Card("s1-1").Fields["brief"] = "tier: pro\nGATE-NEEDS: linux\n\nThe task.\n"
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc := w.s.Fleet.Card("s1-1.w1")
	require.NotNil(t, wc, "the GATE-NEEDS linux card is dealt")
	assert.Equal(t, "m-linux", wc.Row, "a GATE-NEEDS linux card is never dealt to a darwin member")
}

// GateHostPool: a card needing tla is dealt only to the member whose facts carry the TLA
// jar; a card no member satisfies yields one why that names the need and the members looked
// at, the sentence the tick's judgment carries.
func TestGateHostPool(t *testing.T) {
	for _, tc := range []struct {
		name, need   string
		facts        [2]string
		pool, whyHas []string
		msg          string
	}{
		{"ATlaCardIsDealtOnlyToAMemberWhoseFactsCarryTheTlaJar", "linux, tla", [2]string{"linux,go", "linux,tla,go"},
			[]string{"m-b"}, nil, "a card needing tla is dealt only to a member whose facts carry the TLA jar"},
		{"ACardNoMemberSatisfiesNamesTheNeed", "tla", [2]string{"linux", "darwin"},
			nil, []string{"GATE-NEEDS tla", "m-a"}, "the judgment names the need and the members it looked at"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := NewTable(Fleet)
			f.SetRows([]string{"m-a", "m-b"})
			for i, m := range []string{"m-a", "m-b"} {
				f.Put(&Card{ID: CtlID(m), Row: m, Col: Ctl, Fields: map[string]string{"kind": "member", "status": Up, FieldFacts: tc.facts[i]}})
			}
			s := &Snapshot{Now: t0, Fleet: f}
			c := &Card{ID: "s1-1", Fields: map[string]string{"brief": "CARD: c\nGATE-NEEDS: " + tc.need + "\n"}}
			pool, why := s.GateHostPool(c, []string{"m-a", "m-b"})
			assert.Equal(t, tc.pool, pool, tc.msg)
			if tc.whyHas == nil {
				assert.Empty(t, why)
			}
			for _, w := range tc.whyHas {
				assert.Contains(t, why, w, tc.msg)
			}
		})
	}
}

// TestACardNoMemberSatisfiesRaisesOneJudgment: the tick raises one NGateNeeds judgment
// naming the need for a card no member up satisfies, never a silent wait.
func TestACardNoMemberSatisfiesRaisesOneJudgment(t *testing.T) {
	w := fleetWorld(t, 1, 2, "m-darwin", "m-linux")
	w.s.MemberCtl("m-darwin").Fields[FieldFacts] = "darwin,go"
	w.s.MemberCtl("m-linux").Fields[FieldFacts] = "linux,go"
	w.s.Work.Card("s1-1").Fields["brief"] = "tier: pro\nGATE-NEEDS: tla\n\nThe task.\n"
	p, _ := TickDeal(w.s, TickReq{})
	w.must(p)
	assert.Equal(t, Ready, w.state("s1-1"), "the unsatisfiable card is never dealt")
	notes := w.notesOf(NGateNeeds)
	require.Len(t, notes, 1, "one judgment naming the need: %+v", w.notes)
	assert.Contains(t, notes[0].What, "GATE-NEEDS tla")
	assert.Equal(t, []string{"s1-1"}, notes[0].Primaries)
}

// TestAGateJudgmentClosesInTheTickItsCauseEnds: 2026-10-17, a server restart left only
// the studio up for a tick; the deal raised "no member up satisfies a card's GATE-NEEDS"
// for four linux cards, and it held them and 25 behind them for 35 minutes after the
// Linux members were back, raised again every 10 minutes, answerable by no verb. The
// judgment is a cond of the tick's notify now: raised while no member of the deal's pool
// carries the need, closed in the tick one does, and the card dealt in that tick.
// tla/JudgmentRetire.tla GateClosedAfterTick, reversed witness gate_stays.
func TestAGateJudgmentClosesInTheTickItsCauseEnds(t *testing.T) {
	w := fleetWorld(t, 1, 2, "m-darwin", "m-linux")
	w.s.MemberCtl("m-darwin").Fields[FieldFacts] = "darwin,go"
	w.s.MemberCtl("m-linux").Fields[FieldFacts] = "linux,go"
	w.s.Work.Card("s1-1").Fields["brief"] = "tier: pro\nGATE-NEEDS: linux\n\nThe task.\n"
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m-linux"}))
	w.part(TickDeal, TickReq{})
	require.Len(t, w.openOn("s1-1"), 1, "only the darwin member is up: one judgment names the need")
	assert.Equal(t, NGateNeeds, w.openOn("s1-1")[0].Note.Type)
	w.part(TickDeal, TickReq{})
	require.Len(t, w.notesOf(NGateNeeds), 1, "a standing cause is judged once, never every tick")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m-linux", Width: 2}))
	w.part(TickDeal, TickReq{})
	assert.Empty(t, w.openOn("s1-1"), "the linux member is back: the judgment closes in that tick")
	wc := w.s.Fleet.Card("s1-1.w1")
	require.NotNil(t, wc, "and the card is dealt in it")
	assert.Equal(t, "m-linux", wc.Row)
}

// TestGateNeedsWhyNamesTheNeedNoMemberCarries: f3dl-perf-scalar-oracle-regen needed linux,
// x86-64, and its judgment read "no member up satisfies GATE-NEEDS linux" beside nine
// members whose facts carry linux; it names the need none carries (x86-64), or every need
// together when each is carried by some member and none carries all.
func TestGateNeedsWhyNamesTheNeedNoMemberCarries(t *testing.T) {
	f := NewTable(Fleet)
	f.SetRows([]string{"m-a", "m-b"})
	f.Put(&Card{ID: CtlID("m-a"), Row: "m-a", Col: Ctl, Fields: map[string]string{"kind": "member", "status": Up, FieldFacts: "linux,amd64,tla"}})
	f.Put(&Card{ID: CtlID("m-b"), Row: "m-b", Col: Ctl, Fields: map[string]string{"kind": "member", "status": Up, FieldFacts: "darwin,arm64"}})
	s := &Snapshot{Now: t0, Fleet: f}
	_, why := s.GateHostPool(&Card{ID: "c", Fields: map[string]string{"brief": "CARD: c\nGATE-NEEDS: linux, x86-64\n"}}, []string{"m-a", "m-b"})
	assert.Contains(t, why, "GATE-NEEDS x86-64 (", "the need no member carries")
	_, why = s.GateHostPool(&Card{ID: "c", Fields: map[string]string{"brief": "CARD: c\nGATE-NEEDS: tla, arm64\n"}}, []string{"m-a", "m-b"})
	assert.Contains(t, why, "GATE-NEEDS tla, arm64 together (", "each need carried by some member, all by none")
}
