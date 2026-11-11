package steps

import (
	"fmt"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// judgement is one a step opens: kind.about.episode, a cause opening once
// (an open one of the same kind and about is not opened again), a group key
// for one push per shared cause (LESSONS 4.9), the deadline policy states
// (none for held by Glenn's ruling) and the command that answers it.
type judgement struct {
	kind, about, question string
	episode               int // 0: the next
}

// The closed list a step opens, with the choices judge offers for each.
var choices = map[string]string{
	"held": "unhold | drop", "unserved": "retry", "cannot-read": "retry | drop", "land": "land | fix | retry",
	"stall": "retry", "idle": "retry", "land-rate": "retry", "flap": "retry", "brief": "rework | drop",
	"landed-sentinel": "retry", "policy": "retry", "engine": "retry", "roster": "retry",
	"no-model": "retry", "no-policy": "retry", "undealt": "retry", "admission": "retry", "down": "retry",
	"base-red": "retry", "base-moved": "retry",
}

// judge opens kind about about unless one is open (a cause opens once); an
// open one's cause is detected again (held).
func (t *tick) judge(kind, about, question string) {
	t.detect(kind, about)
	if t.held(kind, about) || t.pending(kind, about) {
		return
	}
	if since, quiet := t.answeredQuiet(kind, about); quiet {
		return
	} else if since > 0 {
		question = fmt.Sprintf("STILL HOLDS %s after your answer: %s", since.Truncate(time.Second), question)
	}
	t.addJudge(judgement{kind: kind, about: about, question: question})
}

// answeredQuiet is whether kind about about's latest episode is a watch
// judgement answered within the judgement timeout whose cause has held at
// every tick since the answer: the answer says the coordinator is on it,
// so the same cause still holding is no news until the timeout passes
// (seen live 2026-11-10: a stall answered retry reopened seconds later,
// again and again). A cause a tick saw not holding since the answer
// (Inputs.Broken), or an answer from before this run's view began (it did
// not watch the cause since), is news: no quiet, and since is 0, so it
// opens with no STILL HOLDS. Past the timeout, an unbroken cause's since
// is how long ago it was answered.
func (t *tick) answeredQuiet(kind, about string) (since time.Duration, quiet bool) {
	j, ok := t.answered(kind, about)
	if !ok || t.in.Broken[j.ID] || j.ClosedAt.Before(t.v.Since) {
		return 0, false
	}
	return t.in.Now.Sub(j.ClosedAt), t.in.Now.Before(j.ClosedAt.Add(seconds(t.tmo.Judgement)))
}

// answered is kind about about's latest episode when it is a watch
// judgement (its one choice is retry) the coordinator answered.
func (t *tick) answered(kind, about string) (store.Judgement, bool) {
	if choices[kind] != "retry" {
		return store.Judgement{}, false
	}
	j := t.v.Judgements[fmt.Sprintf("%s.%s.%d", kind, about, t.episodes(kind, about))]
	return j, j.State == "answered"
}

// broken is the next tick's Inputs.Broken: each answered watch judgement,
// the latest of its kind and about, whose cause a tick has seen not holding
// since the answer (no step detected it this tick, or one did not before).
// base-red's red stretch is dated by the watch instead (baseRed).
func (t *tick) broken() map[string]bool {
	var out map[string]bool
	for _, j := range t.v.Judgements {
		if j.State != "answered" || j.Kind == "base-red" {
			continue
		}
		if latest, ok := t.answered(j.Kind, j.About); !ok || latest.ID != j.ID {
			continue
		}
		if _, holds := t.detected[jkey(j.Kind, j.About)]; holds && !t.in.Broken[j.ID] {
			continue
		}
		if out == nil {
			out = map[string]bool{}
		}
		out[j.ID] = true
	}
	return out
}

// judgeAt opens episode n of kind about about once: a count that only grows
// (a row's flaps) names its own episode.
func (t *tick) judgeAt(kind, about string, n int, question string) {
	if j, exists := t.v.Judgements[fmt.Sprintf("%s.%s.%d", kind, about, n)]; exists || t.pending(kind, about) {
		if exists && j.State == "open" {
			t.held(kind, about)
		}
		return
	}
	t.addJudge(judgement{kind: kind, about: about, question: question, episode: n})
}

// engine is the judgement for a defect here or in layer 2: a plan the
// machine or the store refused on fresh state.
func (t *tick) engine(about, why string) { t.judge("engine", about, why) }

// addJudge plans j and indexes it by kind and about, so pending is one lookup
// however many the tick opens (a scan per open was quadratic, measured
// 52 ms for 4,908 opens on the working machine).
func (t *tick) addJudge(j judgement) {
	t.judges = append(t.judges, j)
	if t.pend == nil {
		t.pend = set{}
	}
	t.pend[jkey(j.kind, j.about)] = struct{}{}
}

func jkey(kind, about string) string { return kind + "\x00" + about }

func (t *tick) pending(kind, about string) bool {
	_, ok := t.pend[jkey(kind, about)]
	return ok
}

func (t *tick) isOpen(kind, about string) bool {
	_, ok := t.jindex().open[jkey(kind, about)]
	return ok
}

// held is isOpen, and records that a step detected the open judgement's
// cause this tick: at its deadline it is reopened, never closed (expiry).
func (t *tick) held(kind, about string) bool {
	if !t.isOpen(kind, about) {
		return false
	}
	t.detect(kind, about)
	return true
}

// detect records that a step detected kind about about's cause this tick:
// an open one's deadline reopens it (lapse), and an answered one's quiet
// holds (broken).
func (t *tick) detect(kind, about string) {
	if t.detected == nil {
		t.detected = set{}
	}
	t.detected[jkey(kind, about)] = struct{}{}
}

// episodes is how many judgements of kind about about exist, open or closed.
func (t *tick) episodes(kind, about string) int { return t.jindex().episodes[jkey(kind, about)] }

// judgedIndex is the view's judgements by kind and about: the open ones and
// how many episodes each has. Built once a tick, on first use (the view
// does not change during a tick).
type judgedIndex struct {
	open     set
	episodes map[string]int
}

func (t *tick) jindex() judgedIndex {
	if t.jidx.open != nil {
		return t.jidx
	}
	t.jidx = judgedIndex{open: set{}, episodes: map[string]int{}}
	for _, j := range t.v.Judgements {
		k := jkey(j.Kind, j.About)
		t.jidx.episodes[k]++
		if j.State == "open" {
			t.jidx.open[k] = struct{}{}
		}
	}
	return t.jidx
}

// riding is the writes of a judgement that rides in a group's op (a hold's
// held, a sentinel's landed-sentinel), taken out of the judgement op.
func (t *tick) riding(kind, about, question string) []store.Write {
	if t.held(kind, about) || t.pending(kind, about) {
		return nil
	}
	j := judgement{kind: kind, about: about, question: question}
	t.addJudge(judgement{kind: kind, about: about, question: question, episode: -1})
	return []store.Write{t.put(j)}
}

// put is the write that opens j.
func (t *tick) put(j judgement) store.Write {
	n := j.episode
	if n <= 0 {
		n = t.episodes(j.kind, j.about) + 1
	}
	id := fmt.Sprintf("%s.%s.%d", j.kind, j.about, n)
	w := store.PutJudgement{ID: id, Kind: j.kind, About: j.about, Question: j.question, Group: j.kind,
		Command: fmt.Sprintf("nova-sprint judge %s <choice> --reason <why>   choices: %s", ShellWord(id), choices[j.kind])}
	if j.kind == "policy" {
		w.Group = fmt.Sprintf("policy:v%d", t.o.a.Version)
	}
	if j.kind == "roster" {
		w.Group = RosterGroup(j.about)
	}
	// A held card, a land judgement and the no-policy gate sit until
	// answered or cleared: a land judgement's producer stays merging with no
	// request until the coordinator says land, fix or retry (intent rule 32).
	if j.kind != "held" && j.kind != "land" && j.kind != "no-policy" {
		w.Deadline = t.in.Now.Add(seconds(t.tmo.Judgement))
	}
	return w
}

// judgements is the judgements step (LAYER-3 "Judgements"; the model's jnew,
// Expired and Lapsed): the alarms, one policy judgement per missed
// question, and the expiries: a cause that cleared in the view; at the
// deadline (the kind's safe default never drops, accepts, merges or lands,
// LESSONS 5.7) a cause no step detected this tick closes as cleared, and one
// still detected is never closed silently (the structure notices, pushes,
// repeats and escalates; Glenn 2026-11-07): it expires naming its next
// episode, which opens in the same op with the repeat count, and is pushed
// to the coordinator again as NEEDS YOU.
func (t *tick) judgements() {
	t.alarms()
	t.baseRed()
	t.baseMoved()
	for _, q := range t.o.missed() {
		t.judge("policy", q, fmt.Sprintf("policy v%d failed question %s: %s; the default answered this tick: run nova-sprint policy check", t.o.a.Version, q, t.o.why(q)))
	}
	for _, id := range sortedKeys(t.v.Judgements) {
		j := t.v.Judgements[id]
		if _, replaced := t.expires[id]; replaced || j.State != "open" { // replaced: baseRed or baseMoved opened its next episode
			continue
		}
		if why := t.cleared(j); why != "" {
			t.expires[id] = why
		} else if !j.Deadline.IsZero() && !t.in.Now.Before(j.Deadline) {
			t.lapse(j)
		}
	}
}

// lapse is the deadline of open judgement j: closed as cleared when no step
// detected its cause this tick, else expired and reopened as the next
// episode with the repeat count (episodes so far) at the head of its
// question (repeated replaces the last count).
func (t *tick) lapse(j store.Judgement) {
	if _, still := t.detected[jkey(j.Kind, j.About)]; !still {
		t.expires[j.ID] = "cleared: no step detects its cause at its deadline"
		return
	}
	n := t.episodes(j.Kind, j.About)
	t.expires[j.ID] = fmt.Sprintf("deadline passed unanswered and it still holds: reopened as %s.%s.%d (repeat %d)", j.Kind, j.About, n+1, n)
	times := "TIMES"
	if n == 1 {
		times = "TIME"
	}
	_, question, again := strings.Cut(j.Question, repeated)
	if !again {
		question = j.Question
	}
	t.addJudge(judgement{kind: j.Kind, about: j.About, question: fmt.Sprintf("UNANSWERED %d %s%s%s", n, times, repeated, question)})
}

// repeated ends a reopened judgement's count.
const repeated = ", ESCALATED: "

// cleared is why an open judgement's cause no longer holds in the view, or
// "" while it does.
func (t *tick) cleared(j store.Judgement) string {
	switch j.Kind {
	case "unserved":
		parts := strings.Split(j.About, "+") // the tier, then the routes (unservedAbout)
		for _, n := range t.rowNames {
			if _, resting := t.rested[n]; resting { // a resting row serves no deal (serving)
				continue
			}
			if t.v.Open(t.v.Rows[n]) && t.present(n) && contains(t.tiers(n), parts[0]) && t.in.Roster.ServesRoutes(n, parts[1:]) {
				return "row " + n + " serves " + j.About
			}
		}
	case "cannot-read":
		c, ok := t.v.Cards[j.About]
		if !ok || t.owed(c) <= 0 {
			return j.About + " no longer waits for a read"
		}
		for _, r := range t.serving(c) {
			if r != c.Author {
				return "row " + r + " may read " + j.About
			}
		}
	case "held":
		if c, ok := t.v.Cards[j.About]; !ok || c.State != card.Waiting || !card.HeldReason(c.Reason) {
			return j.About + " is no longer held"
		}
	case "down":
		if r, ok := t.v.Rows[j.About]; !ok || r.State == "up" || !Hooked(r) {
			return "row " + j.About + " is up again or unhooked"
		}
	case "undealt":
		if c, ok := t.v.Cards[j.About]; !ok || c.State != "ready" {
			return j.About + " was dealt or left ready"
		}
	case "admission":
		if len(t.unadmittedIn(j.About)) == 0 {
			return "stream " + j.About + "'s waiting and ready cards pass admission at its base"
		}
	case "stall":
		if at, ok := t.v.Landed[j.About]; ok && at.After(j.OpenedAt) {
			return "stream " + j.About + " landed"
		}
	case "idle":
		if open, working := t.load(); working*4 >= open*3 {
			return "working at or above three quarters of the open width"
		}
	case "land-rate":
		if rate, n := t.landRate(j.About); n < Engine.LandSample || rate >= t.lim.LandTarget {
			return "land rate back at target"
		}
	case "engine":
		if strings.HasPrefix(j.Question, noHead(j.About)) && t.in.Heads[j.About] != "" {
			return "stream " + j.About + " has a head in the tick's inputs again"
		}
		if id := t.aloneChanged(j.Question); id != "" {
			return id + " changed since the store refused its group"
		}
	}
	return t.clearedDeal(j)
}

// clearedDeal is cleared for the deal's own kinds: a tier a model can now be
// named for, the policy gate lifted (or the coordinator's row changed).
func (t *tick) clearedDeal(j store.Judgement) string {
	switch j.Kind {
	case "no-model":
		if t.modelServed(j.About) {
			return "an open row serving " + j.About + " can be named a model"
		}
	case "no-policy":
		if t.o.a.NoDeal == "" {
			return "a policy is active and answered; the run deals"
		}
		if j.About != t.coordinatorRow() {
			return "the coordinator's row is now " + t.coordinatorRow()
		}
	case "base-red":
		return t.clearedBase(j.About)
	case "base-moved":
		return t.clearedMoved(j.About)
	}
	return ""
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// alarms are stall (nothing landed in the bound while producers wait in
// review or merging, per stream), idle (working under the floor of the open
// width while producers wait) and land-rate (a stream's first-time land
// rate under target over the sample).
func (t *tick) alarms() {
	for _, s := range sortedKeys(t.v.Streams) {
		if f := t.streamFacts(s); f.Merging+f.Review > 0 {
			since := t.v.Since
			if at, ok := t.v.Landed[s]; ok && at.After(since) {
				since = at
			}
			if !t.in.Now.Before(since.Add(seconds(t.tmo.LandingStall))) {
				t.judge("stall", s, fmt.Sprintf("stream %s: nothing landed since %s while %d review and %d merging wait", s, since.UTC().Format("15:04:05"), f.Review, f.Merging))
			}
		}
		if rate, n := t.landRate(s); n >= Engine.LandSample && rate < t.lim.LandTarget {
			t.judge("land-rate", s, fmt.Sprintf("stream %s lands first time at %.0f%% over %d; the target is %.0f%%", s, rate*100, n, t.lim.LandTarget*100))
		}
	}
	open, working := t.load()
	waiting := len(t.v.byState["ready"]) + len(t.v.byState["waiting"])
	if open > 0 && waiting > 0 && float64(working) < t.lim.IdleFloor*float64(open) {
		t.judge("idle", "fleet", fmt.Sprintf("working %d of %d open width while %d ready and %d waiting: %s", working, open, len(t.v.byState["ready"]), len(t.v.byState["waiting"]), t.idleCauses()))
	}
}

// load is the open width (up, unheld rows on the bus) and the working
// consumers.
func (t *tick) load() (open, working int) {
	for n, r := range t.v.Rows {
		if t.v.Open(r) && t.present(n) {
			open += t.width(n)
			working += t.v.Working(n)
		}
	}
	return open, working
}

// idleCauses is the idle judgement's facts: the ready producers in held
// streams and with no serving row; when no ready producer is dealable and
// none resolved this tick,
// "nothing dealable" with the chains the waiting ones sit on
// (Waits, as check and the board say them) and the command that moves the
// most. It is built only when the judgement would open.
func (t *tick) idleCauses() string {
	if t.isOpen("idle", "fleet") || t.pending("idle", "fleet") {
		return ""
	}
	held, unserved := 0, 0
	for p := range t.v.byState["ready"] {
		c := t.v.Cards[p]
		switch {
		case t.v.Streams[c.Stream].Held:
			held++
		case len(t.serving(c)) == 0:
			unserved++
		}
	}
	causes := fmt.Sprintf("%d in held streams, %d with no serving row", held, unserved)
	if held+unserved < len(t.v.byState["ready"]) || len(t.resolved) > 0 {
		return causes
	}
	what, fix := IdleWhy(Waits(t.v.Cards))
	if what == "" {
		return "nothing dealable: " + causes
	}
	return "nothing dealable: " + what + "; ready: " + causes + "; " + fix
}

// landRate is a stream's landed with no bounce over landed, and the count.
func (t *tick) landRate(s string) (float64, int) {
	landed, clean := 0, 0
	for p := range t.v.byState["landed"] {
		if c := t.v.Cards[p]; c.Stream == s && c.Kind == "producer" {
			landed++
			if c.Bounces == 0 {
				clean++
			}
		}
	}
	if landed == 0 {
		return 1, 0
	}
	return float64(clean) / float64(landed), landed
}

// judgementOp is the op of the tick's judgements: the opens that ride in no
// group and every expiry, fenced open. False when there is none.
func (t *tick) judgementOp() (store.Op, bool) {
	op := store.Op{Epoch: t.v.Epoch, Actor: Actor, Kind: "judge", At: t.in.Now}
	for _, j := range t.judges {
		if j.episode >= 0 {
			op.Writes = append(op.Writes, t.put(j))
		}
	}
	for _, id := range sortedKeys(t.expires) {
		op.Expect = append(op.Expect, store.JudgementOpen{ID: id})
		op.Writes = append(op.Writes, store.ExpireJudgement{ID: id, Reason: t.expires[id]})
	}
	if len(op.Writes) == 0 {
		return op, false
	}
	op.ID = fmt.Sprintf("j%d.%d.%s", t.v.Epoch, t.v.Seq, hashBody(op.Expect, op.Writes))
	return op, true
}
