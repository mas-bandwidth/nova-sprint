package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Run the registered production tick start, rather than call a test-only
// withdrawal. These witnesses fail before automatic Down recovery is wired.
func recoveryStart(w *world, seats []FriendSeat, heads ...RecoveryHead) []string {
	w.s.Friends = seats
	var moves []string
	for _, part := range TickStart {
		p, _ := part.Fn(w.s, TickReq{Friends: seats, RecoveryHeads: heads})
		p = Applied(w.s, p)
		for _, u := range p.Units {
			moves = append(moves, u.Moved)
		}
		w.must(p)
	}
	return moves
}

func TestDownFriendRevokesReadyAndBegunEvenWithoutAReceiver(t *testing.T) {
	t.Parallel()
	w := takeWorld(t)
	before := gensOf(w.s, "s1-1.w1")
	seats := []FriendSeat{{Name: "amy", Width: 1, Status: Down, Running: []string{"s1-1.w1"}}, {Name: "bob", Width: 1, Status: Down}}
	moves := recoveryStart(w, seats)
	for _, id := range []string{"s1-1.w1", "s1-2.w1"} {
		c := w.s.Fleet.Card(id)
		require.Equal(t, Withdrawn, c.Col, "ready-only recovery must not leave begun work owned")
		assert.Equal(t, 2, c.Int("gen"))
		assert.Equal(t, Ready, w.s.StateOf(c.F("primary")))
		assert.Equal(t, 1, w.s.Primary(c.F("primary")).Int("attempt"))
		assert.Empty(t, c.F(FieldTakeEnded), "recovery spends no bound")
		assert.NotEmpty(t, c.F(FieldRecoveryPending), "missing head evidence remains durable debt")
	}
	require.NotEmpty(t, moves)
	assert.Empty(t, recoveryStart(w, seats), "repeated tick does not revoke or emit moves twice")
	late := Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, As: FriendRow("amy"), Gens: before, Head: strings.Repeat("a", 40)})
	require.NotEmpty(t, late.Refused, "the old generation cannot finish after logical revocation")
	assert.Empty(t, late.Units)
}

func TestDownFriendCarriesExactHeadToDistinctGeneration(t *testing.T) {
	t.Parallel()
	w := takeWorld(t)
	old := w.s.Fleet.Card("s1-1.w1")
	branch := BranchOf(w.s.Prefix, w.s.Epoch, old.ID, old.Int("gen"))
	repo := "https://github.com/mas-bandwidth/nova-tools.git"
	// Use the same production brief parsing as the source branch identity.
	w.s.Work.Card(old.F("primary")).Fields["brief"] = "Task.\nREPO: " + repo + "\nWHO: friend\n\nTask."
	seats := []FriendSeat{{Name: "amy", Width: 1, Status: Down}, {Name: "bob", Width: 1, Status: Up, Class: "flash"}}
	request := RecoveryHeadRequest{Card: old.ID, Gen: old.Int("gen"), CurrentGen: old.Int("gen") + 1, Epoch: w.s.Epoch, Repo: repo, Branch: branch}
	recoveryStart(w, seats, RecoveryHead{RecoveryHeadRequest: request, Error: "remote unavailable"})
	c := w.s.Fleet.Card(old.ID)
	require.NotEmpty(t, c.F(FieldRecoveryPending))
	dealStarted(w, seats...)
	require.Equal(t, Withdrawn, w.s.Fleet.Card(old.ID).Col, "unknown head cannot be redealt")
	head := strings.Repeat("b", 40)
	recoveryStart(w, seats, RecoveryHead{RecoveryHeadRequest: request, Head: head})
	c = w.s.Fleet.Card(old.ID)
	require.Empty(t, c.F(FieldRecoveryPending))
	assert.Equal(t, head, c.F(FieldCarryHead))
	assert.Equal(t, "1", c.F(FieldCarryGen))
	dealStarted(w, seats...)
	c = w.s.Fleet.Card(old.ID)
	require.Equal(t, FriendRow("bob"), c.Row)
	assert.Equal(t, 3, c.Int("gen"))
	assert.Equal(t, branch, c.F(FieldRecoveryBranch), "old branch remains discoverable")
	assert.NotEqual(t, branch, BranchOf(w.s.Prefix, w.s.Epoch, c.ID, c.Int("gen")))
	assert.Equal(t, head, c.F(FieldCarryHead), "new work starts from captured immutable commit")
	packet := PacketOf(w.s.Prefix, w.s.Epoch, c, w.s.Work.Card(c.F("primary")), nil, nil)
	assert.Equal(t, head, packet.BaseHead, "preserved work must be delivered, not merely recorded")
	assert.Equal(t, 1, packet.BaseAttempt)
	assert.Equal(t, branch, packet.Base)
	assert.NotEqual(t, packet.Base, packet.Branch)
}

