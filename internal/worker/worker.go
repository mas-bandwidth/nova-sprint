// Package worker is a fleet machine's worker: one row of the sprint (a machine's work
// row, or its reader row reader-<machine>) worked by passes. Each pass reads the row's
// queue from the sprint's server, reaps what the sprint took back, reports each lane
// that ended, stamps progress, and takes up to the row's width. It is the loop
// `nova-swarm member` ran, cut to what the live sprint uses (WORKER-INVENTORY.md).
//
// A pass is a step of a state machine with the clock passed in (Tick(now)): every lane
// is one card through taken -> running -> ended -> reported, and a lane leaves the
// worker only by a report the server took, a reap (the sprint took the card back) or a
// stop-return (tla/Worker.tla). Nothing here reads the clock, sleeps or opens a socket:
// the server is reached through Send and the children through a Launcher.
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/member"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
	"github.com/mas-bandwidth/nova-tools/pkg/sprintwire"
	"github.com/mas-bandwidth/nova-tools/pkg/typedrec"
)

// Send delivers a batch of verbs to the sprint's server and returns each one's answer.
type Send func(ctx context.Context, verbs ...[]string) ([]sprintwire.Result, error)

// Lane is one card's child as the pass sees it.
type Lane interface {
	// Poll says whether the lane has ended by now. It first ends a lane whose deadline or
	// token budget is spent by now.
	Poll(now time.Time) bool
	// Result is the ended lane's outcome and, for a work card, its push; it is read once
	// Poll said true. The push is made by the lane, outside the wall, before Poll says
	// true, so no pass waits on git.
	Result() (member.Result, member.Push)
	// Printed is when the child last wrote output; zero when it has not.
	Printed() time.Time
	// Stop ends the lane now: its child and everything it started.
	Stop()
	// Remove deletes what the lane left on disk; keep is true for a failed lane, kept for
	// reading until newer failures push it out.
	Remove(keep bool)
}

// Launcher starts one card's lane. It returns at once: staging and the child run on the
// lane's own, and a lane whose staging fails ends with End staging.
type Launcher interface {
	Start(p member.Packet, now time.Time) (Lane, error)
}

// Config is one row's worker.
type Config struct {
	As       string // the row: the machine's name, or reader-<machine>
	Reader   bool   // the readers table's row: begin asked reads, no beat of its own
	Launcher Launcher
	// Load is the machine's load in percent for the beat; Facts are its gate facts.
	Load  func(now time.Time) float64
	Facts []string
	// Room says whether a card may start (the disk floor); nil is always.
	Room func() (bool, string)
	Out  io.Writer
}

// ProgressEvery is how often a running lane that printed is stamped to the sprint.
const ProgressEvery = 3 * time.Minute

// DrainMost is the longest a stop waits for running lanes before it ends them.
const DrainMost = 2 * time.Minute

// lane states (tla/Worker.tla)
const (
	running = "running"
	ended   = "ended"
)

type lane struct {
	p       member.Packet
	l       Lane
	state   string
	res     member.Result
	push    member.Push
	stamped time.Time // when progress was last stamped
}

// Worker is one row's state across passes.
type Worker struct {
	cfg      Config
	send     Send
	lanes    map[string]*lane // by card id
	width    int
	epoch    uint64
	stopped  bool // the machine is STOPPED
	draining bool
	said     map[string]bool // once-only notes
}

// New is a worker for one row.
func New(cfg Config, send Send) *Worker {
	return &Worker{cfg: cfg, send: send, lanes: map[string]*lane{}, said: map[string]bool{}}
}

// Running is how many lanes the worker holds.
func (w *Worker) Running() int { return len(w.lanes) }

// Drain stops the worker taking cards; it reports each lane as it ends.
func (w *Worker) Drain() { w.draining = true }

// StopAll ends every lane now (a drain's bound passed, or an interrupt) and forgets them:
// the sprint deals their cards again once the row's live set stops naming them.
func (w *Worker) StopAll() int {
	n := 0
	for id, ln := range w.lanes {
		ln.l.Stop()
		ln.l.Remove(true)
		delete(w.lanes, id)
		n++
	}
	return n
}

func (w *Worker) say(format string, a ...any) { fmt.Fprintf(w.cfg.Out, format+"\n", a...) }

func (w *Worker) once(key, format string, a ...any) {
	if !w.said[key] {
		w.said[key] = true
		w.say(format, a...)
	}
}

