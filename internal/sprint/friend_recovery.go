package sprint

import (
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	"github.com/mas-bandwidth/nova-sprint/pkg/typedrec"
	"slices"
	"strings"
	"time"
)

// RecoveryHeadRequest names the old generation's exact source branch. Recovery
// revokes the lease before reading it; a read cannot authorize an old finish.
// The source remains discoverable after resolution and after the next deal.
type RecoveryHeadRequest struct {
	Card       string
	Gen        int
	CurrentGen int
	Epoch      uint64
	Repo       string
	Branch     string
}

// RecoveryHead is the result of one bounded read of a RecoveryHeadRequest.
// Empty Head is evidence of an absent branch only when Error is empty and
// every identity field agrees. No result, an error or a malformed head leaves
// recovery pending. A full SHA is an immutable snapshot: subsequent old-branch
// pushes remain discoverable there, but are not accepted new-generation work.
type RecoveryHead struct {
	RecoveryHeadRequest
	Head  string
	Error string
}

const (
	FieldRecoveryPending = "recovery_pending"
	FieldRecoveryGen     = "recovery_gen"
	FieldRecoveryRepo    = "recovery_repo"
	FieldRecoveryBranch  = "recovery_branch"
	FieldRecoveredFrom   = "recovered_from"
)

// PartFriendRecover revokes Down leases before the work pump can deal again.
// PartRecoveryHeads resolves their remote-head debt separately, after revocation.
const (
	PartFriendRecover      = "friend-recover"
	PartRecoveryHeads      = "recovery-heads"
	NFriendRecoveryPending = "a recovered friend's branch head is pending"
	PropRecoveryCursor     = "friend_recovery_cursor"
	RecoveryHeadBatch      = 64
	// RecoveryHeadRemoteBudget bounds one repository's head read, and
	// RecoveryHeadBudget bounds the whole heads part of one tick across
	// retries: a remote outage during recovery debt must never hold the
	// tick hostage; the unresolved cards stay pending and the rest of the
	// tick proceeds (the stall is said once, in TickRecoveryHeads).
	RecoveryHeadRemoteBudget = 2 * time.Second
	RecoveryHeadBudget       = 3 * time.Second
)

// TickFriendRecover reserves the concrete transition modelled by DownRecovery
// Plan/Reserve/Apply. Store guards the exact presence keys at reservation;
// withdrawal reserves exact card revision/place and increments its generation.
// A fresh proof after reservation cannot revive that generation. Table writes
// settle under the pending sprint operation before another mutator can begin.
// Revocation increments the generation only: the primary's attempt and the
// card's redeal bound are not spent (asserted by the recovery tests), so a
// friend who recovers is not charged an attempt for having gone down.
func TickFriendRecover(s *Snapshot, r TickReq) (Plan, int) {
	var p Plan
	p.on(s)
	seats := s.Friends
	if seats == nil {
		seats = r.Friends
	}
	for _, f := range seats {
		if f.Status != Down {
			continue
		}
		row := FriendRow(f.Name)
		cards := append(append([]*Card(nil), s.Fleet.Cell(row, Ready)...), s.Fleet.Cell(row, Working)...)
		SortCards(cards)
		for _, c := range cards {
			old := WireGen(c)
			set := map[string]string{"gen": itoa(old + 1), FieldTakenBack: "taken back from down friend " + f.Name, FieldRecoveredFrom: row}
			why := "friend " + f.Name + " is down; its previous generation is revoked"
			if isRead(c) {
				set["retired_by"], set["retired"] = RetiredByAway, stamp(s.Now)
			} else {
				pr := s.Work.Card(c.F("primary"))
				repo := ""
				if pr != nil {
					repo = swarm.ReadCardBase([]byte(pr.F("brief"))).Repo
				}
				set[FieldRecoveryPending], set[FieldRecoveryGen] = "branch head not yet read", itoa(c.Int("gen"))
				set[FieldRecoveryRepo], set[FieldRecoveryBranch] = repo, BranchOf(s.Prefix, s.Epoch, c.ID, c.Int("gen"))
				why += "; awaits the exact old branch's head before any new take"
			}
			u := withdrawUnit(s, c, set, []string{FieldTakenFrom, "first_taken"}, NTakenBack, r.who(), why)
			u.Moved = fmt.Sprintf("%s withdrawn gen=%d; %s", c.ID, old+1, why)
			p.Units = append(p.Units, u)
		}
		// Reader ownership is revoked even when no replacement is available.
		// The primary remains in review; the existing ask owes its next read.
		if s.Readers != nil {
			reader := ReaderPrefix + f.Name
			cards := append(append([]*Card(nil), s.Readers.Cell(reader, Asked)...), s.Readers.Cell(reader, Reading)...)
			SortCards(cards)
			for _, c := range cards {
				set := map[string]string{"gen": itoa(WireGen(c) + 1), "retired": stamp(s.Now), "retired_by": RetiredByAway}
				p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.F("stream"), Changes: []Change{change(Readers, removeEntry(c, set))}, Moved: c.ID + " returned from down friend " + f.Name + "; review remains owed"})
			}
		}
	}
	return p, 0
}

