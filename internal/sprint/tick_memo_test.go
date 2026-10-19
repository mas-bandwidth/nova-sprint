package sprint

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// tickFix is a fake in-memory snapshot (no Redis) with a fake clock at 1000.0
// seconds, built by buildTickFixture: N cards x M open judgments, with cards at
// their bound, blocked dependents behind blockers and sentinels.
type tickFix struct {
	s *Snapshot
	r TickReq
}

// buildTickFixture makes the fixture for all three tick-memo tests: nCards work
// cards over 16 streams with a realistic mix (about 35% waiting behind a need
// chain up to depth 6, 10% needing an absent card, 16 sentinels with the waiting
// cards of their row behind them), holding judgments on about M cards (about
// 55% of them bounds), and a fleet of friend readers holding read-card records
// with generations. It is always sized above live (live ~1216 cards; these start
// at 40x10 and go to 5000x2000).
func buildTickFixture(t testing.TB, nCards, nJudgments int) *tickFix {
	t.Helper()
	s := &Snapshot{
		Now:         time.Unix(1000, 0),
		Work:        NewTable(Work),
		Readers:     NewTable(Readers),
		Merge:       NewTable(Merge),
		Fleet:       NewTable(Fleet),
		Coordinator: "coordinator",
		Actor:       "coordinator",
		Providers:   map[string]Provider{},
	}
	const nStreams = 16
	rows := make([]string, 0, nStreams)
	for i := 0; i < nStreams; i++ {
		st := fmt.Sprintf("stream%02d", i)
		s.Merge.Put(&Card{ID: CtlID(st), Row: st, Col: Ctl, Fields: map[string]string{"kind": "stream"}})
		rows = append(rows, st)
	}
	s.Work.SetRows(rows)

	brief := "KIND: primary\nPATHS: x.go\nTEST: none reason\n"
	cols := []string{Waiting, Ready, Working, Review, Merging}
	ids := make([]string, nCards)
	for i := 0; i < nCards; i++ {
		id := fmt.Sprintf("card%05d", i)
		ids[i] = id
		c := &Card{ID: id, Row: rows[i%nStreams], Col: cols[(i/7)%len(cols)], Score: float64(i),
			Fields: map[string]string{"brief": brief}}
		switch i % 100 {
		case 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19,
			20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34:
			if i%7 == 0 { // needing the row's sentinel: its cards behind it are blocked
				c.Fields["needs"] = fmt.Sprintf("sentinel%02d", i%nStreams)
			} else { // waiting behind a need chain up to depth 6
				depth := (i/100)%6 + 1
				c.Fields["needs"] = fmt.Sprintf("card%05d", (i+depth)%nCards)
			}
		case 35, 36, 37, 38, 39, 40, 41, 42, 43, 44: // needing an absent card: a direct blocker
			c.Fields["needs"] = "absent-need-" + itoa(i)
		}
		s.Work.Put(c)
	}

	// Sentinels: one per stream, before every card of its row (a negative score), so
	// the waiting cards of the row are the cards behind it (Blockers' Behind walk).
	sentinelBrief := "KIND: sentinel\nCONSUMERS: card00000\nPATHS: x.go\nTEST: none reason\n"
	sentinels := make([]string, nStreams)
	for i := 0; i < nStreams; i++ {
		id := fmt.Sprintf("sentinel%02d", i)
		sentinels[i] = id
		s.Work.Put(&Card{ID: id, Row: rows[i], Col: Waiting, Score: -float64(i + 1),
			Fields: map[string]string{"kind": "sentinel", "brief": sentinelBrief}})
	}

	// The fleet: friend reader rows, and read-card records with generations so
	// ReadCardExtras/readNextGen have bases to index.
	fleetRows := []string{"friend.alice", "friend.bob", "friend.carol"}
	s.Fleet.SetRows(fleetRows)
	for k := 0; k < 300; k++ {
		pid := ids[(k*37)%nCards]
		reader := fleetRows[k%len(fleetRows)][len("friend."):]
		base := ReadCardID(pid, 1, reader)
		g := k % 4
		id := base
		if g > 0 {
			id = base + ".g" + itoa(g)
		}
		s.Fleet.Put(&Card{ID: id, Row: reader, Col: Done, Score: float64(k), Fields: map[string]string{}})
	}

	// Open judgments, about nJudgments of them, holding cards so the needs of cards
	// behind them resolve as held and block: the 16 sentinels first (so each is a
	// blocker), then about 55% bounds and a mix of the other holding kinds.
	holdTypes := []string{NBound, NBriefWrong, NHarnessFault}
	s.Open = make([]Open, 0, nJudgments)
	for j := 0; j < nJudgments; j++ {
		target := ids[j%nCards]
		typ := holdTypes[0]
		if j < nStreams {
			target = sentinels[j%nStreams]
		} else if j%20 >= 11 {
			typ = holdTypes[1+(j%2)]
		}
		note := Note{Kind: Judgment, Type: typ, Primaries: []string{target}, Count: 1,
			At: s.Now.Add(-time.Minute), ID: fmt.Sprintf("j%d", j), What: target + " reached its bound"}
		s.Open = append(s.Open, Open{Key: OpenKey(note.ID, target), Note: note})
	}
	return &tickFix{s: s, r: TickReq{Who: MachineActor}}
}