// ask sends one verb. unanswered is true when the server did not answer it (a transport
// error, or exit 2: the store did not answer), so the caller keeps its state and the next
// pass sends it again.
func (w *Worker) ask(args ...string) (code int, out string, unanswered bool) {
	res, err := w.send(context.Background(), args)
	if err != nil || len(res) != 1 {
		why := "no answer"
		if err != nil {
			why = err.Error()
		}
		return 2, why, true
	}
	r := res[0]
	out = strings.TrimSpace(r.Stdout)
	if r.Code != 0 {
		out = strings.TrimSpace(r.Stderr + " " + r.Stdout)
	}
	return r.Code, out, r.Code == 2
}

type queueCard struct {
	ID      string         `json:"id"`
	Col     string         `json:"col"`
	Gen     int            `json:"gen"`
	Attempt int            `json:"attempt"`
	Packet  *member.Packet `json:"packet"`
}

type queueOut struct {
	Epoch   uint64      `json:"epoch"`
	Width   int         `json:"width"`
	Cards   []queueCard `json:"cards"`
	Reader  *bool       `json:"reader,omitempty"`
	Machine string      `json:"machine,omitempty"`
}

// Beat is the work row's beat: its live set, its load and its facts. A reader has none:
// its pass's queue is its beat.
func (w *Worker) Beat(now time.Time) {
	if w.cfg.Reader {
		return
	}
	args := []string{"fleet", "beat", w.cfg.As, "--live", w.live()}
	if w.draining {
		args = append(args, "--draining")
	}
	if len(w.cfg.Facts) > 0 {
		args = append(args, "--facts", strings.Join(w.cfg.Facts, ","))
	}
	load := 0.0
	if w.cfg.Load != nil {
		load = w.cfg.Load(now)
	}
	args = append(args, "--load", strconv.FormatFloat(load, 'f', 1, 64))
	if code, out, _ := w.ask(args...); code != 0 {
		w.say("NOTE beat: exit %d: %s", code, oneline.Escape(out))
	}
}

// live is the row's live set: <card>@<gen> for each lane, :held once its child ended and
// its report is not yet taken; "-" for none.
func (w *Worker) live() string {
	var ids []string
	for id, ln := range w.lanes {
		s := id + "@" + strconv.Itoa(ln.p.Gen)
		if ln.state == ended {
			s += ":held"
		}
		ids = append(ids, s)
	}
	if len(ids) == 0 {
		return "-"
	}
	slices.Sort(ids)
	return strings.Join(ids, ",")
}

// Tick is one pass at now; acted counts the lanes started, reported, reaped or returned.
func (w *Worker) Tick(now time.Time) (acted int, err error) {
	q, err := w.queue()
	if err != nil {
		return 0, err
	}
	if w.cfg.Reader && q.Reader != nil && !*q.Reader {
		w.once("not-reader", "NOTE %s is not in the readers table: it takes nothing", w.cfg.As)
	}
	if q.Width != w.width {
		w.say("width %d -> %d (nova-config)", w.width, q.Width)
		w.width = q.Width
	}
	w.epoch = q.Epoch
	acted += w.machine(q)
	acted += w.reap(q)
	acted += w.report(now)
	w.progress(now)
	if w.stopped || w.draining {
		return acted, nil
	}
	return acted + w.take(q, now), nil
}

func (w *Worker) queue() (queueOut, error) {
	n := 0
	if w.cfg.Reader && !w.draining {
		n = max(w.width-len(w.lanes), 0)
	}
	args := []string{"queue", "--as", w.cfg.As, "--json", "--packets", strconv.Itoa(n)}
	if len(w.lanes) > 0 {
		var have []string
		for id := range w.lanes {
			have = append(have, id)
		}
		slices.Sort(have)
		args = append(args, "--have", strings.Join(have, ","))
	}
	code, out, _ := w.ask(args...)
	if code != 0 {
		return queueOut{}, fmt.Errorf("queue: exit %d: %s", code, oneline.Escape(out))
	}
	var q queueOut
	if err := json.Unmarshal([]byte(out), &q); err != nil {
		return queueOut{}, fmt.Errorf("queue: not JSON: %v", err)
	}
	return q, nil
}

// StopReturnReason is the stop-return's acknowledgement that the lane was ended.
const StopReturnReason = "owned process stopped"