func TestRecoveryDebtRejectsMalformedMismatchedAndManualAdmissions(t *testing.T) {
	t.Parallel()
	w := takeWorld(t)
	id := "s1-1.w1"
	seats := []FriendSeat{{Name: "amy", Width: 1, Status: Down}, {Name: "bob", Width: 1, Status: Up, Class: "flash"}}
	recoveryStart(w, seats)
	c := w.s.Fleet.Card(id)
	require.Equal(t, Withdrawn, c.Col)
	r := RecoveryHeadRequest{Card: id, Gen: c.Int(FieldRecoveryGen), CurrentGen: c.Int("gen"), Epoch: w.s.Epoch, Repo: c.F(FieldRecoveryRepo), Branch: c.F(FieldRecoveryBranch)}
	wrong := r
	wrong.Gen++
	for _, result := range []RecoveryHead{
		{RecoveryHeadRequest: wrong, Head: strings.Repeat("a", 40)},
		{RecoveryHeadRequest: r, Head: "not-a-sha"},
		{RecoveryHeadRequest: r, Error: "timeout"},
	} {
		recoveryStart(w, seats, result)
		require.NotEmpty(t, w.s.Fleet.Card(id).F(FieldRecoveryPending))
	}
	give := Applied(w.s, FriendGive(w.s, FriendGiveReq{Friend: "bob", IDs: []string{"s1-1"}}))
	assert.Empty(t, give.Units, "manual give cannot bypass preservation debt")
	require.NotEmpty(t, give.Refused)
}

func TestPinnedRecoveredFriendMayTakeAfterSuccessfulEmptyHeadRead(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, friendBrief("only friend amy"))
	dealStarted(w, FriendSeat{Name: "amy", Width: 1, Status: Up, Class: "flash"})
	id := "s1-1.w1"
	recoveryStart(w, []FriendSeat{{Name: "amy", Width: 1, Status: Down}, {Name: "bob", Width: 1, Status: Up, Class: "flash"}})
	c := w.s.Fleet.Card(id)
	require.Equal(t, Withdrawn, c.Col)
	r := RecoveryHeadRequest{Card: id, Gen: c.Int(FieldRecoveryGen), CurrentGen: c.Int("gen"), Epoch: w.s.Epoch, Repo: c.F(FieldRecoveryRepo), Branch: c.F(FieldRecoveryBranch)}
	recoveryStart(w, []FriendSeat{{Name: "amy", Width: 1, Status: Down}}, RecoveryHead{RecoveryHeadRequest: r})
	dealStarted(w, FriendSeat{Name: "amy", Width: 1, Status: Down}, FriendSeat{Name: "bob", Width: 1, Status: Up, Class: "flash"})
	require.Equal(t, Withdrawn, w.s.Fleet.Card(id).Col, "a pin is not silently rewritten")
	require.Equal(t, Ready, w.s.StateOf("s1-1"))
	dealStarted(w, FriendSeat{Name: "amy", Width: 1, Status: Up, Class: "flash"})
	require.Equal(t, FriendRow("amy"), w.s.Fleet.Card(id).Row)
	require.NotEqual(t, Withdrawn, w.s.Fleet.Card(id).Col, "automatic Down recovery is not a permanent ban")
}

