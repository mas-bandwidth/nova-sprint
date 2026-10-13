package sprint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every declared judgment menu must offer a concrete command. Ack either closes
// the fixture or prints the commands that answer the obligation it still holds.
func TestEveryJudgmentKindHasAVerbThatClosesIt(t *testing.T) {
	t.Parallel()
	all := map[string][]string{}
	for typ, ds := range Decisions {
		all[typ] = ds
	}
	for typ, ds := range TickDecisions {
		all[typ] = ds
	}
	all[NLandingStall] = TickDecisions[NLandingStall]
	all[NLanderRefuses] = LandRefusalDecisions
	all[NLanderFails] = LandRefusalDecisions
	all[NCostGap] = CostGapDecisions
	all[NBrokenReadsOutrun] = BrokenReadsDecisions
	all[NReaderBreaks] = ReaderBreaksDecisions
	all[NRemindFailed] = []string{"ack"}
	all[NMemberEnvFault] = []string{"fleet up <m>", "wait"}
	// Inspect the producers too: a new literal N* judgment or tick condition
	// cannot quietly escape the declaration table.
	fs := token.NewFileSet()
	pkgs, err := parser.ParseDir(fs, ".", func(info os.FileInfo) bool { return !strings.HasSuffix(info.Name(), "_test.go") }, 0)
	require.NoError(t, err)
	values := map[string]string{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				v, ok := node.(*ast.ValueSpec)
				if !ok {
					return true
				}
				for i, name := range v.Names {
					if i < len(v.Values) {
						if lit, ok := v.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							values[name.Name], _ = strconv.Unquote(lit.Value)
						}
					}
				}
				return true
			})
		}
	}
	requireRow := func(expr ast.Expr) {
		if id, ok := expr.(*ast.Ident); ok && strings.HasPrefix(id.Name, "N") {
			if value := values[id.Name]; value != "" {
				assert.NotEmpty(t, all[value], "%s is raised but has no closing decision row", id.Name)
			}
		}
	}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				if call, ok := node.(*ast.CallExpr); ok {
					if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "judgment" && len(call.Args) > 0 {
						requireRow(call.Args[0])
					}
				}
				if lit, ok := node.(*ast.CompositeLit); ok {
					id, ok := lit.Type.(*ast.Ident)
					if !ok || (id.Name != "Note" && id.Name != "cond") {
						return true
					}
					var kind, typ ast.Expr
					for _, elt := range lit.Elts {
						if kv, ok := elt.(*ast.KeyValueExpr); ok {
							if key, ok := kv.Key.(*ast.Ident); ok {
								if key.Name == "Kind" {
									kind = kv.Value
								}
								if key.Name == "Type" || key.Name == "typ" {
									typ = kv.Value
								}
							}
						}
					}
					judgment := id.Name == "cond"
					if k, ok := kind.(*ast.Ident); ok && k.Name == "Judgment" {
						judgment = true
					}
					if judgment && typ != nil {
						requireRow(typ)
					}
				}
				return true
			})
		}
	}
	for typ, ds := range all {
		t.Run(typ, func(t *testing.T) {
			t.Parallel()
			w := setup(t, 1)
			n := Note{ID: "n-x.1", Kind: Judgment, Type: typ, Stream: "s1", Primaries: []string{"s1-1"}, Count: 1, Decisions: ds, At: w.s.Now}
			if slicesContainSubstring(ds, "resume") {
				n.StreamLevel = true
				n.Primaries = nil
				n.Count = 0
				w.s.StreamCtl("s1").Fields["state"] = StreamStopped
			}
			g := Group{ID: n.ID, Kind: Judgment, Type: typ, Stream: n.Stream, Size: 1, Notes: []string{n.ID}, Members: n.Primaries, Decisions: ds}
			require.NotEmpty(t, commands(g, n, "", nil), "%s has no executable decision", typ)
			w.s.Open = []Open{{Key: OpenKey(n.ID, n.Subjects()[0]), Note: n}}
			var p Plan
			switch {
			case typ == NSprintDone:
				p = Add(w.s, AddReq{Stream: "s1", Count: 1})
			case slicesContainSubstring(ds, "resume"):
				p = Resume(w.s, ResumeReq{Stream: "s1", Did: "the cause was repaired", Answers: []string{n.ID}})
			case contains(ds, "ack") || contains(ds, "keep"):
				p = Ack(w.s, AckReq{Notes: []string{n.ID}, Reason: "cause checked"})
			case contains(ds, "drop"):
				p = Drop(w.s, DropReq{Sel: Sel{Only: []string{"s1-1"}}, Answers: []string{n.ID}, Reason: "cause checked"})
			case contains(ds, "wait") || slicesContainPrefix(ds, "wait "):
				p = Wait(w.s, WaitReq{Note: n.ID, Until: w.s.Now.Add(time.Minute)})
			case slicesContainPrefix(ds, "rework"):
				stallCard(w, "s1-1", Review)
				p = Rework(w.s, ReworkReq{Sel: Sel{Only: []string{"s1-1"}}, Answers: []string{n.ID}, Fix: "repair the cause"})
			case contains(ds, "act"):
				// The inbox's actLine prints wait for an informational judgment.
				p = Wait(w.s, WaitReq{Note: n.ID, Until: w.s.Now.Add(time.Minute)})
			default:
				t.Fatalf("%s has no closing verb fixture for %v", typ, ds)
			}
			if len(p.Refused) > 0 {
				why := p.Refused[0].Why
				require.True(t, strings.Contains(why, "nova-sprint ") || strings.Contains(why, "run:") || strings.Contains(why, "its review time is set instead"), "a refusal must name a runnable remedy or setting its review time: %s", why)
			} else {
				require.Len(t, p.Units, 1)
				require.NotEmpty(t, p.Units[0].Closes, "the verb closes its judgment")
			}
		})
	}
}