// machine follows the machine's RUNNING/STOPPED word: on STOPPED every lane is ended and
// handed back with a stop-return; nothing is taken while it stands.
func (w *Worker) machine(q queueOut) (acted int) {
	switch q.Machine {
	case "STOPPED":
		if !w.stopped {
			w.say("MEMBER STOP machine STOPPED: ending %d lane(s), taking nothing", len(w.lanes))
		}
		w.stopped = true
	case "":
	default:
		if w.stopped {
			w.say("MEMBER START machine %s: taking cards again", q.Machine)
		}
		w.stopped = false
	}
	if !w.stopped {
		return 0
	}
	for _, id := range sortedKeys(w.lanes) {
		ln := w.lanes[id]
		ln.l.Stop()
		code, out, unanswered := w.ask("stop-return", "--as", w.cfg.As, id+"@"+strconv.Itoa(ln.p.Gen), "--epoch", strconv.FormatUint(ln.p.Epoch, 10), "--reason", StopReturnReason)
		if unanswered {
			continue // ended, and returned on a later pass
		}
		if code != 0 {
			w.say("NOTE stop-return %s refused: %s", id, oneline.Escape(out))
		}
		w.say("LANE CANCELLED BY STOP card=%s", id)
		ln.l.Remove(true)
		delete(w.lanes, id)
		acted++
	}
	return acted
}

// reap forgets each lane whose card the sprint took back: gone from the queue, or its
// claim (epoch and generation; a read's epoch and attempt) moved. No report is sent.
func (w *Worker) reap(q queueOut) (acted int) {
	byID := map[string]queueCard{}
	for _, c := range q.Cards {
		byID[c.ID] = c
	}
	for _, id := range sortedKeys(w.lanes) {
		ln := w.lanes[id]
		c, ok := byID[id]
		why := ""
		switch {
		case !ok:
			why = "no longer in the queue (dropped or returned)"
		case w.moved(ln, c, q.Epoch):
			why = "the claim moved"
		default:
			continue
		}
		ln.l.Stop()
		ln.l.Remove(false)
		delete(w.lanes, id)
		w.say("reaped %s: %s", id, why)
		acted++
	}
	return acted
}

func (w *Worker) moved(ln *lane, c queueCard, epoch uint64) bool {
	e, gen, attempt := epoch, c.Gen, c.Attempt
	if c.Packet != nil {
		e, gen, attempt = c.Packet.Epoch, c.Packet.Gen, c.Packet.Attempt
	}
	if ln.p.Epoch != e {
		return true
	}
	if ln.p.Kind == "read" && w.cfg.Reader {
		return ln.p.Attempt != attempt
	}
	return ln.p.Gen != gen
}

// report polls each lane and reports the ones that ended. A report the server did not
// answer is sent again next pass; one it refused is said and the lane forgotten: the
// sprint's own rules take the card back.
func (w *Worker) report(now time.Time) (acted int) {
	for _, id := range sortedKeys(w.lanes) {
		ln := w.lanes[id]
		if ln.state == running {
			if !ln.l.Poll(now) {
				continue
			}
			ln.res, ln.push = ln.l.Result()
			ln.state = ended
			switch pu := ln.push; {
			case ln.p.Kind == "read":
			case pu.Sha != "":
				w.say("push %s pushed=%s branch=%s", id, pu.Sha, ln.p.Branch)
			case pu.Refused != "":
				w.say("push %s refused: %s", id, oneline.Escape(pu.Refused))
			}
		}
		args, ok := w.reportArgs(ln)
		code, out, unanswered := w.ask(args...)
		if unanswered {
			continue
		}
		if code != 0 {
			w.say("NOTE %s %s refused (exit %d): %s", args[0], id, code, oneline.Escape(out))
		}
		ln.l.Remove(!ok)
		delete(w.lanes, id)
		acted++
	}
	return acted
}