// runTickDecisions runs the tick's decision functions the CPU profile names:
// ReadCardExtras (the read-generation scan), TickCheck and JudgmentOverdue over
// every open judgment (which drives quietedByBlocker and the Blockers memo).
func runTickDecisions(f *tickFix) {
	ReadCardExtras(f.s)
	TickCheck(f.s, f.r)
	for _, o := range f.s.Open {
		if o.Note.Kind == Judgment {
			JudgmentOverdue(f.s, f.r, o.Note)
		}
	}
}

// refBlockers is the pre-change Blockers walk, kept as the reference the memo's
// answers are compared against.
func refBlockers(s *Snapshot) []Blocker {
	if s == nil || s.Work == nil {
		return nil
	}
	var unlandedCards []*Card
	for _, st := range []State{Waiting, Ready, Working, Review, Merging} {
		for _, c := range s.Work.Column(st) {
			if !IsSentinel(c) {
				unlandedCards = append(unlandedCards, c)
			}
		}
	}
	for _, c := range s.Work.Cards() {
		if IsSentinel(c) && c.Placed() && c.Col != Landed {
			unlandedCards = append(unlandedCards, c)
		}
	}
	blockerSet := map[string]bool{}
	for _, c := range unlandedCards {
		unres := UnresolvedNeeds(s, c.ID, Split(c.F("needs")), nil)
		for _, u := range unres {
			if u.Reason == UnresolvedHeld || u.Reason == UnresolvedDropped || u.Reason == UnresolvedAbsent {
				blockerSet[u.Need] = true
			}
		}
	}
	if len(blockerSet) == 0 {
		return nil
	}
	blockerIDs := make([]string, 0, len(blockerSet))
	for id := range blockerSet {
		blockerIDs = append(blockerIDs, id)
	}
	slices.Sort(blockerIDs)
	var blockers []Blocker
	for _, bID := range blockerIDs {
		visited := map[string]bool{bID: true}
		queue := []string{bID}
		var depList []Dependent
		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[1:]
			if currCard := s.Work.Card(curr); currCard != nil && IsSentinel(currCard) {
				for _, bh := range Behind(s, currCard) {
					if bh.Placed() && bh.Col != Landed && !visited[bh.ID] {
						visited[bh.ID] = true
						queue = append(queue, bh.ID)
						if !IsSentinel(bh) {
							depList = append(depList, Dependent{ID: bh.ID, Stream: bh.Row})
						}
					}
				}
			}
			for _, c := range unlandedCards {
				if visited[c.ID] {
					continue
				}
				needs := Split(c.F("needs"))
				waived := Split(c.F("waived"))
				if contains(needs, curr) && !contains(waived, curr) {
					visited[c.ID] = true
					queue = append(queue, c.ID)
					if !IsSentinel(c) {
						depList = append(depList, Dependent{ID: c.ID, Stream: c.Row})
					}
					if IsSentinel(c) {
						for _, bh := range Behind(s, c) {
							if bh.Placed() && bh.Col != Landed && !visited[bh.ID] {
								visited[bh.ID] = true
								queue = append(queue, bh.ID)
								if !IsSentinel(bh) {
									depList = append(depList, Dependent{ID: bh.ID, Stream: bh.Row})
								}
							}
						}
					}
				}
			}
		}
		if len(depList) > 0 {
			blockers = append(blockers, Blocker{
				ID:         bID,
				Card:       s.Work.Card(bID),
				Dependents: depList,
			})
		}
	}
	return blockers
}