func TestDownFriendReturnsReaderWorkWithoutAnotherReader(t *testing.T) {
	t.Parallel()
	for _, col := range []string{Asked, Reading} {
		t.Run(col, func(t *testing.T) {
			reader := ReaderPrefix + "amy"
			w := newWorld(t, reader)
			w.readersTable()
			putReview(w, "s1-1", "Task.", 1, 1, "head")
			id := ReadCardID("s1-1", 1, reader)
			w.s.Readers.Put(&Card{ID: id, Row: reader, Col: col, Rev: 1, Fields: map[string]string{"kind": "read", "primary": "s1-1", "stream": "s1", "attempt": "1", "gen": "1", "head": "head"}})
			recoveryStart(w, []FriendSeat{{Name: "amy", Width: 1, Status: Down}})
			rc := w.s.Readers.Card(id)
			require.False(t, rc.Placed(), "a down reader is revoked even with no receiver")
			assert.Equal(t, RetiredByAway, rc.F("retired_by"))
			assert.Greater(t, rc.Int("gen"), 1)
			assert.Equal(t, Review, w.s.StateOf("s1-1"), "review obligation remains pending")
			late := Read(w.s, ReadReq{Sel: Sel{IDs: []string{id}}, As: reader, Gens: map[string]int{id: 1}, Verdict: "ok"})
			assert.Empty(t, late.Units)
			require.NotEmpty(t, late.Refused)
		})
	}
}

func TestDownFriendRevokesFleetReadWithoutHeadDebt(t *testing.T) {
	t.Parallel()
	w := readCardsWorld(t, 1)
	putReviewBy(w, "s1-1", "Task.", "m1", 1)
	dealReads(t, w, []FriendSeat{{Name: "amy", Width: 1, Status: Up, Class: "flash", Roles: []string{RoleReader}}})
	reads := readCardsOf(w, "s1-1")
	require.Len(t, reads, 1)
	id := reads[0].ID
	oldgen := max(reads[0].Int("gen"), 1)
	recoveryStart(w, []FriendSeat{{Name: "amy", Width: 1, Status: Down, Roles: []string{RoleReader}}})
	rc := w.s.Fleet.Card(id)
	require.Equal(t, Withdrawn, rc.Col)
	assert.Equal(t, RetiredByAway, rc.F("retired_by"))
	assert.Empty(t, rc.F(FieldRecoveryPending), "read ownership needs no remote work-head query")
	assert.Greater(t, rc.Int("gen"), oldgen)
	assert.Equal(t, Review, w.s.StateOf("s1-1"))
	late := Read(w.s, ReadReq{Sel: Sel{IDs: []string{id}}, As: FriendRow("amy"), Gens: map[string]int{id: oldgen}, Verdict: "ok"})
	require.NotEmpty(t, late.Refused)
	assert.Empty(t, late.Units)
}

func TestRepeatedRecoveryRetainsBaselineWhenNewBranchHasNoPush(t *testing.T) {
	t.Parallel()
	w := takeWorld(t)
	id := "s1-1.w1"
	down := []FriendSeat{{Name: "amy", Width: 1, Status: Down}, {Name: "bob", Width: 1, Status: Up, Class: "flash"}}
	recoveryStart(w, down)
	requestOf := func() RecoveryHeadRequest {
		c := w.s.Fleet.Card(id)
		return RecoveryHeadRequest{Card: id, Gen: c.Int(FieldRecoveryGen), CurrentGen: c.Int("gen"), Epoch: w.s.Epoch, Repo: c.F(FieldRecoveryRepo), Branch: c.F(FieldRecoveryBranch)}
	}
	head := strings.Repeat("c", 40)
	first := requestOf()
	recoveryStart(w, down, RecoveryHead{RecoveryHeadRequest: first, Head: head})
	dealStarted(w, down...)
	require.Equal(t, FriendRow("bob"), w.s.Fleet.Card(id).Row)
	require.Equal(t, 3, w.s.Fleet.Card(id).Int("gen"))
	allDown := []FriendSeat{{Name: "amy", Width: 1, Status: Down}, {Name: "bob", Width: 1, Status: Down}}
	recoveryStart(w, allDown)
	second := requestOf()
	require.Equal(t, 3, second.Gen)
	recoveryStart(w, allDown, RecoveryHead{RecoveryHeadRequest: second})
	c := w.s.Fleet.Card(id)
	require.Empty(t, c.F(FieldRecoveryPending))
	assert.Equal(t, head, c.F(FieldCarryHead), "successful absence never destroys an accepted baseline")
	assert.Equal(t, "1", c.F(FieldCarryGen))
	packet := PacketOf(w.s.Prefix, w.s.Epoch, c, w.s.Work.Card(c.F("primary")), nil, nil)
	assert.Equal(t, head, packet.BaseHead)
	assert.Equal(t, first.Branch, BranchOf(w.s.Prefix, w.s.Epoch, c.ID, c.Int(FieldCarryGen)), "baseline source branch remains discoverable")
}

