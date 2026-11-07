package steps

import (
	"fmt"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// judgement is one a step opens: kind.about.episode, a cause opening once
// (an open one of the same kind and about is not opened again), a group key
// for one push per shared cause (LESSONS 4.9), the deadline policy states
// (none for blocked by Glenn's ruling) and the command that answers it.
type judgement struct {
	kind, about, question string
	episode               int // 0: the next
}

// The closed list a step opens, with the choices judge offers for each.
var choices = map[string]string{
	"blocked": "unblock | drop", "unserved": "retry", "cannot-read": "retry | drop", "conflict": "by-hand | drop",
	"base-red": "retry", "stall": "retry", "idle": "retry", "land-rate": "retry", "flap": "retry", "brief": "rework | drop",
	"landed-sentinel": "retry", "policy": "retry", "engine": "retry", "roster": "retry",
	"land": "land | fix | retry",
}

// judge opens kind about about unless one is open (a cause opens once).
func (t *tick) judge(kind, about, question string) {
	if t.isOpen(kind, about) || t.pending(kind, about) {
		return
	}
	t.judges = append(t.judges, judgement{kind: kind, about: about, question: question})
}

// judgeAgain opens a new episode whether or not one is open (a base's red is
// met again).
func (t *tick) judgeAgain(kind, about, question string) {
	if !t.pending(kind, about) {
		t.judges = append(t.judges, judgement{kind: kind, about: about, question: question})
	}
}

// judgeAt opens episode n of kind about about once: a count that only grows
// (a row's flaps) names its own episode.
func (t *tick) judgeAt(kind, about string, n int, question string) {
	if _, exists := t.v.Judgements[fmt.Sprintf("%s.%s.%d", kind, about, n)]; exists || t.pending(kind, about) {
		return
	}
	t.judges = append(t.judges, judgement{kind: kind, about: about, question: question, episode: n})
}

// engine is the judgement for a defect here or in layer 2: a plan the
// machine or the store refused on fresh state.
func (t *tick) engine(about, why string) { t.judge("engine", about, why) }

func (t *tick) pending(kind, about string) bool {
	for _, j := range t.judges {
		if j.kind == kind && j.about == about {
			return true
		}
	}
	return false
}

func (t *tick) isOpen(kind, about string) bool {
	for _, j := range t.v.Judgements {
		if j.Kind == kind && j.About == about && j.State == "open" {
			return true
		}
	}
	return false
}

// episodes is how many judgements of kind about about exist, open or closed.
func (t *tick) episodes(kind, about string) int {
	n := 0
	for _, j := range t.v.Judgements {
		if j.Kind == kind && j.About == about {
			n++
		}
	}
	return n
}

// riding is the writes of a judgement that rides in a group's op (a block's
// blocked, a sentinel's landed-sentinel), taken out of the judgement op.
func (t *tick) riding(kind, about, question string) []store.Write {
	if t.isOpen(kind, about) || t.pending(kind, about) {
		return nil
	}
	j := judgement{kind: kind, about: about, question: question}
	t.judges = append(t.judges, judgement{kind: kind, about: about, question: question, episode: -1})
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
		Command: fmt.Sprintf("nova-sprint judge %s <choice> --reason <why>   choices: %s", id, choices[j.kind])}
	if j.kind == "policy" {
		w.Group = fmt.Sprintf("policy:v%d", t.o.a.Version)
	}
	if j.kind != "blocked" && j.kind != "land" { // a landing escalated waits for the coordinator: no default lands or fixes
		w.Deadline = t.in.Now.Add(seconds(t.tmo.Judgement))
	}
	return w
}

// judgements is the judgements step (LAYER-3 "Judgements"; the model's jnew
// and Expired): the alarms, one policy judgement per missed question, and
// the expiries: a cause that cleared in the view, or a deadline passed with
// the kind's safe default, which never drops, accepts, merges or lands
// (LESSONS 5.7).
func (t *tick) judgements() {
	t.alarms()
	for _, q := range t.o.missed() {
		t.judge("policy", q, fmt.Sprintf("policy v%d failed question %s: %s; the default answered this tick: run nova-sprint policy check", t.o.a.Version, q, t.o.a.Failed[q]))
	}
	for _, id := range sortedKeys(t.v.Judgements) {
		j := t.v.Judgements[id]
		if j.State != "open" {
			continue
		}
		if why := t.cleared(j); why != "" {
			t.expires[id] = why
		} else if !j.Deadline.IsZero() && !t.in.Now.Before(j.Deadline) {
			t.expires[id] = fmt.Sprintf("deadline passed; %s's safe default: nothing dropped, accepted, merged or landed", j.Kind)
		}
	}
}

// cleared is why an open judgement's cause no longer holds in the view, or
// "" while it does.
func (t *tick) cleared(j store.Judgement) string {
	switch j.Kind {
	case "unserved":
		for _, n := range t.rowNames {
			if t.v.Open(t.v.Rows[n]) && contains(t.tiers(n), j.About) {
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
	case "land":
		if _, stream := t.v.Streams[j.About]; stream {
			if t.streamFacts(j.About).Merging == 0 {
				return "stream " + j.About + " has nothing merging"
			}
		} else if c, ok := t.v.Cards[j.About]; !ok || c.State != "merging" {
			return j.About + " is no longer merging"
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
		if rate, n := t.landRate(j.About); n < Engine.LandSample || rate >= Engine.LandTarget {
			return "land rate back at target"
		}
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
		if rate, n := t.landRate(s); n >= Engine.LandSample && rate < Engine.LandTarget {
			t.judge("land-rate", s, fmt.Sprintf("stream %s lands first time at %.0f%% over %d; the target is %.0f%%", s, rate*100, n, Engine.LandTarget*100))
		}
	}
	open, working := t.load()
	waiting := len(t.v.byState["ready"]) + len(t.v.byState["waiting"])
	if open > 0 && waiting > 0 && float64(working) < Engine.IdleFloor*float64(open) {
		t.judge("idle", "fleet", fmt.Sprintf("working %d of %d open width while %d ready and %d waiting: %s", working, open, len(t.v.byState["ready"]), len(t.v.byState["waiting"]), t.idleCauses()))
	}
}

// load is the open width (up, unheld rows) and the working consumers.
func (t *tick) load() (open, working int) {
	for n, r := range t.v.Rows {
		if t.v.Open(r) {
			open += t.width(n)
			working += t.v.Working(n)
		}
	}
	return open, working
}

// idleCauses counts why producers wait: held streams, tiers no row serves.
func (t *tick) idleCauses() string {
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
	return fmt.Sprintf("%d in held streams, %d with no serving row", held, unserved)
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
