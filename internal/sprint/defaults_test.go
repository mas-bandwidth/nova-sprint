package sprint

import "testing"

// The working default (docs/SPEC-SPRINT.md section 11, the settings; the owner,
// 2026-10-11: "Defaults should always be the sprint machine works"; "The person
// installing nova-sprint for the first time must get smart defaults that WORK"):
// an absent read_cards property, and ReadTierDefault, are on; only ReadCardsOffWord
// degrades the machine (reads asked one per tick through the readers table).
func TestReadCardsDefaultsOn(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	if !w.s.ReadCardsOn() {
		t.Fatal("an absent read_cards property must be on: the working machine")
	}
	w.s.Work.SetProp(PropReadCards, ReadTierDefault)
	if !w.s.ReadCardsOn() {
		t.Fatal("read_cards default must be on: the working machine")
	}
	w.s.Work.SetProp(PropReadCards, ReadCardsOffWord)
	if w.s.ReadCardsOn() {
		t.Fatal("read_cards off must be off, and is the one word that degrades the machine")
	}
}