func TestLegacyUnknownGenerationDoesNotInferAnEmptyNewerBranch(t *testing.T) {
	t.Parallel()
	w := takeWorld(t)
	id := "s1-1.w1"
	w.s.Fleet.Card(id).Fields["gen"] = "0"
	seats := []FriendSeat{{Name: "amy", Width: 1, Status: Down}}
	recoveryStart(w, seats)
	c := w.s.Fleet.Card(id)
	assert.Equal(t, 1, c.Int("gen"), "unknown original generation is revoked without guessing its branch")
	branch := BranchOf(w.s.Prefix, w.s.Epoch, id, 0)
	assert.Equal(t, branch, c.F(FieldRecoveryBranch), "never substitute a branch not actually issued")
	r := RecoveryHeadRequest{Card: id, Gen: 0, CurrentGen: 1, Epoch: w.s.Epoch, Repo: c.F(FieldRecoveryRepo), Branch: branch}
	recoveryStart(w, seats, RecoveryHead{RecoveryHeadRequest: r})
	assert.NotEmpty(t, w.s.Fleet.Card(id).F(FieldRecoveryPending), "incomplete original identity remains visible debt")
}

func TestUnreadableRecoveryBatchDoesNotStarveLaterDebt(t *testing.T) {
	t.Parallel()
	briefs := make([]string, RecoveryHeadBatch+1)
	for i := range briefs {
		briefs[i] = friendBrief("friend")
	}
	w := friendWorld(t, briefs...)
	dealStarted(w, FriendSeat{Name: "amy", Width: len(briefs), Status: Up, Class: "flash"})
	seats := []FriendSeat{{Name: "amy", Width: len(briefs), Status: Down}}
	recoveryStart(w, seats)
	first := RecoveryHeadRequests(w.s)
	require.Len(t, first, RecoveryHeadBatch)
	blocked := map[string]bool{}
	for _, r := range first {
		blocked[r.Card] = true
	}
	// These errors leave the first batch pending; the next tick must still
	// reach the one later source whose head can genuinely be read.
	var failures []RecoveryHead
	for _, r := range first {
		failures = append(failures, RecoveryHead{RecoveryHeadRequest: r, Error: "unreachable"})
	}
	p, _ := TickRecoveryHeads(w.s, TickReq{RecoveryHeads: failures})
	w.must(Applied(w.s, p))
	second := RecoveryHeadRequests(w.s)
	var reachable RecoveryHeadRequest
	for _, r := range second {
		if !blocked[r.Card] {
			reachable = r
			break
		}
	}
	require.NotEmpty(t, reachable.Card, "failed first batch cannot starve a later recoverable card")
	p, _ = TickRecoveryHeads(w.s, TickReq{RecoveryHeads: []RecoveryHead{{RecoveryHeadRequest: reachable, Head: strings.Repeat("d", 40)}}})
	w.must(Applied(w.s, p))
	assert.Empty(t, w.s.Fleet.Card(reachable.Card).F(FieldRecoveryPending))
	assert.Equal(t, strings.Repeat("d", 40), w.s.Fleet.Card(reachable.Card).F(FieldCarryHead))
}