// RecoveryHeadRequests rotates bounded batches so an unreadable repository
// cannot indefinitely starve later debts. The cursor advances even on failure.
func RecoveryHeadRequests(s *Snapshot) []RecoveryHeadRequest {
	var cards []*Card
	for _, c := range s.Fleet.Column(Withdrawn) {
		if !isRead(c) && c.F(FieldRecoveryPending) != "" {
			cards = append(cards, c)
		}
	}
	slices.SortFunc(cards, func(a, b *Card) int { return strings.Compare(a.ID, b.ID) })
	cursor, _ := s.Fleet.Prop(PropRecoveryCursor)
	cut := 0
	for cut < len(cards) && cards[cut].ID <= cursor {
		cut++
	}
	cards = append(slices.Clone(cards[cut:]), cards[:cut]...)
	var out []RecoveryHeadRequest
	for _, c := range cards {
		out = append(out, RecoveryHeadRequest{Card: c.ID, Gen: c.Int(FieldRecoveryGen), CurrentGen: WireGen(c), Epoch: s.Epoch, Repo: c.F(FieldRecoveryRepo), Branch: c.F(FieldRecoveryBranch)})
		if len(out) == RecoveryHeadBatch {
			break
		}
	}
	return out
}

// TickRecoveryHeads accepts only a successful, exact old-branch snapshot.
// Empty success preserves an earlier carried baseline. Only a nonempty full
// SHA replaces it. Later old-branch writes are not accepted new-generation
// work: the old branch is retained, and the next launch has a distinct branch.
func TickRecoveryHeads(s *Snapshot, r TickReq) (Plan, int) {
	var p Plan
	requests := RecoveryHeadRequests(s)
	facts := s.RecoveryHeads
	if facts == nil {
		facts = r.RecoveryHeads
	}
	byCard := map[string]RecoveryHead{}
	duplicate := map[string]bool{}
	for _, f := range facts {
		if _, ok := byCard[f.Card]; ok {
			duplicate[f.Card] = true
		}
		byCard[f.Card] = f
	}
	for _, request := range requests {
		c := s.Fleet.Placed(request.Card)
		fact, known := byCard[request.Card]
		why := "branch head reader supplied no evidence"
		// unresolvable says no retry of the same read can ever succeed: the
		// debt names no repository, its generation was never known, its exact
		// branch cannot be recomputed, or the remote itself is gone (renamed).
		// Such debt goes to the seat as a judgment; everything else stays a
		// retriable happened note.
		unresolvable := false
		switch {
		case duplicate[request.Card]:
			why = "ambiguous branch head evidence"
		case !known:
		case fact.RecoveryHeadRequest != request:
			why = "branch head evidence names a different card, generation, epoch or source"
		case fact.Error != "":
			why = cutText(fact.Error, MaxCardTextBytes)
			unresolvable = strings.Contains(strings.ToLower(fact.Error), "not found")
		case request.Repo == "" || request.Gen < 1 || request.Branch != BranchOf(s.Prefix, s.Epoch, c.ID, request.Gen):
			why = "the revoked generation has no readable repository or exact source branch"
			unresolvable = true
		case fact.Head != "" && !typedrec.IsFullSha(fact.Head):
			why = "branch head is not a full commit SHA"
		default:
			set := map[string]string{}
			what := "resolved recovered branch " + request.Branch + ": no push; previous carried baseline retained"
			if fact.Head != "" {
				set[FieldCarryHead], set[FieldCarryGen] = fact.Head, itoa(request.Gen)
				what = "carried recovered branch " + request.Branch + " at " + fact.Head
			}
			p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.F("stream"), Changes: []Change{change(Fleet, setEntry(c, set, FieldRecoveryPending))}, Moved: c.ID + ": " + what})
			continue
		}
		if c.F(FieldRecoveryPending) != why {
			// said once: a later tick with the same why writes the same
			// pending text, so neither note is raised again.
			var n Note
			if unresolvable {
				friend := strings.TrimPrefix(c.F(FieldRecoveredFrom), friendRowPrefix)
				n = judgment(NRecoveryUnresolvable, c.F("stream"), s.Now, 0, c.F("primary"))
				n.Card, n.Who, n.To = c.ID, r.who(), s.Coordinator
				n.What = "No new take until " + request.Branch + " can be resolved: " + why + "; no retry of this read can succeed, so the seat answers for " + c.ID
				n.Decisions = []string{"friend give " + friend, "friend take " + friend, "ack", "wait"}
			} else {
				n = happened(NFriendRecoveryPending, c.F("stream"), s.Now, c.F("primary"))
				n.Card, n.Who, n.What = c.ID, r.who(), "No new take until "+request.Branch+" is read: "+why+"; tick retries without discarding the carried baseline"
			}
			p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.F("stream"), Changes: []Change{change(Fleet, setEntry(c, map[string]string{FieldRecoveryPending: why}))}, Notes: []Note{n}, Moved: c.ID + ": recovery pending: " + why})
		}
	}
	if len(requests) > 0 {
		cursor, exists := s.Fleet.Prop(PropRecoveryCursor)
		last := requests[len(requests)-1].Card
		if cursor != last {
			p.Props = append(p.Props, PropWrite{Table: Fleet, Name: PropRecoveryCursor, Value: last, Was: cursor, WasAbsent: !exists})
		}
	}
	return p, max(0, len(recoveryPendingCards(s))-len(requests))
}