func TestNoRouteConditionClearsItsJudgmentAndAck(t *testing.T) {
	t.Parallel()
	for _, acked := range []bool{false, true} {
		w := setup(t, 1)
		n := Note{ID: "n-route.1", Kind: Judgment, Type: NNoRoute, Stream: TierSubject("heavy"), StreamLevel: true, Decisions: TickDecisions[NNoRoute], At: w.s.Now}
		o := Open{Key: OpenKey(n.ID, StreamSubject(n.Stream)), Note: n}
		if acked {
			o.Note.Kind = Acknowledged
			w.s.Acked = []Open{o}
		} else {
			w.s.Open = []Open{o}
		}
		var p Plan
		notify(&p, w.s, nil, []string{NNoRoute}, TickReq{})
		require.Len(t, p.Closes, 1)
		require.NotEmpty(t, p.Notes)
		require.True(t, strings.Contains(p.Notes[0].What, "the condition cleared:"))
	}
}

func TestLandingStallAckWaitAndLandingClose(t *testing.T) {
	t.Parallel()
	for _, wait := range []bool{false, true} {
		w := setup(t, 1)
		stallCard(w, "s1-1", Review)
		w.s.Work.SetProp(PropLastLanding, stamp(w.s.Now.Add(-2*time.Minute)))
		p, _ := TickLandingStall(w.s, TickReq{})
		w.must(p)
		ns := w.notesOf(NLandingStall)
		require.Len(t, ns, 1)
		if wait {
			p = Wait(w.s, WaitReq{Note: ns[0].ID, Until: w.s.Now.Add(3 * time.Minute)})
		} else {
			p = Ack(w.s, AckReq{Notes: []string{ns[0].ID}, Reason: "investigating"})
		}
		require.Empty(t, p.Refused)
		for _, u := range p.Units {
			for _, n := range u.Notes {
				if n.Kind == Acknowledged {
					n.ID = "n-held.1"
					w.s.Acked = []Open{{Key: OpenKey(n.ID, n.Subjects()[0]), Note: n}}
				}
			}
		}
		w.must(p)
		p, _ = TickLandingStall(w.s, TickReq{})
		require.Empty(t, p.Notes, "held until review or bound")
		w.tick(time.Second)
		w.s.Work.SetProp(PropLastLanding, landingStamp(w.s.Now))
		p, _ = TickLandingStall(w.s, TickReq{})
		require.NotEmpty(t, p.Closes, "the next landing clears held ack too")
	}
}