// reportArgs is the ended lane's report verb and whether it reports success.
func (w *Worker) reportArgs(ln *lane) (args []string, ok bool) {
	p, r := ln.p, ln.res
	epoch := []string{"--epoch", strconv.FormatUint(p.Epoch, 10)}
	if p.Kind == "read" {
		verdict := r.Verdict
		if !r.Ran || (verdict != "ok" && verdict != "broken") || (verdict == "broken" && !typedrec.NamesADefect(findingOf(r))) {
			why := r.Staging
			if why == "" {
				why = oneLine(r.Report)
			}
			reason := cut(fmt.Sprintf("no verdict (ran=%t verdict=%q): %s", r.Ran, verdict, why))
			w.say("read %s: returned: %s", p.Card, reason)
			args = append([]string{"read", "--as", w.cfg.As, "--return", p.Card, "--reason", reason}, usageArgs(r)...)
			return append(append(args, exitArgs(r)...), epoch...), false
		}
		finding := oneLine(r.Report)
		if verdict == "broken" {
			finding = findingOf(r)
		}
		w.say("read %s ok=%t", p.Card, verdict == "ok")
		args = append([]string{"read", "--as", w.cfg.As, "--" + verdict, p.Card, "--finding", finding}, usageArgs(r)...)
		return append(args, epoch...), true
	}
	fin, why := member.Judge(r, ln.push)
	report := oneLine(r.Report)
	if r.Carry != "" {
		report = cut("stage: " + oneLine(r.Carry) + "; " + report)
	}
	if ln.push.Sha != "" {
		report = cut("pushed=" + ln.push.Sha + " to " + p.Branch + ": " + report)
	}
	ok = fin == member.FinishOK
	if !ok {
		report = cut(why + "; " + report)
		w.say("NOTE finish %s failed: %s", p.Card, oneline.Escape(why))
	}
	args = []string{"finish", "--as", w.cfg.As, p.Card + "@" + strconv.Itoa(p.Gen), "--report", member.CarryProposed(report, r)}
	if ln.push.Sha != "" {
		args = append(args, "--head", ln.push.Sha, "--branch", p.Branch)
	}
	if !ok {
		args = append(args, "--failed")
	}
	args = append(append(args, usageArgs(r)...), exitArgs(r)...)
	w.say("finish %s ok=%t exit=%d", p.Card, ok, r.Exit)
	return append(args, epoch...), ok
}

// progress stamps each running lane that printed since its last stamp, at most once in
// ProgressEvery, one verb per epoch.
func (w *Worker) progress(now time.Time) {
	byEpoch := map[uint64][]string{}
	for _, id := range sortedKeys(w.lanes) {
		ln := w.lanes[id]
		if ln.state != running || ln.p.Kind == "read" || now.Sub(ln.stamped) < ProgressEvery {
			continue
		}
		if pr := ln.l.Printed(); pr.IsZero() || !pr.After(ln.stamped) {
			continue
		}
		ln.stamped = now
		byEpoch[ln.p.Epoch] = append(byEpoch[ln.p.Epoch], id+"@"+strconv.Itoa(ln.p.Gen))
	}
	for epoch, ids := range byEpoch {
		args := append(append([]string{"progress", "--as", w.cfg.As}, ids...), "--epoch", strconv.FormatUint(epoch, 10))
		if code, out, _ := w.ask(args...); code != 0 {
			w.say("NOTE progress refused (exit %d): %s", code, oneline.Escape(out))
		}
	}
}

// room is the free capacity: a reader's in whole lanes; a member's in half slots (a read
// card holds one, a work card two) when the queue has a ready read card, else whole.
func (w *Worker) room(q queueOut) int {
	if w.cfg.Reader {
		return w.width - len(w.lanes)
	}
	halves := 0
	for _, ln := range w.lanes {
		halves += 2
		if ln.p.Kind == "read" {
			halves--
		}
	}
	room := 2*w.width - halves
	reads := slices.ContainsFunc(q.Cards, func(c queueCard) bool {
		_, ours := w.lanes[c.ID]
		return !ours && c.Col == "ready" && member.IsReadCardID(c.ID)
	})
	if !reads {
		room /= 2
	}
	return room
}

