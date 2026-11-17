package steps

import (
	"fmt"
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Judgement identity (tla/JudgementId.tla): the next episode of a kind and
// subject is one past the highest episode that exists, whoever names it; a
// tick writes each judgement at most once; at most one episode of a kind and
// subject is open after a tick.

// flapQuestion is the flap judgement's question at n flaps, as rowLimits
// asks it.
func flapQuestion(row string, n int) string {
	return fmt.Sprintf("row %s has flapped %d times (down and up); the limit is %d", row, n, Engine.Limits.FlapLimit)
}

// legacy puts an open episode n of kind about about, as a store written
// before the fix holds them: any number, several open at once.
func (s *sim) legacy(kind, about string, n int, question string, deadline time.Time) string {
	id := fmt.Sprintf("%s.%s.%d", kind, about, n)
	s.must("legacy", store.PutJudgement{ID: id, Kind: kind, About: about, Question: question, Group: kind, Deadline: deadline})
	return id
}

// checkedTick is one tick, applied (the store's check runs on every op: an
// invalid op fails the test in planTick), with the plan checked against the
// store as it was before: no op writes one judgement twice, and no
// PutJudgement names an id that exists.
func (s *sim) checkedTick() Plan {
	s.t.Helper()
	before := s.snap().Judgements
	plan := s.tick()
	for _, op := range plan.Ops {
		written := set{}
		for _, w := range op.Writes {
			id := ""
			switch x := w.(type) {
			case store.PutJudgement:
				id = x.ID
				_, exists := before[id]
				require.False(s.t, exists, "op %s puts judgement %s, which exists", op.ID, id)
			case store.ExpireJudgement:
				id = x.ID
			case store.AnswerJudgement:
				id = x.ID
			default:
				continue
			}
			_, twice := written[id]
			require.False(s.t, twice, "op %s writes judgement %s twice", op.ID, id)
			written[id] = struct{}{}
		}
	}
	for _, r := range s.rejected {
		require.NotContains(s.t, r.Reason, "judgement", "a judgement op was refused: %s", r.Reason)
	}
	return plan
}

// settled requires the planner at a fixed point for kind: the next ticks
// plan no write to a judgement of it, and open stays as it is.
func (s *sim) settled(kind string, open ...string) {
	s.t.Helper()
	for range 3 {
		for _, op := range s.checkedTick().Ops {
			for _, w := range op.Writes {
				switch x := w.(type) {
				case store.PutJudgement:
					require.NotEqual(s.t, kind, x.Kind, "planned again: %+v", x)
				case store.ExpireJudgement:
					require.NotEqual(s.t, kind, s.snap().Judgements[x.ID].Kind, "planned again: %+v", x)
				}
			}
		}
		require.Equal(s.t, open, s.openJudgements(kind))
	}
}

// TestSeveralOpenFlapEpisodesLapseAsOne is the collision: a row at 5 flaps
// with flap.r.3, .4 and .5 open (a store written by the planner that named a
// reopened episode by the count of judgements), all past their deadline.
// Every one of them reopened as episode 4 (three judgements, plus one), an id
// that exists, is open and is itself lapsing, so the tick's one judgement op
// expired and put flap.r.4 at once: the store's check refused the op as
// invalid ("a second write to judgement flap.r.4 field state"), the tick
// ended there, and the next tick planned the same op. Now the latest episode
// reopens as one past the highest, the others expire superseded, and the
// next tick plans nothing more.
func TestSeveralOpenFlapEpisodesLapseAsOne(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.row("r", 1, "pro")
	s.must("flaps", store.SetRowFlaps{Row: "r", Flaps: 5})
	deadline := s.now.Add(time.Minute)
	for n := 3; n <= 5; n++ {
		s.legacy("flap", "r", n, flapQuestion("r", n), deadline)
	}
	s.now = deadline.Add(time.Second)
	s.checkedTick()
	j := s.snap().Judgements
	require.Equal(t, []string{"flap.r.6"}, s.openJudgements("flap"))
	assert.Equal(t, "expired", j["flap.r.3"].State)
	assert.Equal(t, "superseded by flap.r.5: a later episode of the same cause is open", j["flap.r.3"].Answer)
	assert.Equal(t, "superseded by flap.r.5: a later episode of the same cause is open", j["flap.r.4"].Answer)
	assert.Equal(t, "deadline passed unanswered and it still holds: reopened as flap.r.6 (repeat 1)", j["flap.r.5"].Answer)
	assert.Equal(t, "UNANSWERED 1 TIME, ESCALATED: "+flapQuestion("r", 5), j["flap.r.6"].Question)
	s.settled("flap", "flap.r.6")
}

// TestFlapRaisedOncePerCountPastReopenedEpisodes: a lapse takes the next
// episode number, so the ids run ahead of the flap count; a count reached
// later is still asked, once, as the next episode (never the count's own
// number, which a reopened episode holds), and the open one before it is
// closed in the same op.
func TestFlapRaisedOncePerCountPastReopenedEpisodes(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.row("r", 1, "pro")
	limit := Engine.Limits.FlapLimit
	s.must("flaps", store.SetRowFlaps{Row: "r", Flaps: limit})
	s.checkedTick()
	first := fmt.Sprintf("flap.r.%d", limit)
	require.Equal(t, []string{first}, s.openJudgements("flap"))
	s.now = s.snap().Judgements[first].Deadline.Add(time.Second)
	s.checkedTick() // reopened as limit+1, still about `limit` flaps
	second := fmt.Sprintf("flap.r.%d", limit+1)
	require.Equal(t, []string{second}, s.openJudgements("flap"))
	s.settled("flap", second)

	s.must("answer", store.AnswerJudgement{ID: second, Answer: "retry", Actor: "carol"})
	s.settled("flap")

	s.must("flaps", store.SetRowFlaps{Row: "r", Flaps: limit + 1}) // its own number is taken
	s.checkedTick()
	third := fmt.Sprintf("flap.r.%d", limit+2)
	require.Equal(t, []string{third}, s.openJudgements("flap"), "a new flap is asked though flap.r.<count> exists")
	assert.Equal(t, flapQuestion("r", limit+1), s.snap().Judgements[third].Question)
	s.settled("flap", third)
}

// lapseCase is a kind whose open judgement lapses and reopens: how its
// cause is made to hold, and what it is about.
type lapseCase struct {
	kind, about string
	cause       func(*sim)
}

var lapseCases = []lapseCase{
	{"unserved", "heavy", func(s *sim) {
		s.row("r1", 2, "pro")
		s.stream("s1")
		s.producer("p1", "s1", "heavy", "a.go")
	}},
	{"admission", "s1", func(s *sim) {
		s.row("r1", 2, "pro")
		s.stream("s1")
		s.producer("p1", "s1", "pro", "a.go")
		s.unadmitted = map[string]string{"p1": "it touches a path the base no longer has"}
	}},
	{"engine", "s1", func(s *sim) {
		s.row("r1", 2, "pro")
		s.stream("s1")
		s.producer("p1", "s1", "pro", "a.go")
		s.headless = set{"s1": {}}
	}},
	{"idle", "fleet", func(s *sim) {
		s.row("r1", 8, "pro")
		s.stream("s1")
		s.producer("p1", "s1", "heavy", "a.go")
	}},
	{"base-red", "o/r@main", func(s *sim) {
		s.row("r1", 2, "pro")
		s.stream("s1")
		s.stream("s2")
		s.producer("p1", "s1", "pro", "a.go")
		s.bases = redBase(strings.Repeat("a", 40), "unit", "lint")
	}},
	{"base-moved", "o/r@main", func(s *sim) {
		s.row("r1", 2, "pro")
		s.stream("s1")
		s.stream("s2")
		s.producer("p1", "s1", "pro", "a.go")
		s.run(2)
		s.must("review", store.SetCardState{Card: "p1", State: "review"})
		s.moves = movedBase(strings.Repeat("f", 40))
	}},
}

// TestSeveralOpenEpisodesLapseAsOneEveryKind is the flap case for the kinds
// a step raises by episode: episode 1 as the tick opens it, with episodes 4
// and 7 open beside it (ids that do not run from 1, as a store written
// before the fix holds), all past their deadline. Episode 7 reopens as 8,
// episodes 1 and 4 expire superseded, and the planner settles.
func TestSeveralOpenEpisodesLapseAsOneEveryKind(t *testing.T) {
	t.Parallel()
	for _, c := range lapseCases {
		t.Run(c.kind, func(t *testing.T) {
			t.Parallel()
			s := newSim(t)
			c.cause(s)
			s.run(3)
			id := func(n int) string { return fmt.Sprintf("%s.%s.%d", c.kind, c.about, n) }
			open := func() []string {
				j := s.snap().Judgements
				return slices.DeleteFunc(s.openJudgements(c.kind), func(x string) bool { return j[x].About != c.about })
			}
			require.Equal(t, []string{id(1)}, open())
			first := s.snap().Judgements[id(1)]
			require.False(t, first.Deadline.IsZero())
			s.legacy(c.kind, c.about, 4, first.Question, first.Deadline)
			s.legacy(c.kind, c.about, 7, first.Question, first.Deadline)
			s.now = first.Deadline.Add(time.Second)
			s.checkedTick()
			j := s.snap().Judgements
			require.Equal(t, []string{id(8)}, open())
			for _, n := range []int{1, 4} {
				assert.Equal(t, "expired", j[id(n)].State)
				assert.Equal(t, "superseded by "+id(7)+": a later episode of the same cause is open", j[id(n)].Answer)
			}
			assert.Equal(t, "deadline passed unanswered and it still holds: reopened as "+id(8)+" (repeat 1)", j[id(7)].Answer)
			assert.Equal(t, "UNANSWERED 1 TIME, ESCALATED: "+first.Question, j[id(8)].Question)
			for range 3 {
				s.checkedTick()
				require.Equal(t, []string{id(8)}, open(), "the planner settles: nothing is planned again")
			}
		})
	}
}

// TestLiveStoreShapeSettles is a store as the collision left it: one row at
// 26 flaps with flap.r.3 to flap.r.26, one per count (24 judgements), most
// expired, several still open and past their deadline, and one answered by
// hand an hour ago. The fixed planner takes that store as it is: one tick
// closes every open one but the latest, which reopens as flap.r.27; nothing
// is refused, nothing is invalid, and the next ticks plan nothing more. A
// later flap is asked as flap.r.28.
func TestLiveStoreShapeSettles(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.row("r", 1, "pro")
	s.must("flaps", store.SetRowFlaps{Row: "r", Flaps: 26})
	deadline := s.now.Add(time.Minute)
	for n := 3; n <= 26; n++ {
		id := s.legacy("flap", "r", n, flapQuestion("r", n), deadline)
		switch {
		case n == 25:
			s.must("answer", store.AnswerJudgement{ID: id, Answer: "retry", Actor: "carol"})
		case n < 20:
			s.must("expire", store.ExpireJudgement{ID: id, Reason: "deadline passed unanswered and it still holds: reopened as flap.r." + strconv.Itoa(n+1) + " (repeat " + strconv.Itoa(n) + ")"})
		}
	}
	require.Len(t, s.openJudgements("flap"), 6) // 20 to 24, and 26
	s.now = deadline.Add(time.Hour)
	s.checkedTick()
	j := s.snap().Judgements
	require.Equal(t, []string{"flap.r.27"}, s.openJudgements("flap"))
	for n := 20; n <= 24; n++ {
		assert.Equal(t, "superseded by flap.r.26: a later episode of the same cause is open", j[fmt.Sprintf("flap.r.%d", n)].Answer)
	}
	assert.Equal(t, "answered", j["flap.r.25"].State, "an answered one is left as it is")
	assert.Equal(t, "deadline passed unanswered and it still holds: reopened as flap.r.27 (repeat 1)", j["flap.r.26"].Answer)
	assert.Equal(t, "UNANSWERED 1 TIME, ESCALATED: "+flapQuestion("r", 26), j["flap.r.27"].Question)
	s.settled("flap", "flap.r.27")

	s.must("flaps", store.SetRowFlaps{Row: "r", Flaps: 27})
	s.checkedTick()
	require.Equal(t, []string{"flap.r.28"}, s.openJudgements("flap"))
	assert.Equal(t, flapQuestion("r", 27), s.snap().Judgements["flap.r.28"].Question)
	assert.Equal(t, "reopened at 27 as flap.r.28", s.snap().Judgements["flap.r.27"].Answer)
	s.settled("flap", "flap.r.28")
}

// TestJudgementIdsOverRandomHistories is the property over seeded histories
// of one row's flap judgements and one stream's admission judgements: a
// store that starts with episodes of any numbers, several open; then flaps,
// the admission cause on and off, answers to any open one, deadlines
// passing, and ticks. After every tick: no op was invalid or refused for a
// judgement, no op wrote one judgement twice or put an id that exists
// (checkedTick), at most one episode of each is open, and a flap count at
// the limit has been asked.
func TestJudgementIdsOverRandomHistories(t *testing.T) {
	t.Parallel()
	seeds := 200
	if testing.Short() {
		seeds = 40
	}
	for seed := 1; seed <= seeds; seed++ {
		rng := rand.New(rand.NewSource(int64(seed)))
		s := newSim(t)
		s.row("r", 1, "pro")
		s.stream("s1")
		s.producer("p1", "s1", "pro", "a.go")
		flaps := rng.Intn(3)
		if flaps > 0 {
			flaps += Engine.Limits.FlapLimit - 1
			s.must("flaps", store.SetRowFlaps{Row: "r", Flaps: flaps})
		}
		for _, k := range [][2]string{{"flap", "r"}, {"admission", "s1"}} {
			for _, n := range rng.Perm(9)[:rng.Intn(5)] {
				q := "stream s1: an older cause"
				if k[0] == "flap" {
					q = flapQuestion("r", min(n+1, flaps))
				}
				id := s.legacy(k[0], k[1], n+1, q, s.now.Add(time.Minute))
				switch rng.Intn(3) {
				case 0:
					s.must("answer", store.AnswerJudgement{ID: id, Answer: "retry", Actor: "carol"})
				case 1:
					s.must("expire", store.ExpireJudgement{ID: id, Reason: "cleared"})
				}
			}
		}
		for step := range 40 {
			switch rng.Intn(6) {
			case 0:
				flaps++
				s.must("flaps", store.SetRowFlaps{Row: "r", Flaps: flaps})
			case 1:
				if s.unadmitted == nil {
					s.unadmitted = map[string]string{"p1": "it touches a path the base no longer has"}
				} else {
					s.unadmitted = nil
				}
			case 2:
				if open := append(s.openJudgements("flap"), s.openJudgements("admission")...); len(open) > 0 {
					s.must("answer", store.AnswerJudgement{ID: open[rng.Intn(len(open))], Answer: "retry", Actor: "carol"})
				}
			case 3:
				s.now = s.now.Add(2 * seconds(Engine.Timeouts.Judgement))
			default:
				s.checkedTick()
				j := s.snap().Judgements
				require.LessOrEqual(t, len(s.openJudgements("flap")), 1, "seed %d step %d", seed, step)
				require.LessOrEqual(t, len(s.openJudgements("admission")), 1, "seed %d step %d", seed, step)
				if flaps >= Engine.Limits.FlapLimit {
					asked := false
					for _, x := range j {
						asked = asked || x.Kind == "flap" && strings.HasSuffix(x.Question, flapQuestion("r", flaps))
					}
					require.True(t, asked, "seed %d step %d: %d flaps never asked", seed, step, flaps)
				}
			}
		}
	}
}

// TestNoJudgementStaysOpenAboutAFailedCard: a judgement about a card that
// has failed (a store an older drop left so: a land judgement has no
// deadline and no cause in the view to clear it) is expired by the next
// tick; one of a kind that is not about a card is left, whatever its
// subject is called.
func TestNoJudgementStaysOpenAboutAFailedCard(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.row("r1", 2, "pro")
	s.stream("s1")
	s.producer("p1", "s1", "pro", "a.go")
	s.must("fail", store.SetCardState{Card: "p1", State: "fail"})
	s.must("left", store.PutJudgement{ID: "land.p1.1", Kind: "land", About: "p1", Question: "card p1 could not land", Group: "land"},
		store.PutJudgement{ID: "stall.p1.1", Kind: "stall", About: "p1", Question: "stream p1: nothing landed", Group: "stall"})
	s.checkedTick()
	j := s.snap().Judgements
	assert.Equal(t, "expired", j["land.p1.1"].State)
	assert.Equal(t, "p1 failed: nothing is left to decide about it", j["land.p1.1"].Answer)
	assert.Equal(t, "open", j["stall.p1.1"].State)
}