func slicesContainPrefix(ds []string, prefix string) bool {
	for _, d := range ds {
		if strings.HasPrefix(d, prefix) {
			return true
		}
	}
	return false
}

func TestNoRouteClosesOnTheTickAfterRouteAdd(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1", Width: 2}))
	w.s.Routes = []Route{{Name: "heavy-old", Tier: cardhdr.RouteHeavy, Provider: "p", Model: "m", Enabled: true}}
	w.s.Tiers = map[string][]string{cardhdr.RouteHeavy: {"heavy-old"}}
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 1}))
	p, _ := TickDeal(w.s, TickReq{})
	w.must(p)
	require.NotNil(t, tierJudgment(w, cardhdr.RouteFlash))
	// route add is a configuration write: the tick reads the newly served route.
	w.s.Routes = []Route{{Name: "flash-new", Tier: cardhdr.RouteFlash, Provider: "p", Model: "m", Enabled: true, Deadline: 600}}
	w.s.Tiers = map[string][]string{cardhdr.RouteFlash: {"flash-new"}}
	p, _ = TickDeal(w.s, TickReq{})
	require.NotEmpty(t, p.Closes)
	w.must(p)
	for _, o := range w.s.Open {
		require.NotEqual(t, NNoRoute, o.Note.Type)
	}
}

func TestLegacyStallAndNoRouteHaveClosingVerbs(t *testing.T) {
	t.Parallel()
	for _, typ := range []string{NLandingStall, NNoRoute} {
		w := setup(t, 1)
		n := Note{ID: "n-old.1", Kind: Judgment, Type: typ, SprintLevel: true, At: w.s.Now}
		w.s.Open = []Open{{Key: OpenKey(n.ID, n.Subjects()[0]), Note: n}}
		p := Ack(w.s, AckReq{Notes: []string{n.ID}, Reason: "legacy cause checked"})
		require.Empty(t, p.Refused)
		require.NotEmpty(t, p.Units)
	}
	w := setup(t, 1)
	n := Note{ID: "n-old.2", Kind: Judgment, Type: NLandingStall, SprintLevel: true, At: w.s.Now}
	w.s.Open = []Open{{Key: OpenKey(n.ID, n.Subjects()[0]), Note: n}}
	p, _ := TickLandingStall(w.s, TickReq{})
	require.Len(t, p.Closes, 1, "the condition is false even without episode stamps")
}

func TestEveryTickKeptConditionClearsOnFirstTick(t *testing.T) {
	t.Parallel()
	for typ := range TickDecisions {
		for _, acked := range []bool{false, true} {
			w := setup(t, 1)
			n := Note{ID: "n-clear.1", Kind: Judgment, Type: typ, StreamLevel: true, Stream: "subject", At: w.s.Now}
			o := Open{Key: OpenKey(n.ID, StreamSubject(n.Stream)), Note: n}
			if acked {
				o.Note.Kind = Acknowledged
				w.s.Acked = []Open{o}
			} else {
				w.s.Open = []Open{o}
			}
			var p Plan
			notify(&p, w.s, nil, []string{typ}, TickReq{})
			require.Len(t, p.Closes, 1, "%s clears immediately", typ)
			require.Len(t, p.Notes, 1, "%s records its clear", typ)
			require.Contains(t, p.Notes[0].What, "the condition cleared:")
		}
	}
}