func recoveryPendingCards(s *Snapshot) []*Card {
	var out []*Card
	for _, c := range s.Fleet.Column(Withdrawn) {
		if c.F(FieldRecoveryPending) != "" {
			out = append(out, c)
		}
	}
	return out
}

func recoveryDebt(s *Snapshot, primary *Card) string {
	if primary == nil {
		return ""
	}
	if c := s.Fleet.Placed(WorkCardID(primary.ID, primary.Int("attempt"))); c != nil && c.F(FieldRecoveryPending) != "" {
		return c.F(FieldRecoveryPending)
	}
	return ""
}

// recoveryAdmissions is the final guard on every planner, including manual
// verbs. No new work lease can bypass a revoked generation's head debt.
func recoveryAdmissions(s *Snapshot, p Plan) Plan {
	var kept []Unit
	for _, u := range p.Units {
		why := ""
		for _, ch := range u.Changes {
			e := ch.Entry
			if e.Move == nil || (e.Move.Col != Ready && e.Move.Col != Working) {
				continue
			}
			if ch.Table == Fleet {
				if c := s.Fleet.Card(e.ID); c != nil {
					why = c.F(FieldRecoveryPending)
				}
			} else if ch.Table == Work && e.Move.Col == Working {
				why = recoveryDebt(s, s.Work.Card(e.ID))
			}
			if why != "" {
				break
			}
		}
		if why != "" {
			p.refuse(u.Key, "recovery head pending: "+why)
			continue
		}
		kept = append(kept, u)
	}
	p.Units = kept
	return p
}

// Recovery provenance is retained after a deal, but only the outstanding
// automatic withdrawal relaxes the former-owner/running-beat exclusion. A
// later manual take keeps its own exclusion and running-card rules.
func recoveredLease(c *Card) bool {
	return c != nil && c.Col == Withdrawn && c.F(FieldRecoveredFrom) == c.Row && strings.HasPrefix(c.F(FieldTakenBack), "taken back from down friend ")
}