// refReadNextGen is the pre-change scan-based readNextGen, kept as the reference
// the memo's answers are compared against.
func refReadNextGen(cards []*Card, primary string, attempt int, reader string) int {
	base := ReadCardID(primary, attempt, reader)
	next := 0
	for _, c := range cards {
		if g := readGenOf(c.ID, base); g >= next {
			next = g + 1
		}
	}
	return next
}

// refQuietedByBlocker is quietedByBlocker's pre-change form using the reference walk.
func refQuietedByBlocker(s *Snapshot, n Note) bool {
	if s == nil {
		return false
	}
	if n.Type == NNeedBlocked {
		return true
	}
	if contains(NeedHoldingKinds, n.Type) {
		cards := cardsHeldBy(n)
		for _, b := range refBlockers(s) {
			if contains(cards, b.ID) && len(b.Dependents) > 0 {
				return true
			}
		}
	}
	return false
}

// refQuietedByBlockerBlockers is refQuietedByBlocker with the reference walk
// computed once and shared across the judgments.
func refQuietedByBlockerBlockers(blockers []Blocker, n Note) bool {
	if n.Type == NNeedBlocked {
		return true
	}
	if contains(NeedHoldingKinds, n.Type) {
		cards := cardsHeldBy(n)
		for _, b := range blockers {
			if contains(cards, b.ID) && len(b.Dependents) > 0 {
				return true
			}
		}
	}
	return false
}

// refJudgmentOverdueBlockers is JudgmentOverdue's pre-change form with the
// reference Blockers walk shared: quietedByTier, the reference quietedByBlocker,
// then the due-time comparison.
func refJudgmentOverdueBlockers(s *Snapshot, r TickReq, n Note, blockers []Blocker) bool {
	if s == nil {
		return false
	}
	if quietedByTier(s, n) || refQuietedByBlockerBlockers(blockers, n) {
		return false
	}
	if !n.Review.IsZero() {
		return DueNow(s.Now, n.Review, n.ReviewSet, r.Stopped)
	}
	d, ok := r.running(s.Now, stamp(n.At))
	return ok && d > DeadlineJudgment
}

// TestTickMemoDecisionsAreIdentical asserts the memoized tick produces the exact
// decisions the unmemoized one does: Blockers equals the reference walk, readNextGen
// equals the reference scan for every (primary, reader), and the tick's overdue and
// check decisions are identical across a fresh snapshot. It does not run parallel:
// the tick counters are package-level and shared with the complexity test, which must
// not race them.
func TestTickMemoDecisionsAreIdentical(t *testing.T) {
	t.Parallel()
	for _, size := range [][2]int{{40, 10}, {600, 330}, {5000, 2000}} {
		size := size
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			t.Parallel()
			// the complexity test's counters are off here, but its measured phase holds
			// the lock: a tick of this test must not land in the middle of it
			tickCountMu.Lock()
			defer tickCountMu.Unlock()
			f := buildTickFixture(t, size[0], size[1])

			memo := Blockers(f.s)
			ref := refBlockers(f.s)
			require.Equal(t, ref, memo, "Blockers memo differs from reference walk")

			cards := f.s.Fleet.Cards()
			for _, c := range f.s.Work.Column(Review) {
				attempt := readAttempt(c)
				for _, row := range f.s.Fleet.Rows() {
					name, ok := FriendOfRow(row)
					if !ok {
						continue
					}
					base := ReadCardID(c.ID, attempt, name)
					want := refReadNextGen(cards, c.ID, attempt, name)
					got := f.s.readNextGenFor(base)
					require.Equal(t, want, got, "readNextGen differs for base %s", base)
				}
			}

			p1, _ := TickCheck(f.s, f.r)
			f2 := buildTickFixture(t, size[0], size[1])
			p2, _ := TickCheck(f2.s, f2.r)
			require.Equal(t, p1, p2, "TickCheck not deterministic across snapshots")

			refBlockersOnce := refBlockers(f.s)
			for _, o := range f.s.Open {
				if o.Note.Kind != Judgment {
					continue
				}
				n := o.Note
				require.Equal(t, refJudgmentOverdueBlockers(f.s, f.r, n, refBlockersOnce),
					JudgmentOverdue(f.s, f.r, n), "overdue decision differs for %s", n.ID)
			}
		})
	}
}