func TestLandingStallHoldExpiresAtBoundOrReview(t *testing.T) {
	t.Parallel()
	for _, wait := range []bool{false, true} {
		w := setup(t, 1)
		stallCard(w, "s1-1", Review)
		w.s.Work.SetProp(PropLastLanding, landingStamp(w.s.Now.Add(-2*time.Minute)))
		w.s.Work.SetProp(PropLandingStallSince, landingStamp(w.s.Now.Add(-time.Minute)))
		w.s.Work.SetProp(PropLandingStallSaid, landingStamp(w.s.Now))
		n := Note{ID: "n-hold.1", Kind: Acknowledged, Type: NLandingStall, SprintLevel: true, At: w.s.Now}
		if wait {
			n.Review = w.s.Now.Add(3 * time.Minute)
			n.ReviewSet = w.s.Now
		}
		w.s.Acked = []Open{{Key: OpenKey(n.ID, n.Subjects()[0]), Note: n}}
		w.tick(LandingStallBound)
		p, _ := TickLandingStall(w.s, TickReq{})
		if wait {
			require.Empty(t, p.Closes)
			require.Empty(t, p.Notes)
			w.tick(2 * time.Minute)
			p, _ = TickLandingStall(w.s, TickReq{})
		}
		require.Len(t, p.Closes, 1)
		require.Len(t, p.Notes, 1)
		require.Equal(t, Judgment, p.Notes[0].Kind)
		require.Equal(t, NLandingStall, p.Notes[0].Type)
	}
}

func TestLegacyLandingStallClosesWithWaitingCardsAfterFreshLanding(t *testing.T) {
	t.Parallel()
	for _, acked := range []bool{false, true} {
		w := setup(t, 1)
		stallCard(w, "s1-1", Review)
		w.s.Work.SetProp(PropLastLanding, landingStamp(w.s.Now))
		n := Note{ID: "n-legacy.1", Kind: Judgment, Type: NLandingStall, SprintLevel: true, At: w.s.Now.Add(-2 * time.Minute)}
		o := Open{Key: OpenKey(n.ID, n.Subjects()[0]), Note: n}
		if acked {
			o.Note.Kind = Acknowledged
			w.s.Acked = []Open{o}
		} else {
			w.s.Open = []Open{o}
		}
		p, _ := TickLandingStall(w.s, TickReq{})
		require.Len(t, p.Closes, 1)
		require.Contains(t, p.Notes[0].What, "the condition cleared:")
	}
}

func TestLandingStallWaitAndAckIgnoreStoppedTime(t *testing.T) {
	t.Parallel()
	for _, wait := range []bool{false, true} {
		w := setup(t, 1)
		stallCard(w, "s1-1", Review)
		w.s.Work.SetProp(PropLastLanding, landingStamp(w.s.Now.Add(-2*time.Minute)))
		p, _ := TickLandingStall(w.s, TickReq{})
		w.must(p)
		n := w.notesOf(NLandingStall)[0]
		if wait {
			p = Wait(w.s, WaitReq{Note: n.ID, Until: w.s.Now.Add(time.Minute)})
		} else {
			p = Ack(w.s, AckReq{Notes: []string{n.ID}, Reason: "investigating"})
		}
		require.Empty(t, p.Refused)
		for _, u := range p.Units {
			for _, n := range u.Notes {
				if n.Kind == Acknowledged {
					n.ID = "n-produced.1"
					w.s.Acked = []Open{{Key: OpenKey(n.ID, n.Subjects()[0]), Note: n}}
					if wait {
						require.Equal(t, w.s.Now, n.At, "wait hold fallback base is the decision time")
					}
				}
			}
		}
		w.must(p)
		w.tick(2 * time.Minute)
		p, _ = TickLandingStall(w.s, TickReq{Stopped: func(from, to time.Time) time.Duration { return to.Sub(from) }})
		require.Empty(t, p.Closes)
		require.Empty(t, p.Notes)
		p, _ = TickLandingStall(w.s, TickReq{})
		require.NotEmpty(t, p.Closes)
		require.Len(t, p.Notes, 1)
		require.Equal(t, Judgment, p.Notes[0].Kind)
	}
}

func slicesContainSubstring(ds []string, part string) bool {
	for _, d := range ds {
		if strings.Contains(d, part) {
			return true
		}
	}
	return false
}
