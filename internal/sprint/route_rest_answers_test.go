package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// routes rest --answers closes a low-funds judgment in its own step, a second
// rest of a resting provider is a no-op OK (a later --until extends it), the
// funds judgments print the truthful ack line, and an ack holds a funds
// judgment until its balance recovers and falls again (the v1.2.17 friction
// card routes-rest-answers-and-a-second-rest-is-ok; tla/RouteRest.tla: a
// second rest is a stutter, its states unchanged). In-memory snapshot, fake
// clock (Snapshot.Now), no real time, sockets, network or services.
func TestRoutesRestAnswersAndASecondRestIsOK(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2030, 1, 2, 3, 0, 0, 0, time.UTC)
	routes := []Route{
		{Name: "flash-glm53-openrouter", Tier: "flash", Provider: "openrouter", Enabled: true},
		{Name: "pro-grok47-openrouter", Tier: "pro", Provider: "openrouter", Enabled: true},
	}
	balance := func(v, spend float64, at time.Time) string {
		return ProviderBalance{Provider: "openrouter", Known: true, Balance: v, At: at, SpendHour: spend}.value()
	}
	openOn := func(s *Snapshot, id string) []Open {
		var out []Open
		for _, o := range s.Open {
			if o.Note.ID == id {
				out = append(out, o)
			}
		}
		return out
	}
	closesID := func(p Plan, id string) bool {
		for _, o := range p.Closes {
			if o.Note.ID == id {
				return true
			}
		}
		for _, u := range p.Units {
			for _, o := range u.Closes {
				if o.Note.ID == id {
					return true
				}
			}
		}
		return false
	}
	said := func(p Plan) string {
		var b strings.Builder
		for _, u := range p.Units {
			b.WriteString(u.Moved + "\n")
		}
		return b.String() + strings.Join(p.Said, "\n")
	}

	// --- a low-funds judgment on a provider, closed by routes rest --answers
	f := NewTable(Fleet)
	f.SetProp(PropProviderBalance("openrouter"), balance(10, 100, t0))
	s := &Snapshot{Now: t0, Fleet: f, Work: NewTable(Work), Routes: routes, Coordinator: Coordinator, Actor: Coordinator}
	ws, _ := s.withRests()
	conds, _ := providerConds(ws)
	require.Len(t, conds, 1)
	require.Equal(t, NProviderLow, conds[0].typ)
	low := conds[0]

	// the printed decisions list the truthful ack line, and TickDecisions for
	// both funds judgments match providerConds' lists (provider named generically)
	for _, c := range conds {
		g := Group{ID: "judg-low.1", Kind: Judgment, Type: c.typ, Stream: c.stream, Size: 1, Notes: []string{"judg-low.1"}, Decisions: c.decisions}
		var lines []string
		for _, cmd := range commands(g, Note{ID: "judg-low.1", Kind: Judgment, Type: c.typ, Stream: c.stream, StreamLevel: true}, "", nil) {
			lines = append(lines, cmd.Lines...)
		}
		assert.Contains(t, lines, `nova-sprint ack judg-low.1 --reason "the owner chose not to fund openrouter"`, "the low judgment prints the truthful ack: %v", lines)
	}
	generic := func(ds []string, p string) []string {
		out := append([]string(nil), ds...)
		for i := range out {
			out[i] = strings.ReplaceAll(out[i], p, "<provider>")
		}
		return out
	}
	assert.Equal(t, generic(low.decisions, "openrouter"), TickDecisions[NProviderLow], "TickDecisions low matches providerConds")

	s.Open = []Open{{Key: OpenKey("judg-low.1", StreamSubject(ProviderSubject("openrouter"))),
		Note: Note{ID: "judg-low.1", Kind: Judgment, Type: NProviderLow, Stream: ProviderSubject("openrouter"), StreamLevel: true, Decisions: low.decisions, What: low.what, At: t0}}}
	p := RestRoutes(s, RouteRestReq{Target: "openrouter", Reason: "the owner chose not to top up", Who: Coordinator, Answers: []string{"judg-low.1"}})
	require.Empty(t, p.Refused, "routes rest --answers the low judgment: %v", p.Refused)
	require.Len(t, p.Props, 1, "the rest is written")
	assert.True(t, closesID(p, "judg-low.1"), "the rest's own step closes the judgment it answers: %+v", p)

	// an id the rest does not resolve is refused with its reason, whole step
	p = RestRoutes(s, RouteRestReq{Target: "openrouter", Reason: "x", Who: Coordinator, Answers: []string{"judg-nope.1"}})
	require.NotEmpty(t, p.Refused)
	assert.Contains(t, p.Refused[0].Why, "judg-nope.1")
	assert.Empty(t, p.Props, "a refused answer writes nothing")

	// apply the rest: the provider rests now
	for _, w := range RestRoutes(s, RouteRestReq{Target: "openrouter", Reason: "the owner chose not to top up", Who: Coordinator, Answers: []string{"judg-low.1"}}).Props {
		f.SetProp(w.Name, w.Value)
	}
	s2 := &Snapshot{Now: t0.Add(time.Minute), Fleet: f, Work: NewTable(Work), Routes: routes, Coordinator: Coordinator, Actor: Coordinator}

	// --- a second rest of the same provider is OK and changes nothing
	p = RestRoutes(s2, RouteRestReq{Target: "openrouter", Reason: "still resting", Who: Coordinator})
	require.Empty(t, p.Refused, "a second rest is OK: %v", p.Refused)
	assert.Empty(t, p.Props, "a second rest changes nothing")
	assert.Contains(t, said(p), "rests already until")
	assert.Contains(t, said(p), "nothing changed")

	// --answers on an already-resting provider still closes the judgments:
	// rest deepseek's one route for an hour, extend it with a later --until,
	// then a rest inside the held time changes nothing
	s3 := &Snapshot{Now: t0, Fleet: NewTable(Fleet), Work: NewTable(Work), Routes: append(append([]Route(nil), routes...),
		Route{Name: "flash-deepseek41-direct", Tier: "pro", Provider: "deepseek", Enabled: true}), Coordinator: Coordinator, Actor: Coordinator}
	p = RestRoutes(s3, RouteRestReq{Target: "flash-deepseek41-direct", Reason: "slow", Until: t0.Add(time.Hour), Who: Coordinator})
	require.Empty(t, p.Refused)
	for _, w := range p.Props {
		s3.Fleet.SetProp(w.Name, w.Value)
	}
	p = RestRoutes(s3, RouteRestReq{Target: "flash-deepseek41-direct", Reason: "slower", Until: t0.Add(2 * time.Hour), Who: Coordinator})
	require.Empty(t, p.Refused, "a later --until is OK: %v", p.Refused)
	require.Len(t, p.Props, 1, "a later --until extends the rest")
	assert.Contains(t, said(p), "extends")
	for _, w := range p.Props {
		s3.Fleet.SetProp(w.Name, w.Value)
	}
	p = RestRoutes(s3, RouteRestReq{Target: "flash-deepseek41-direct", Reason: "again", Until: t0.Add(30 * time.Minute), Who: Coordinator})
	require.Empty(t, p.Refused)
	assert.Empty(t, p.Props, "a rest inside the held time changes nothing")
	assert.Contains(t, said(p), "nothing changed")

	// --- the funds judgment: its decisions, and an ack that holds it until
	// the balance recovers and falls again
	f2 := NewTable(Fleet)
	f2.SetProp(PropProviderRest("openrouter"), "2030-01-02T02:00:00Z open c9 out-of-credit out of credit: provider openrouter refused card c9")
	f2.SetProp(PropProviderBalance("openrouter"), balance(0, 100, t0))
	routes4 := append(append([]Route(nil), routes...), Route{Name: "flash-deepseek41-direct", Tier: "pro", Provider: "deepseek", Enabled: true})
	s4 := &Snapshot{Now: t0, Fleet: f2, Work: NewTable(Work), Routes: routes4, Coordinator: Coordinator, Actor: Coordinator}
	ws4, _ := s4.withRests()
	fconds, _ := providerConds(ws4)
	require.Len(t, fconds, 1)
	require.Equal(t, NProviderFunds, fconds[0].typ)
	assert.Equal(t, generic(fconds[0].decisions, "openrouter"), TickDecisions[NProviderFunds], "TickDecisions funds matches providerConds")
	g := Group{ID: "judg-funds.1", Kind: Judgment, Type: NProviderFunds, Stream: ProviderSubject("openrouter"), Size: 1, Notes: []string{"judg-funds.1"}, Decisions: fconds[0].decisions}
	var lines []string
	for _, cmd := range commands(g, Note{ID: "judg-funds.1", Kind: Judgment, Type: NProviderFunds, Stream: ProviderSubject("openrouter"), StreamLevel: true}, "", nil) {
		lines = append(lines, cmd.Lines...)
	}
	assert.Contains(t, lines, `nova-sprint ack judg-funds.1 --reason "the owner chose not to fund openrouter"`, "the funds judgment prints the truthful ack: %v", lines)

	s4.Open = []Open{{Key: OpenKey("judg-funds.1", StreamSubject(ProviderSubject("openrouter"))),
		Note: Note{ID: "judg-funds.1", Kind: Judgment, Type: NProviderFunds, Stream: ProviderSubject("openrouter"), StreamLevel: true, Decisions: fconds[0].decisions, What: fconds[0].what, At: t0}}}
	require.Len(t, openOn(s4, "judg-funds.1"), 1)
	ap := Ack(s4, AckReq{Notes: []string{"judg-funds.1"}, Reason: "the owner chose not to fund openrouter", Who: Coordinator})
	require.Empty(t, ap.Refused, "ack with --reason closes a funds judgment: %v", ap.Refused)
	assert.True(t, closesID(ap, "judg-funds.1"))
	var acked []Open
	for _, u := range ap.Units {
		for _, n := range u.Notes {
			if n.Kind == Acknowledged {
				acked = append(acked, Open{Key: OpenKey("judg-funds.1", StreamSubject(ProviderSubject("openrouter"))), Note: n})
			}
		}
	}
	require.Len(t, acked, 1, "the ack is held on the judgment")

	types := []string{NProviderFunds, NProviderLow, NProviderKey, NAllOutOfCredit}
	notifyConds := func(snap *Snapshot) []cond {
		w, _ := snap.withRests()
		cs, _ := providerConds(w)
		return cs
	}
	// while the cause holds, the tick writes no new judgment (the ack holds it)
	held := &Snapshot{Now: t0.Add(time.Minute), Fleet: f2, Work: NewTable(Work), Routes: routes4, Coordinator: Coordinator, Actor: Coordinator, Acked: acked}
	var tp Plan
	notify(&tp, held, notifyConds(held), types, TickReq{Who: MachineActor})
	assert.Empty(t, tp.Notes, "the acknowledged funds judgment is not raised again while it holds")
	assert.Empty(t, tp.Closes, "nor is its acknowledgement closed while it holds")

	// the balance recovers (the rest ends, the balance is over the spend):
	// the acknowledgement closes
	props := f2.Props()
	delete(props, PropProviderRest("openrouter"))
	f2.SetProps(props)
	f2.SetProp(PropProviderBalance("openrouter"), balance(500, 100, t0.Add(2*time.Minute)))
	back := &Snapshot{Now: t0.Add(2 * time.Minute), Fleet: f2, Work: NewTable(Work), Routes: routes4, Coordinator: Coordinator, Actor: Coordinator, Acked: acked}
	tp = Plan{}
	notify(&tp, back, notifyConds(back), types, TickReq{Who: MachineActor})
	require.NotEmpty(t, tp.Closes, "the acknowledgement closes when the cause clears")

	// and falls again (a fresh refusal rests it, the balance low): the
	// judgment is raised again
	f2.SetProp(PropProviderRest("openrouter"), "2030-01-02T03:03:00Z open c10 out-of-credit out of credit: provider openrouter refused card c10")
	f2.SetProp(PropProviderBalance("openrouter"), balance(0, 100, t0.Add(3*time.Minute)))
	again := &Snapshot{Now: t0.Add(3 * time.Minute), Fleet: f2, Work: NewTable(Work), Routes: routes4, Coordinator: Coordinator, Actor: Coordinator}
	tp = Plan{}
	notify(&tp, again, notifyConds(again), types, TickReq{Who: MachineActor})
	require.Len(t, tp.Notes, 1, "the funds judgment is raised again when the balance falls again")
	assert.Equal(t, NProviderFunds, tp.Notes[0].Type)
}