func (w *Worker) take(q queueOut, now time.Time) (acted int) {
	room := w.room(q)
	if room <= 0 {
		return 0
	}
	if w.cfg.Room != nil {
		if ok, why := w.cfg.Room(); !ok {
			w.once("room:"+why, "take REFUSED: %s", why)
			return 0
		}
	}
	held := []string{"--epoch", strconv.FormatUint(q.Epoch, 10)}
	var packets []member.Packet
	if w.cfg.Reader {
		var ids []string
		for _, c := range q.Cards {
			if _, ours := w.lanes[c.ID]; !ours && c.Col == "asked" && c.Packet != nil && len(ids) < room {
				ids = append(ids, c.ID+"@"+strconv.Itoa(c.Gen))
				packets = append(packets, *c.Packet)
			}
		}
		if len(ids) == 0 {
			return 0
		}
		if code, out, _ := w.ask(append(append([]string{"read", "--as", w.cfg.As, "--begin"}, ids...), held...)...); code != 0 {
			w.say("read --begin refused (exit %d): %s", code, oneline.Escape(out))
			return 0
		}
	} else {
		if !slices.ContainsFunc(q.Cards, func(c queueCard) bool { _, ours := w.lanes[c.ID]; return !ours && c.Col == "ready" }) {
			return 0
		}
		code, out, _ := w.ask(append([]string{"take", "--as", w.cfg.As, "--limit", strconv.Itoa(room), "--json"}, held...)...)
		if code != 0 {
			w.say("take refused (exit %d): %s", code, oneline.Escape(out))
			return 0
		}
		var t struct {
			Packets []member.Packet `json:"packets"`
		}
		if err := json.Unmarshal([]byte(out), &t); err != nil {
			w.say("take: not JSON: %v", err)
			return 0
		}
		packets = t.Packets
	}
	for _, p := range packets {
		if w.start(p, now) {
			acted++
		}
	}
	return acted
}

// start launches a taken card. A launch that cannot start is handed back at once.
func (w *Worker) start(p member.Packet, now time.Time) bool {
	if _, ours := w.lanes[p.Card]; ours {
		return false
	}
	l, err := w.cfg.Launcher.Start(p, now)
	if err != nil {
		epoch := strconv.FormatUint(p.Epoch, 10)
		reason := cut("staging refused: " + oneLine(err.Error()))
		args := []string{"finish", "--as", w.cfg.As, p.Card + "@" + strconv.Itoa(p.Gen), "--failed", "--report", reason, "--epoch", epoch}
		if p.Kind == "read" {
			args = []string{"read", "--as", w.cfg.As, "--return", p.Card, "--reason", reason, "--epoch", epoch}
		}
		w.say("start %s refused: %s", p.Card, oneline.Escape(reason))
		if code, out, _ := w.ask(args...); code != 0 {
			w.say("NOTE %s %s refused (exit %d): %s", args[0], p.Card, code, oneline.Escape(out))
		}
		return false
	}
	w.lanes[p.Card] = &lane{p: p, l: l, state: running, stamped: now}
	route := ""
	if p.Kind != "read" && p.Route != "" {
		route = " route=" + p.Route + " model=" + p.Model
	}
	w.say("start %s attempt=%d gen=%d running=%d/%d%s", p.Card, p.Attempt, p.Gen, len(w.lanes), w.width, route)
	return true
}

// LongestDeadline is the longest deadline of the lanes held, the bound a drain waits.
func (w *Worker) LongestDeadline() time.Duration {
	var d time.Duration
	for _, ln := range w.lanes {
		d = max(d, time.Duration(ln.p.Deadline)*time.Second)
	}
	return d
}

// DrainBound is how long a drain waits for the lanes held: their longest deadline, at
// most DrainMost.
func DrainBound(longest time.Duration) time.Duration { return min(longest, DrainMost) }

func sortedKeys(m map[string]*lane) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

// maxReport is a report's cap, as the sprint keeps it.
const maxReport = 500

func cut(s string) string { return oneline.Cap(s, maxReport) }

// oneLine is the first line of s that is not blank or a heading, cut to the cap.
func oneLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			return cut(l)
		}
	}
	return ""
}

// findingOf is a broken read's finding: its report and its body's lines, one line.
func findingOf(r member.Result) string {
	report := oneLine(r.Report)
	var lines []string
	if report != "" {
		lines = append(lines, report)
	}
	for _, l := range strings.Split(r.Body, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") && l != report {
			lines = append(lines, l)
		}
	}
	return oneline.Cap(strings.Join(lines, " / "), member.MaxFindingBytes)
}

func usageArgs(r member.Result) []string {
	if r.Usage == "" {
		return nil
	}
	return []string{"--usage", r.Usage}
}

// HarnessTailLines is how much of a dead child's output its report carries.
const HarnessTailLines = 40

func exitArgs(r member.Result) []string {
	if r.Exit == 0 {
		return nil
	}
	args := []string{"--exit", strconv.Itoa(r.Exit)}
	if out := strings.TrimRight(r.HarnessOutput, "\n"); out != "" {
		lines := strings.Split(out, "\n")
		args = append(args, "--harness", strings.Join(lines[max(len(lines)-HarnessTailLines, 0):], "\n"))
	}
	return args
}
