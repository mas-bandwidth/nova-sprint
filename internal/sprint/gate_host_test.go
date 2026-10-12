package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/card"
)

// gateHostFleet is a two-member fleet whose control cards carry the given facts, one
// per member, in the order given.
func gateHostFleet(members ...struct {
	name  string
	facts string
}) *Snapshot {
	f := NewTable(Fleet)
	var rows []string
	for _, m := range members {
		rows = append(rows, m.name)
	}
	f.SetRows(rows)
	for _, m := range members {
		f.Put(&Card{ID: CtlID(m.name), Row: m.name, Col: Ctl, Fields: map[string]string{"kind": "member", "status": Up, FieldFacts: m.facts}})
	}
	return &Snapshot{Now: t0, Fleet: f}
}

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
	assert.Empty(t, card.Checks("c", "CARD: c\nSTEP: build\nTASK\nbuild the card\n", card.Options{}))
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

// TestATlaCardIsDealtOnlyToAMemberWhoseFactsCarryTheTlaJar: a card needing tla is dealt
// only to the member whose facts carry the TLA jar.
func TestATlaCardIsDealtOnlyToAMemberWhoseFactsCarryTheTlaJar(t *testing.T) {
	s := gateHostFleet(
		struct {
			name  string
			facts string
		}{"m-linux", "linux,go"},
		struct {
			name  string
			facts string
		}{"m-tla", "linux,tla,go"},
	)
	c := &Card{ID: "s1-1", Fields: map[string]string{"brief": "CARD: c\nGATE-NEEDS: linux, tla\n"}}
	pool, why := s.GateHostPool(c, []string{"m-linux", "m-tla"})
	require.Empty(t, why)
	assert.Equal(t, []string{"m-tla"}, pool, "a card needing tla is dealt only to a member whose facts carry the TLA jar")
}

// TestACardNoMemberSatisfiesNamesTheNeed: a card no member satisfies yields one why that
// names the need, the sentence the tick's judgment carries.
func TestACardNoMemberSatisfiesNamesTheNeed(t *testing.T) {
	s := gateHostFleet(
		struct {
			name  string
			facts string
		}{"m-linux", "linux"},
		struct {
			name  string
			facts string
		}{"m-darwin", "darwin"},
	)
	c := &Card{ID: "s1-1", Fields: map[string]string{"brief": "CARD: c\nGATE-NEEDS: tla\n"}}
	pool, why := s.GateHostPool(c, []string{"m-linux", "m-darwin"})
	assert.Empty(t, pool)
	assert.Contains(t, why, "GATE-NEEDS tla", "the judgment names the need")
	assert.Contains(t, why, "m-linux", "the judgment names the members it looked at")
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
