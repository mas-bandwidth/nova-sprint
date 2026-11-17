package steps

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
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

// judgeAt asks question about a count that only grows (a row's flaps at n)
// once: when no judgement of kind about about asks it yet (asked). It takes
// the count's own number while that is past every episode there is, else
// the next episode (a lapse reopens under the next number, so the ids run
// ahead of the count): never an id that exists. Every open episode before
// it expires in the same op, so at most one is open (tla/JudgementId.tla
// RaiseC, CountIsAsked).
func (t *tick) judgeAt(kind, about string, n int, question string) {
	t.detect(kind, about)
	if t.pending(kind, about) || t.asked(kind, about, question) {
		return
	}
	episode := max(n, t.nextEpisode(kind, about))
	id := fmt.Sprintf("%s.%s.%d", kind, about, episode)
	for _, old := range t.jindex().ids[jkey(kind, about)] {
		if t.v.Judgements[old].State == "open" {
			t.expires[old] = fmt.Sprintf("reopened at %d as %s", n, id)
		}
	}
	t.addJudge(judgement{kind: kind, about: about, question: question, episode: episode})
}

// asked is whether a judgement of kind about about, open or closed, asks
// question: as it was first asked, or reopened at a deadline (lapse keeps
// the question after the repeat count).
func (t *tick) asked(kind, about, question string) bool {
	for _, id := range t.jindex().ids[jkey(kind, about)] {
		if q := t.v.Judgements[id].Question; q == question || strings.HasSuffix(q, repeated+question) {
			return true
		}
	}
	return false
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

// episodes is the latest episode of kind about about, open or closed: the
// highest episode number there is, 0 for none.
func (t *tick) episodes(kind, about string) int { return t.jindex().last[jkey(kind, about)] }

// nextEpisode is the episode the next judgement of kind about about takes.
// Every place that names a judgement asks here (put, lapse, judgeAt, the
// base-red and base-moved replacements; layer 6 through NextEpisode), so no
// two of them can name one id.
func (t *tick) nextEpisode(kind, about string) int { return t.episodes(kind, about) + 1 }

// NextEpisode is the episode the next judgement of kind about about takes
// among js: one past the highest episode there is, never the number of
// judgements. A kind a count names (flap.<row>.<flaps>) starts past 1, so
// the count of its judgements names an id that exists (24 flap judgements
// numbered 3 to 26 reopened as 25: the op expired and put one judgement,
// the store's check refused it as invalid, and every tick ended there).
// tla/JudgementId.tla NextEp; its witness by-count is the count.
func NextEpisode(js map[string]store.Judgement, kind, about string) int {
	return lastEpisodes(js)[jkey(kind, about)] + 1
}

// lastEpisodes is the highest episode number of each kind and about in js.
func lastEpisodes(js map[string]store.Judgement) map[string]int {
	last := map[string]int{}
	for _, j := range js {
		k := jkey(j.Kind, j.About)
		last[k] = max(last[k], episodeOf(j))
	}
	return last
}

// judgedIndex is the view's judgements by kind and about: their ids, the
// latest open one, and the highest episode number. Built once a tick, on
// first use (the view does not change during a tick).
type judgedIndex struct {
	ids  map[string][]string // sorted
	open map[string]string   // the open one with the highest episode
	last map[string]int
}

// episodeOf is j's episode number, 0 for an id that is not kind.about.<n>
// (a policy failure's): such an id is none a step names, so none collides.
func episodeOf(j store.Judgement) int {
	rest, ok := strings.CutPrefix(j.ID, j.Kind+"."+j.About+".")
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (t *tick) jindex() judgedIndex {
	if t.jidx.open != nil {
		return t.jidx
	}
	t.jidx = judgedIndex{ids: map[string][]string{}, open: map[string]string{}, last: lastEpisodes(t.v.Judgements)}
	for _, id := range sortedKeys(t.v.Judgements) {
		j := t.v.Judgements[id]
		k := jkey(j.Kind, j.About)
		t.jidx.ids[k] = append(t.jidx.ids[k], id)
		if cur, ok := t.jidx.open[k]; j.State == "open" && (!ok || episodeOf(j) >= episodeOf(t.v.Judgements[cur])) {
			t.jidx.open[k] = id
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
		n = t.nextEpisode(j.kind, j.about)
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
// to the coordinator again as NEEDS YOU. A tick leaves at most one episode
// of a kind and about open: every open one but the latest expires as
// superseded, and so does the latest when this tick opens another, so
// several past their deadline reopen as one and the op writes each
// judgement once (tla/JudgementId.tla OneOpenAfterTick, NoDoubleWrite).
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
		latest := t.jindex().open[jkey(j.Kind, j.About)]
		switch why := t.cleared(j); {
		case why != "":
			t.expires[id] = why
		case latest != id:
			t.expires[id] = "superseded by " + latest + ": a later episode of the same cause is open"
		case t.pending(j.Kind, j.About):
			t.expires[id] = fmt.Sprintf("superseded by %s.%s.%d: this tick opens it", j.Kind, j.About, t.nextEpisode(j.Kind, j.About))
		case !j.Deadline.IsZero() && !t.in.Now.Before(j.Deadline):
			t.lapse(j)
		}
	}
}

// lapse is the deadline of open judgement j, the latest of its kind and
// about: closed as cleared when no step detected its cause this tick, else
// expired and reopened as the next episode with the repeat count (how many
// deadlines the question has now passed unanswered: one more than the count
// it carries) at the head of its question (repeated replaces the last
// count).
func (t *tick) lapse(j store.Judgement) {
	if _, still := t.detected[jkey(j.Kind, j.About)]; !still {
		t.expires[j.ID] = "cleared: no step detects its cause at its deadline"
		return
	}
	head, question, again := strings.Cut(j.Question, repeated)
	n := 0
	if !again {
		question = j.Question
	} else if _, err := fmt.Sscanf(head, "UNANSWERED %d", &n); err != nil {
		n = 0 // a question that only holds the words: its first repeat
	}
	n++
	t.expires[j.ID] = fmt.Sprintf("deadline passed unanswered and it still holds: reopened as %s.%s.%d (repeat %d)", j.Kind, j.About, t.nextEpisode(j.Kind, j.About), n)
	times := "TIMES"
	if n == 1 {
		times = "TIME"
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
		return t.clearedUnserved(j.About)
	case "cannot-read":
		c, ok := t.v.Cards[j.About]
		if !ok || t.owed(c) <= 0 {
			return j.About + " no longer waits for a read"
		}
		if rows := t.servingKind(c, "read"); len(rows) > 0 {
			return "row " + rows[0] + " may read " + j.About
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
		return t.clearedIdle()
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

func (t *tick) clearedUnserved(about string) string {
	tier, needs := t.parseUnservedAbout(about)
	for _, n := range t.rowNames {
		if _, resting := t.rested[n]; resting {
			continue
		}
		if t.v.Open(t.v.Rows[n]) && t.present(n) && contains(t.tiers(n), tier) && t.rowFitsMax(n, needs) {
			return "row " + n + " serves " + about
		}
	}
	return ""
}

func (t *tick) clearedIdle() string {
	if t.suppressIdle() {
		return "idle suppressed while bench paused or resource busy"
	}
	if openRows, util := t.fleetUtilization(); openRows == 0 || util >= Engine.IdleCeiling {
		return "fleet utilization at or above ceiling"
	}
	return ""
}

// clearedDeal is cleared for the deal's own kinds: a tier a model can now be
// named for, the policy gate lifted (or the coordinator's row changed); and
// for a judgement about a card that has failed (aboutCard), whatever failed
// it.
func (t *tick) clearedDeal(j store.Judgement) string {
	if c, ok := t.v.Cards[j.About]; ok && c.State == card.Fail && aboutCard(j) {
		return j.About + " failed: nothing is left to decide about it"
	}
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

// aboutCard is whether j's About is a card's id (it is a tier, a stream, a
// row or a base for the other kinds, and a stream or row may be named as a
// card is): the kinds a step opens about a producer, and the engine
// judgement of a merging card with no request. No such judgement stays open
// once its card has failed: nothing it offers can be done to a failed card
// (cleared; Verb answers them in the op that fails the card).
func aboutCard(j store.Judgement) bool {
	switch j.Kind {
	case "held", "cannot-read", "land", "undealt", "brief":
		return true
	case "engine":
		return strings.HasPrefix(j.Question, "card "+j.About+" ")
	}
	return false
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// alarms are stall (nothing landed in the bound less PausedMillis while
// producers wait in review or merging, per stream), idle (fleet utilization
// under the floor across open rows while producers wait, unless suppressed)
// and land-rate (a stream's first-time land rate under target over the sample).
func (t *tick) alarms() {
	for _, s := range sortedKeys(t.v.Streams) {
		if f := t.streamFacts(s); f.Merging+f.Review > 0 {
			since := t.v.Since
			if at, ok := t.v.Landed[s]; ok && at.After(since) {
				since = at
			}
			elapsed := max(time.Duration(0), t.in.Now.Sub(since)-t.streamPaused(s, since))
			if elapsed >= seconds(t.tmo.LandingStall) {
				t.judge("stall", s, fmt.Sprintf("stream %s: nothing landed since %s while %d review and %d merging wait", s, since.UTC().Format("15:04:05"), f.Review, f.Merging))
			}
		}
		if rate, n := t.landRate(s); n >= Engine.LandSample && rate < t.lim.LandTarget {
			t.judge("land-rate", s, fmt.Sprintf("stream %s lands first time at %.0f%% over %d; the target is %.0f%%", s, rate*100, n, t.lim.LandTarget*100))
		}
	}
	openRows, util := t.fleetUtilization()
	waiting := len(t.v.byState["ready"]) + len(t.v.byState["waiting"])
	if openRows > 0 && waiting > 0 && util < t.lim.IdleFloor && !t.suppressIdle() {
		t.judge("idle", "fleet", fmt.Sprintf("fleet utilization %.0f%% (floor %.0f%%, %d open row(s)) while %d ready and %d waiting: %s",
			util*100, t.lim.IdleFloor*100, openRows, len(t.v.byState["ready"]), len(t.v.byState["waiting"]), t.idleCauses()))
	}
}

// streamPaused returns the maximum PausedMillis across consumers of stream s's
// review and merging producers since the last landing (R39b).
func (t *tick) streamPaused(stream string, since time.Time) time.Duration {
	var best time.Duration
	for _, state := range []string{"review", "merging"} {
		for p := range t.v.byState[state] {
			if t.v.Cards[p].Stream != stream {
				continue
			}
			for id := range t.v.consOf[p] {
				c := t.v.Cards[id]
				if isLive(c) || !t.dealtAt(id).Before(since) {
					best = max(best, t.consumerPaused(c.Holder, id))
				}
			}
		}
	}
	return best
}

// fleetUtilization averages row utilization across open, present rows (R54/Q14).
func (t *tick) fleetUtilization() (openRows int, avg float64) {
	sum := 0.0
	for _, n := range t.rowNames {
		r := t.v.Rows[n]
		if !t.v.Open(r) || !t.present(n) {
			continue
		}
		if u, ok := t.rowUtilization(n, r); ok {
			openRows++
			sum += u
		}
	}
	if openRows == 0 {
		return 0, 0
	}
	return openRows, sum / float64(openRows)
}

func (t *tick) rowUtilization(n string, r store.Row) (float64, bool) {
	rr := t.rowOf(n)
	switch rr.Kind {
	case roster.KindKin:
		if rr.Width <= 0 {
			return 0, false
		}
		return min(1.0, float64(t.v.Working(n))/float64(rr.Width)), true
	case roster.KindWorker:
		if rr.Held != "" {
			return 0, false
		}
		totalCPU := rr.Resources["cpu"]
		if totalCPU <= 0 {
			return 0, false
		}
		res := t.in.Reserve[n]
		usedCPU := max(res.ActiveClaimedCPU, t.benchUsed(n)["cpu"])
		return min(1.0, float64(usedCPU)/float64(totalCPU)), true
	}
	return 0, false
}

// suppressIdle returns true when any open present bench is paused/offline or
// when an undealt ready card is waiting on a busy/claimed resource across its
// serving rows (R54).
func (t *tick) suppressIdle() bool {
	for _, n := range t.rowNames {
		r := t.v.Rows[n]
		res := t.in.Reserve[n]
		if t.v.Open(r) && t.present(n) && t.kind(n) == roster.KindWorker && (res.AnyPaused || res.Offline) {
			return true
		}
	}
	for _, p := range t.v.Producers("ready") {
		c := t.v.Cards[p]
		if c.Kind != "producer" || !t.free(p) || t.v.Streams[c.Stream].Held || !t.admitted(p) {
			continue
		}
		serving := t.named(c, t.serving(c))
		if len(serving) == 0 {
			continue
		}
		needs := t.consumerNeeds("work", c.Brief)
		if len(t.withRoomFor(serving, needs)) == 0 {
			return true
		}
	}
	return false
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
