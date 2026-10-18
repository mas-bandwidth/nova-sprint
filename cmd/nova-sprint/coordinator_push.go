package main

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
)

// The coordinator's push, over the bus (docs/SPEC-SPRINT.md, "The hook"; v1.3.6: the
// sprint's own seat hook stream and seat push are retired). The sprint's server sends
// every judgment and note for the coordinator to the coordinator's name on the bus
// (bus2:to:<name>), where nova-hook's bus push puts it into the coordinator's session; and
// it holds the coordinator's sprint push in the nova-hook record while it runs and reaches
// the bus, so the gate (coordinator_hook.go) lets a coordinator command run only while
// the judgments have a way to the seat. The model is tla/CoordinatorPush.tla.
//
// Every log event (and at most coordPushLook on a quiet log) the server looks, on its line, at the inbox, the epoch and the
// machine's line, and then, off the line:
//
//   - delivers: every group for the coordinator (a judgment, or a note addressed to
//     someone: forCoordinator) with a delivery key not told yet (deliveryKeys: each note,
//     and a waited judgment again once its wait is due) goes in one bus message to the
//     seat's holder, or, addressed to someone else (a timer for a friend), to that name;
//     State episodes freeze their message and persist a success receipt on the
//     judgment note; their token makes ambiguous sends safe to retry. Other groups
//     keep their delivery keys in this server's memory. A failed send is sent again at
//     the next look. Keys of groups no longer open are forgotten; a new
//     epoch, or a new holder, starts from nothing told, as a server's start does.
//   - warns: a judgment waiting sprint.JudgmentWarnAfter or longer is warned about at
//     once and when its ID set changes, then at most once a sprint.ReminderEvery
//     (30m) while the set stands. Only successful sends advance that receipt
//     (sprint.JudgmentWarnings, sprint.JudgmentWarnDue; CoordinatorPush.tla).
//   - tells a late tick: the tick running TickLatePush late or more is one message, at
//     most once a sprint.PassEvery (lateWatch, pushlate.go).
//   - holds the sprint push: every hook.RenewEvery, when the holder's record is hooked as
//     the coordinator (the server joins a record, and never arms, hooks or unhooks one),
//     the sprint push's lease is renewed, and proven while the last send to the holder did
//     not fail (unproven while it did). Only the session's answer to nova-hook's challenge
//     proves the coordinator is there; the sprint push proves the way to the session.

// coordPushLook bounds WaitLog and remains the quiet-log retry backstop: a full pass runs at
// least this often, whatever the skip (coordPassSkip) says. It was 5 s until 2026-10-17. The
// sprint push's lease (hook.LeaseTTL, 90 s) is renewed every hook.RenewEvery (30 s) on its own
// clock: the loop's wait ends when the renewal is due (coordPushWait), and the pass renews it
// first, off the line, before anything that waits on the line (renewEarly).
const coordPushLook = 30 * time.Second

// coordPushRetry is the longest the loop waits while something failed: a send to the holder
// (the sprint push is unproven until one goes), a read of the look, a wait on the log. It is
// main's 5 s look, kept for failures: a blip that clears within it is re-proven within it.
const coordPushRetry = 5 * time.Second

// holdRetryCap is the longest wait between two tries of a sprint push renewal that failed:
// the first waits coordPushRetry, each after doubles it, up to this.
const holdRetryCap = 30 * time.Second

// coordPushFloor is the least time between the starts of two passes: a busy log (a line a
// tick) wakes at most one pass a second. Before (2026-10-17), every log line woke a pass.
const coordPushFloor = time.Second

// coordPushMax is the most groups one message names in full; the rest are counted.
const coordPushMax = 20

// coordPush is the server's memory of what it told the coordinator: the holder and the
// epoch it is for, the delivery keys told, and the warning's and the late tick's state.
type coordPush struct {
	mu             sync.Mutex
	logEpoch       uint64
	logCursor      string
	streamCursor   string
	streamPending  []sprint.Note
	holder         string
	epoch          uint64
	told           map[string]bool
	firstSeen      map[string]time.Time
	warned         map[string]bool
	warnedAt       time.Time
	late           lateWatch
	recoveryBefore time.Time // last safe retry of the transport recovery send
	recoveryFrom   string    // frozen sender of the recovery token
	recoveryToken  string    // one transport recovery send per failed episode
	failed         bool      // the last send to the holder failed: the sprint push is unproven
	heldAt         time.Time // the sprint push's last renewal
	heldWhy        string    // what the last renewal said, said again only when it changes
	heldOK         bool      // the last renewal proved the sprint push (else it unproved it)
	holdFailed     bool      // the last renewal could not be made: tried again at holdNext
	holdNext       time.Time // when a failed renewal is tried again
	holdWait       time.Duration
	holdSaid       string // the renewal's failure said, said once until it succeeds or changes
	readOK         bool   // the last pass read its look: only then is the renewal made early

	passPrint string                   // what the last full pass read as it began (coordPassPrint)
	passAt    time.Time                // when the last full pass began
	decisions map[string]decisionRetry // each decision lane's failed delivery, by lane
}

// decisionRetry is a decision lane's failed delivery: the token it was sent under, and when
// it is sent again (a refusal waits, doubling; busRetry).
type decisionRetry struct {
	token string
	busRetry
}

// coordPushLoop wakes on each committed log event. The cursor is captured by
// readCoordLook on the line, before sending: a commit racing a send stays after
// it and wakes the next pass. Never hold the server line while blocking.
// tla/CoordinatorPush.tla LogPush, Look, PushedWithinOneEvent.
func (a *app) coordPushLoop(ctx context.Context) {
	var last time.Time
	for ctx.Err() == nil {
		last = a.coordPushOnce(ctx, last)
	}
}

// coordPushOnce is one turn of the loop: at least coordPushFloor after the last pass began, a
// pass (or its skip, coordPassSkip), then a wait for the next log event. It answers when the
// pass began.
func (a *app) coordPushOnce(ctx context.Context, last time.Time) time.Time {
	if !last.IsZero() {
		if wait := coordPushFloor - a.now().Sub(last); wait > 0 {
			a.sleep(wait)
		}
	}
	began := a.now()
	// the renewal first, before anything that waits on the line (renewEarly)
	a.renewEarly(ctx)
	if !a.coordPassSkip(ctx) && !a.coordinatorPass(ctx) {
		a.coord.mu.Lock()
		a.coord.passPrint = "" // a pass that could not read is never skipped
		a.coord.mu.Unlock()
		if ctx.Err() == nil {
			a.sleep(max(a.coordPushWait(began), coordPushFloor))
		}
		return began
	}
	if ctx.Err() != nil {
		return began
	}
	st, err := a.store(common{redis: a.serveAddr})
	if err == nil {
		a.coord.mu.Lock()
		epoch, cursor := a.coord.logEpoch, a.coord.logCursor
		a.coord.mu.Unlock()
		if wait := a.coordPushWait(began); wait > 0 {
			_, _, err = st.WaitLog(ctx, epoch, cursor, wait)
		}
	}
	if err != nil && ctx.Err() == nil {
		// A failed wait must not busy-spin, nor wait past the renewal or a retry.
		a.sleep(max(a.coordPushWait(began), coordPushFloor))
	}
	return began
}

// coordPushWait is how long the loop waits, for the next log event or after a failure:
// coordPushLook, cut short to when the backstop's full pass is due, the sprint push's renewal
// is due (or a failed renewal's retry), a decision lane's retry is due, and to coordPushRetry
// while a send to the holder has failed or the last look could not be read. Before, a skipped
// pass right before the renewal was due waited a whole coordPushLook, and the lease went near
// two looks unrenewed.
func (a *app) coordPushWait(began time.Time) time.Duration {
	now := a.now()
	p := &a.coord
	p.mu.Lock()
	defer p.mu.Unlock()
	wait := coordPushLook
	if !p.passAt.IsZero() {
		wait = min(wait, p.passAt.Add(coordPushLook).Sub(now))
	}
	if p.failed || !p.readOK {
		wait = min(wait, coordPushRetry)
	}
	if at := p.renewAt(); !at.IsZero() && p.readOK {
		wait = min(wait, at.Sub(now)) // (a pass that cannot read renews nothing: coordPushRetry)
	}
	for _, r := range p.decisions {
		if !r.next.IsZero() {
			wait = min(wait, r.next.Sub(now))
		}
	}
	return wait
}

// coordPassPrint is what a pass depends on that a log event can change, read on the line in
// one short look: the epoch, the seat's holder, the end of the inbox's stream (every note
// written, a stream transition among them) and every open note as stored. It answers the
// print, and the log's end at the epoch for WaitLog.
func (a *app) coordPassPrint(ctx context.Context) (print string, epoch uint64, logCursor string, err error) {
	err = a.onLine(ctx, func(st *store.Store) error {
		es, err := st.EpochNow(ctx)
		if err != nil {
			return err
		}
		epoch = es.N
		backend := st.B
		if epoch != 0 {
			backend = backend.AtEpoch(epoch, false)
		}
		var inbox string
		if logCursor, inbox, err = backend.Tails(ctx); err != nil {
			return err
		}
		holder, err := st.B.Coordinator(ctx)
		if err != nil {
			return err
		}
		open, err := backend.OpenNotes(ctx)
		if err != nil {
			return err
		}
		h := sha256.New()
		fmt.Fprintf(h, "%d\x00%s\x00%s\x00", epoch, holder, inbox)
		for _, o := range open {
			b, err := json.Marshal(o.Note)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "%s\x00", o.Key)
			h.Write(b)
		}
		print = fmt.Sprintf("%x", h.Sum(nil))
		return nil
	})
	return print, epoch, logCursor, err
}

// coordPassSkip says the pass may be skipped, and keeps WaitLog's cursor when it is: what the
// pass reads is as the last full pass began (coordPassPrint), that pass began within
// coordPushLook, and nothing of the pass's own is due (a failed send to the holder, a stream
// notice pending, the sprint push's renewal, a decision lane's retry). Every time-driven duty
// of a pass (a warning, a wait come due, a late tick, a decision's minute) waits at most
// coordPushLook for the next full pass. Before (2026-10-17), every log line ran a full pass:
// two decision lanes each loading the work, readers and fleet, then the inbox loaded again.
// A pass that may not be skipped records what it read as it began.
func (a *app) coordPassSkip(ctx context.Context) bool {
	print, epoch, logCursor, err := a.coordPassPrint(ctx)
	if err != nil {
		return false // the pass reads again, and says why it could not
	}
	now := a.now()
	p := &a.coord
	p.mu.Lock()
	defer p.mu.Unlock()
	// a renewal is due once its interval is over, or after one that failed; with none made
	// (no holder, an unresolved identity's unproving, a new epoch) none is due: a full pass
	// makes it when what it reads changes, and the backstop's pass when it does not
	renew := p.renewDue(now)
	skip := p.passPrint == print && !p.passAt.IsZero() && !now.Before(p.passAt) && now.Sub(p.passAt) < coordPushLook &&
		!p.failed && len(p.streamPending) == 0 && !renew
	for _, r := range p.decisions {
		skip = skip && !r.due(now)
	}
	if skip {
		p.logEpoch, p.logCursor = epoch, logCursor
		return true
	}
	p.passPrint, p.passAt = print, now
	return false
}

// coordLook is what one look read on the line.
type coordLook struct {
	holder       string
	logCursor    string
	streamCursor string
	streamEvents []sprint.Note
	scope        string
	epoch        uint64
	groups       []sprint.Group
	open         []sprint.Open
	machine      string
	stopped      func(time.Time, time.Time) time.Duration
}

// readCoordLook reads the look on the line: the seat's holder, the epoch, the inbox and
// the machine's line.
func (a *app) readCoordLook(ctx context.Context) (coordLook, error) {
	var l coordLook
	err := a.onLine(ctx, func(st *store.Store) error {
		es, err := st.EpochNow(ctx)
		if err != nil {
			return err
		}
		l.epoch = es.N
		backend := st.B
		if l.epoch != 0 {
			backend = backend.AtEpoch(l.epoch, false)
		}
		if l.logCursor, _, err = backend.Tails(ctx); err != nil {
			return err
		}
		if l.holder, err = st.B.Coordinator(ctx); err != nil || l.holder == "" {
			return err
		}
		a.coord.mu.Lock()
		if a.coord.holder == l.holder && a.coord.epoch == l.epoch {
			l.streamCursor = a.coord.streamCursor
			l.streamEvents = append(l.streamEvents, a.coord.streamPending...)
		}
		a.coord.mu.Unlock()
		// Read immutable event notes independently of the human inbox cursor:
		// answering a stop and advancing inbox must not swallow its notification.
		for {
			notes, ids, err := backend.NotesSince(ctx, l.streamCursor, 1000)
			if err != nil {
				return err
			}
			for _, n := range notes {
				if n.StreamTransition != nil {
					l.streamEvents = append(l.streamEvents, n)
				}
			}
			if len(ids) == 0 {
				break
			}
			l.streamCursor = ids[len(ids)-1]
			if len(ids) < 1000 {
				break
			}
		}
		l.scope = a.serveAddr + "\x00" + st.Names.Prefix
		v, err := st.Inbox(ctx, defaultDeadline, defaultStale, 10000)
		if err != nil {
			return err
		}
		l.groups, l.open = v.Groups, v.Open
		for _, o := range v.Open {
			if !o.Note.Review.IsZero() {
				machine, _, err := st.Machine(ctx)
				if err != nil {
					return err
				}
				l.stopped = machine.StoppedBetween
				break
			}
		}
		l.machine = st.MachineLine(ctx)
		return nil
	})
	return l, err
}

// coordinatorPass is one look: read, then deliver, warn, tell a late tick and hold the
// sprint push.
func (a *app) coordinatorPass(ctx context.Context) (read bool) {
	a.renewEarly(ctx)
	a.decisionPass(ctx)
	l, err := a.readCoordLook(ctx)
	a.coord.mu.Lock()
	a.coord.readOK = err == nil
	a.coord.mu.Unlock()
	if err != nil {
		a.serveSay("COORDINATOR PUSH: the inbox could not be read, tried again at the next look: %s", oneline.Escape(err.Error()))
		return
	}
	read = true
	a.coord.mu.Lock()
	a.coord.logEpoch, a.coord.logCursor = l.epoch, l.logCursor
	a.coord.mu.Unlock()
	if l.holder == "" {
		return
	}
	now := a.now()
	login, identityErr := a.coordinatorBusLogin()
	if identityErr != nil {
		a.serveSay("COORDINATOR PUSH: sender identity unresolved: %s", identityErr)
		a.renewSprintPush(ctx, l.holder, false, now)
		return
	}
	sender := cmp.Or(login, l.holder)
	p := &a.coord
	p.mu.Lock()
	if p.holder != l.holder || p.epoch != l.epoch || p.told == nil {
		// Ordinary delivery keys reset for a new holder or epoch; state receipts remain.
		p.holder, p.epoch, p.told = l.holder, l.epoch, map[string]bool{}
		p.failed, p.heldAt, p.recoveryToken, p.recoveryBefore, p.recoveryFrom = false, time.Time{}, "", time.Time{}, ""
		p.holdFailed, p.holdNext, p.holdWait = false, time.Time{}, 0
		p.firstSeen, p.warned, p.warnedAt = map[string]time.Time{}, nil, time.Time{}
	}
	p.streamCursor, p.streamPending = l.streamCursor, l.streamEvents
	sends := coordFresh(p.told, l, now)
	var warn string
	var warnSeen map[string]bool
	if ws := sprint.JudgmentWarnings(coordWarningGroups(l), now, p.firstSeen); len(ws) > 0 {
		seen := make(map[string]bool, len(ws))
		for _, w := range ws {
			seen[w.ID] = true
		}
		if sprint.JudgmentWarnDue(ws, p.warned, p.warnedAt, now) {
			set := sprint.JudgmentWarningSet(ws)
			warn = set + "\n"
			if !maps.Equal(seen, p.warned) {
				warn = sprint.JudgmentWarningText(ws)
			}
		}
		warnSeen = seen
	} else {
		p.warned, p.warnedAt = nil, time.Time{}
	}
	late := p.late.step(l.machine, now)
	p.mu.Unlock()

	failed := false
	attemptedHolder := false
	send := func(to string, m bus.Message) bool {
		m.From, m.To = cmp.Or(m.From, sender), []string{to}
		if why := coordinatorSenderRefusal(m.From, login); why != "" {
			if to == l.holder {
				failed, attemptedHolder = true, true
			}
			a.serveSay("COORDINATOR PUSH: %s", why)
			return false
		}
		if to == l.holder {
			attemptedHolder = true
		}
		if err := a.bus(ctx, m, func(s string) { a.serveSay("%s", s) }); err != nil {
			if busMessageWent(err) {
				// The bus already carried this message under this token: it went, and
				// the token has aged past its life, so the bus will never carry it
				// again. That is a delivery, not a failure: told, and the sprint push
				// stays proven. Counting it as a failure unproved the push on every
				// look and refused every coordinator write, the verb that would have
				// cleared the message included (v1.4.6, 2026-10-16: STREAM STOPPED
				// tool-review-2026-10-15, resent by a restarted server a day later).
				a.serveSay("COORDINATOR PUSH to=%s: %q went under its token before; told: %s", to, m.Subject, oneline.Escape(err.Error()))
				return true
			}
			a.serveSay("COORDINATOR PUSH to=%s: %q could not be sent, sent again at the next look: %s", to, m.Subject, oneline.Escape(err.Error()))
			if to == l.holder {
				failed = true
			}
			return false
		}
		return true
	}
	candidates := map[string]sprint.CoordinatorDelivery{}
	legacy := map[string]sprint.CoordinatorDelivery{}
	for _, o := range l.open {
		if d := o.Note.CoordinatorDelivery; d != nil && !d.Sent && d.From == "" {
			legacy[o.Note.ID] = *d
		}
	}
	legacySenders := map[string]string{}
	legacyErrors := map[string]error{}
	for _, d := range sends {
		if d.note != "" {
			from := sender
			if old, ok := legacy[d.note]; ok {
				var err error
				if !now.Before(old.RetryBefore) || old.RetryBefore.IsZero() {
					err = errors.New("legacy receipt unknown: retry window expired")
				} else {
					key := old.Token
					if _, known := legacySenders[key]; !known && legacyErrors[key] == nil {
						legacySenders[key], legacyErrors[key] = a.coordinatorLegacySender(ctx, old, sender)
					}
					from, err = legacySenders[key], legacyErrors[key]
				}
				if err != nil {
					failed, attemptedHolder = true, true
					a.serveSay("COORDINATOR PUSH note=%s: legacy sender unresolved: %s", d.note, err)
					continue
				}
				old.From = from
				candidates[d.note] = old
				continue
			}
			candidates[d.note] = sprint.CoordinatorDelivery{From: from, To: d.to, Token: d.token, Subject: d.subject, Body: d.text, Episode: d.episode, CoveredBy: d.coveredBy, RetryBefore: now.Add(bus.DefaultTokenLife)}
		}
	}
	prepared := map[string]sprint.CoordinatorDelivery{}
	if len(candidates) > 0 {
		err := a.onLine(ctx, func(st *store.Store) error {
			var err error
			prepared, err = st.CoordinatorDeliveries(ctx, candidates, false, l.epoch)
			return err
		})
		if err != nil {
			failed, attemptedHolder = true, true
			a.serveSay("COORDINATOR PUSH: preparing episode batch: %s", err)
			prepared = nil
		}
	}
	receipts := map[string]sprint.CoordinatorDelivery{}
	attemptedTokens := map[string]bool{}
	blockedStreams := map[string]bool{}
	for _, d := range sends {
		streamRecipient := d.stream + "\x00" + d.to
		if d.stream != "" && blockedStreams[streamRecipient] {
			continue
		}
		frozen, found := prepared[d.note]
		if d.note != "" {
			if !found || frozen.Sent {
				continue
			}
			if frozen.From == "" {
				failed, attemptedHolder = true, true
				a.serveSay("COORDINATOR PUSH note=%s: sender has not been frozen", d.note)
				continue
			}
			if frozen.RetryBefore.IsZero() || !now.Before(frozen.RetryBefore) {
				failed, attemptedHolder = true, true
				a.serveSay("COORDINATOR PUSH note=%s: delivery receipt unknown; retry window ended, inspect the bus and resolve the judgment manually", d.note)
				continue
			}
			if attemptedTokens[frozen.Token] {
				continue
			}
			attemptedTokens[frozen.Token] = true
			d.to, d.subject, d.text, d.token = frozen.To, frozen.Subject, frozen.Body, frozen.Token
		}
		subject := d.subject
		switch {
		case subject != "":
		case d.to == l.holder:
			subject = fmt.Sprintf("nova-sprint: %d new for the coordinator", len(d.groups))
		default:
			subject = fmt.Sprintf("nova-sprint: %d new for %s", len(d.groups), d.to)
		}
		if !send(d.to, bus.Message{From: frozen.From, Kind: d.kind, Subject: subject, Body: d.text, Token: d.token}) {
			if d.stream != "" {
				blockedStreams[streamRecipient] = true
			}
			continue
		}
		if found {
			for id, shared := range prepared {
				if shared.Token == frozen.Token {
					receipts[id] = shared
				}
			}
		}
		p.mu.Lock()
		if p.holder == l.holder && p.epoch == l.epoch {
			for _, k := range d.keys {
				p.told[k] = true
			}
		}
		p.mu.Unlock()
		a.serveSay("COORDINATOR PUSH to=%s groups=%s", d.to, strings.Join(d.groups, ","))
	}
	p.mu.Lock()
	if p.holder == l.holder && p.epoch == l.epoch {
		pending := p.streamPending[:0]
		for _, n := range p.streamPending {
			all := true
			for _, target := range streamRecipients(n, l.holder) {
				all = all && p.told[streamNoticeKey(n, target.to, target.kind)]
			}
			if !all {
				pending = append(pending, n)
			}
		}
		p.streamPending = pending
	}
	p.mu.Unlock()
	if len(receipts) > 0 {
		err := a.onLine(ctx, func(st *store.Store) error {
			_, err := st.CoordinatorDeliveries(ctx, receipts, true, l.epoch)
			return err
		})
		if err != nil {
			a.serveSay("COORDINATOR PUSH: recording episode batch: %s", err)
		}
	}
	if warn != "" {
		if send(l.holder, bus.Message{Kind: bus.KindRequest, Subject: "nova-sprint: judgments waiting on the coordinator", Body: warn}) {
			p.mu.Lock()
			p.warnedAt, p.warned = now, warnSeen
			p.mu.Unlock()
		}
	}
	if late != "" {
		send(l.holder, bus.Message{Kind: bus.KindBlocker, Subject: "nova-sprint: the machine's tick runs late", Body: late})
	}
	p.mu.Lock()
	recover := p.failed && len(sends) == 0 && warn == "" && late == ""
	recoveryToken, recoveryBefore, recoveryFrom := p.recoveryToken, p.recoveryBefore, p.recoveryFrom
	p.mu.Unlock()
	if recover && (recoveryBefore.IsZero() || !now.Before(recoveryBefore)) {
		failed, attemptedHolder = true, true
		a.serveSay("COORDINATOR PUSH: recovery receipt unknown; retry window ended, inspect the bus before restoring delivery")
	} else if recover {
		send(l.holder, bus.Message{From: recoveryFrom, Kind: bus.KindStatus, Token: recoveryToken, Subject: "nova-sprint: coordinator delivery restored", Body: "The previous delivery attempt failed; its work is resolved. This one recovery message confirms that coordinator bus delivery works again."})
	}
	p.mu.Lock()
	if attemptedHolder {
		p.failed = failed
		if failed && p.recoveryToken == "" {
			p.recoveryBefore = now.Add(bus.DefaultTokenLife)
			p.recoveryFrom = sender
			p.recoveryToken = fmt.Sprintf("recovery:%x", sha256.Sum256([]byte(l.scope+l.holder+strconv.FormatUint(l.epoch, 10)+now.UTC().Format(time.RFC3339Nano))))
		}
		if !failed {
			p.recoveryToken, p.recoveryBefore, p.recoveryFrom = "", time.Time{}, ""
		}
	}
	ok := !p.failed
	// due at its interval, after a failed renewal, and at once when this pass's sends changed
	// whether the way to the seat is proven
	// (a failed renewal waits for its retry, whatever changed)
	due := p.renewDue(now) || !p.holdFailed && (p.heldAt.IsZero() || ok != p.heldOK)
	p.mu.Unlock()
	if due {
		a.renewSprintPush(ctx, l.holder, ok, now)
	}
	return

}

// renewSprintPush holds the sprint push of holder, proven when ok (holdSprintPush), and keeps
// when, and what it said, said again only when it changes.
func (a *app) renewSprintPush(ctx context.Context, holder string, ok bool, now time.Time) {
	why, err := a.holdSprintPush(ctx, holder, ok)
	p := &a.coord
	if err != nil {
		// a failed renewal is tried again in coordPushRetry, doubling to holdRetryCap, and
		// said once until it succeeds or its text changes; before, it was tried and said on
		// every pass, a second apart
		p.mu.Lock()
		p.holdFailed = true
		p.holdWait = min(max(2*p.holdWait, coordPushRetry), holdRetryCap)
		p.holdNext = now.Add(p.holdWait)
		why := oneline.Escape(err.Error())
		say := why != p.holdSaid
		p.holdSaid = why
		wait := p.holdWait
		p.mu.Unlock()
		if say {
			a.serveSay("COORDINATOR PUSH: the sprint push of %s could not be held, tried again in %s, the wait doubling to %s (said once until it changes): %s", holder, wait, holdRetryCap, why)
		}
		return
	}
	p.mu.Lock()
	p.heldAt, p.heldOK, p.holdFailed = now, ok, false
	p.holdNext, p.holdWait, p.holdSaid = time.Time{}, 0, ""
	say := why != p.heldWhy
	p.heldWhy = why
	p.mu.Unlock()
	if say {
		a.serveSay("COORDINATOR PUSH: %s", cmp.Or(why, "the sprint push of "+holder+" is held and proven"))
	}
}

// renewAt is when the sprint push's renewal is next due: its interval's end, or a failed
// renewal's retry; zero with none made. The caller holds p.mu.
func (p *coordPush) renewAt() time.Time {
	if p.holdFailed {
		return p.holdNext
	}
	if p.heldAt.IsZero() {
		return time.Time{}
	}
	return p.heldAt.Add(hook.RenewEvery)
}

// renewDue says the renewal is due at now. The caller holds p.mu.
func (p *coordPush) renewDue(now time.Time) bool {
	at := p.renewAt()
	return !at.IsZero() && !now.Before(at)
}

// renewEarly renews the sprint push when its renewal is due, first in a turn of the loop and
// in a pass, before anything that waits on the server's line: a tick holding the line must
// not let the lease lapse. It reads the seat's holder off the line and renews only for the
// holder the last pass held it for (the pass itself renews for a new one), and only when the
// last pass read its look: a pass that cannot read delivers nothing, and its renewal waits for
// a good read, so the lease lapses and the gate refuses, as before the early renewal.
func (a *app) renewEarly(ctx context.Context) {
	now := a.now()
	p := &a.coord
	p.mu.Lock()
	holder, ok := p.holder, !p.failed
	due := holder != "" && p.readOK && p.renewDue(now)
	p.mu.Unlock()
	if !due {
		return
	}
	st, err := a.store(common{redis: a.serveAddr})
	if err != nil {
		return // the pass renews it, or says why it could not
	}
	if current, err := st.B.Coordinator(ctx); err != nil || current != holder {
		return
	}
	a.renewSprintPush(ctx, holder, ok, now)
}

// coordSend is one message the look sends: to whom, its kind, its text, and the groups
// and keys it tells.
type coordSend struct {
	stream                          string // stream notices preserve order for each recipient across retries
	to                              string
	kind                            string
	subject                         string // "" for the batch's own
	text                            string
	groups                          []string
	keys                            []string
	note, token, episode, coveredBy string // state episode: persisted frozen payload and bus retry token
}

// coordFresh is what the look delivers: for each name a group is for (its addressee, a
// note's To, else the seat's holder), one message with the text of every group for it with
// a key not told (the first coordPushMax in full, the rest counted), its kind a request when
// a judgment is in it, else a report, and the keys and groups it names; the holder's first,
// then the others by name; then each judgment that changes a member's or a friend's state
// (sprint.StateJudgment) as a message of its own, kind judgment, its subject naming the
// machine or the friend and the reason. told loses every key no longer open.
func coordFresh(told map[string]bool, l coordLook, now time.Time) []coordSend {
	active := map[string]bool{}
	var transitions []coordSend
	// CoordinatorPush.tla: every committed transition has its own blocker line,
	// plus one status for each affected friend. Keep the judgment in its group.
	for _, n := range l.streamEvents {
		tr := n.StreamTransition
		if tr == nil {
			continue
		}
		subject := fmt.Sprintf("STREAM STOPPED %s cause=%s waiting=%d at=%s", n.Stream, tr.Cause, tr.Waiting, n.At.UTC().Format(time.RFC3339))
		if n.Type == sprint.NStreamResumed {
			subject = fmt.Sprintf("STREAM RESUMED %s -> %s at=%s", n.Stream, tr.State, n.At.UTC().Format(time.RFC3339))
		}
		for _, target := range streamRecipients(n, l.holder) {
			to, kind := target.to, target.kind
			key := streamNoticeKey(n, to, kind)
			active[key] = true
			if told[key] {
				continue
			}
			token := fmt.Sprintf("stream:%x", sha256.Sum256([]byte(l.scope+"\x00"+strconv.FormatUint(l.epoch, 10)+"\x00"+key)))
			transitions = append(transitions, coordSend{stream: n.Stream, to: to, kind: kind, subject: subject,
				text: subject + "\nwho=" + n.Who + " did=" + n.What, keys: []string{key}, token: token})
		}
	}
	byTo := map[string]*coordSend{}
	var order []string
	more := map[string]int{}
	body := map[string]*strings.Builder{}
	var state []coordSend
	notes := make(map[string]sprint.Note, len(l.open))
	for _, o := range l.open {
		notes[o.Note.ID] = o.Note
	}
	for _, g := range l.groups {
		// Review and merge decisions use the existing durable decisions-owed
		// receipt below. Do not send a second, process-memory-only reminder.
		if g.Kind == sprint.Judgment && sprint.RepeatedDecisionType(g.Type) {
			continue
		}
		if g.Kind == sprint.Judgment && len(g.Notes) > 0 && !g.Due.IsZero() && !now.Before(g.Due) {
			// An overdue judgment is the deadline lane's (decisionPass), and its keys stay
			// told: forgetting them here told the whole group again, every note in it, each
			// time the group came back from overdue (2026-10-17: one group told 36 times).
			all, _ := deliveryKeys(g, l.open, now, l.stopped)
			for _, k := range all {
				active[k] = true
			}
			continue
		}
		// The deadline lane replaces the old duplicate overdue reminders.
		if g.Type == sprint.NOverdue || g.Type == sprint.NSeatSilent || g.Type == sprint.NCoordinatorBehind || g.Type == sprint.NRaisedAgain && strings.Contains(g.What, "still holds") {
			continue
		}
		if !forCoordinator(g) || g.Type == sprint.NStatusChanged && (strings.Contains(g.What, " is down ") || strings.Contains(g.What, " is held ")) {
			continue
		}
		to := cmp.Or(g.To, l.holder)
		who, reason, stateJudgment := sprint.StateJudgment(g)
		coveredBy := sprint.StateStatusOwner(g, l.groups)
		if stateJudgment || coveredBy != "" {
			for _, id := range g.Notes {
				n := notes[id]
				if n.ID == "" || n.CoordinatorDelivery != nil && n.CoordinatorDelivery.Sent {
					continue
				}
				episode := sprint.StateEpisodeKey(n)
				token := fmt.Sprintf("state:%x", sha256.Sum256([]byte(l.scope+"\x00"+episode)))
				// The payload is frozen in the note before its first send; later cause
				// rewrites, waits and elapsed clocks cannot change retry arguments.
				state = append(state, coordSend{to: to, kind: bus.KindJudgment, groups: []string{g.ID}, note: id, token: token, episode: episode, coveredBy: coveredBy,
					subject: "nova-sprint: " + cmp.Or(who, "-") + " " + g.Type + ": " + reason,
					text:    "NOVA SPRINT JUDGMENT: " + cmp.Or(who, "-") + "\n" + groupText(g, now, false)})
			}
			continue
		}
		all, eligible := deliveryKeys(g, l.open, now, l.stopped)
		for _, k := range all {
			active[k] = true
		}
		var fresh []string
		for _, k := range eligible {
			if !told[k] {
				fresh = append(fresh, k)
			}
		}
		if len(fresh) == 0 {
			continue
		}
		d := byTo[to]
		if d == nil {
			d = &coordSend{to: to, kind: bus.KindReport}
			byTo[to], body[to] = d, &strings.Builder{}
			order = append(order, to)
		}
		d.keys = append(d.keys, fresh...)
		d.groups = append(d.groups, g.ID)
		if g.Kind == sprint.Judgment {
			d.kind = bus.KindRequest
		}
		if len(d.groups) > coordPushMax {
			more[to]++
			continue
		}
		body[to].WriteString(groupText(g, now, false))
	}
	for k := range told {
		if !active[k] {
			delete(told, k)
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		if hi, hj := order[i] == l.holder, order[j] == l.holder; hi != hj {
			return hi
		}
		return order[i] < order[j]
	})
	var out []coordSend
	for _, to := range order {
		d := byTo[to]
		if n := more[to]; n > 0 {
			fmt.Fprintf(body[to], "and %d more: nova-sprint inbox\n", n)
		}
		head := "NOVA SPRINT INBOX: new for the coordinator\n"
		if to != l.holder {
			head = "NOVA SPRINT INBOX: new for " + to + "\n"
		}
		d.text = head + body[to].String()
		sort.Strings(d.keys)
		out = append(out, *d)
	}
	return append(transitions, append(out, state...)...)
}

type streamRecipient struct{ to, kind string }

func streamRecipients(n sprint.Note, holder string) []streamRecipient {
	out := []streamRecipient{{holder, bus.KindBlocker}}
	seen := map[string]bool{}
	if n.StreamTransition != nil {
		for _, name := range n.StreamTransition.Owners {
			if !seen[name] {
				out = append(out, streamRecipient{name, bus.KindStatus})
				seen[name] = true
			}
		}
	}
	return out
}

// The kind and immutable note ID distinguish stop/resume and recurrence even
// when the store commits both in the same timestamp precision.
func streamNoticeKey(n sprint.Note, to, kind string) string {
	return "stream\x00" + n.Stream + "\x00" + n.At.UTC().Format(time.RFC3339Nano) + "\x00" + n.Type + "\x00" + n.ID + "\x00" + to + "\x00" + kind
}

// deliveryKeys identifies each open note separately, including its explicit wait
// deadline. A changed Review/ReviewSet is a new delivery generation, so a wait set on a
// judgment that was told sends it again once its wait is due.
func deliveryKeys(g sprint.Group, open []sprint.Open, now time.Time, stopped func(time.Time, time.Time) time.Duration) (active, eligible []string) {
	keys := noteKeys(g)
	if g.HookKey != "" {
		keys = []string{g.HookKey}
	}
	if g.Kind != sprint.Judgment {
		return keys, keys
	}
	notes := make(map[string]sprint.Note, len(open))
	for _, o := range open {
		notes[noteKey(o.Note.ID)] = o.Note
	}
	for _, key := range keys {
		n, ok := notes[key]
		if ok && !n.Review.IsZero() {
			key += "\x00review=" + n.Review.UTC().Format(time.RFC3339Nano) + "\x00set=" + n.ReviewSet.UTC().Format(time.RFC3339Nano)
		}
		active = append(active, key)
		if ok && !n.Review.IsZero() && !sprint.DueNow(now, n.Review, n.ReviewSet, stopped) {
			continue
		}
		eligible = append(eligible, key)
	}
	return active, eligible
}

// sprintPushBy is who holds the sprint push, as the lease names it: this server.
func (a *app) sprintPushBy() string {
	return "nova-sprint run " + cmp.Or(a.serveAddr, "-") + " pid " + strconv.Itoa(os.Getpid())
}

// holdSprintPush renews name's sprint push in its nova-hook record, and proves it when ok
// (unproves it when not), when the record is hooked as the coordinator; why says what was
// not held and why, "" when it was. The server joins the record the identity hooked; it
// never arms, hooks or unhooks one (an unhook stands).
func (a *app) holdSprintPush(ctx context.Context, name string, ok bool) (string, error) {
	hs, err := a.hookStore(ctx)
	if err != nil {
		return "", err
	}
	st, err := a.hookStatus(ctx, name)
	if err != nil {
		return "", err
	}
	if st.State == hook.StateNone || st.State == hook.StateUnhooked || st.Role != hook.Coordinator {
		return fmt.Sprintf("the sprint push of %s is not held: its record is %s (the server holds it only for a record hooked as the coordinator: %s)", name, st.Words(), hook.HookCommand(name, hook.Coordinator)), nil
	}
	by := a.sprintPushBy()
	err = hs.Renew(ctx, name, hook.Coordinator, hook.Sprint, st.Epoch, by)
	if err == nil {
		if ok {
			err = hs.Prove(ctx, name, hook.Coordinator, hook.Sprint, st.Epoch, by)
		} else {
			err = hs.Unprove(ctx, name, hook.Coordinator, hook.Sprint, st.Epoch)
		}
	}
	if errors.Is(err, hook.ErrNotHooked) {
		return "the sprint push of " + name + " is not held: the record moved since it was read (unhooked, or hooked again); the next look reads it again", nil
	}
	if err != nil {
		a.hookDrop()
		return "", err
	}
	if !ok {
		return "the sprint push of " + name + " is held and unproven: the last send to it failed", nil
	}
	return "", nil
}

// A status audit covered by a causal episode remains quiet after that causal
// note closes; only a new status transition clears the durable coverage.
func coordWarningGroups(l coordLook) []sprint.Group {
	covered := map[string]bool{}
	for _, o := range l.open {
		if d := o.Note.CoordinatorDelivery; d != nil && d.CoveredBy != "" {
			covered[o.Note.ID] = true
		}
	}
	var groups []sprint.Group
	for _, g := range l.groups {
		if g.Kind == sprint.Judgment && sprint.RepeatedDecisionType(g.Type) {
			continue
		}
		skip := false
		for _, id := range g.Notes {
			skip = skip || covered[id]
		}
		if !skip {
			groups = append(groups, g)
		}
	}
	return groups
}

// decisionPass is the deadline escalation lane. The owner lane goes to whoever holds
// the coordinator seat at push time, like the coordinator lane (owner-pushes-route-to-coordinator).
// The frozen payload/token, success receipt and fault are durable, unlike the pre-deadline
// reminder memory.
// tla/DecisionsOwed.tla NoSilentPile means an outstanding delivery obligation,
// not human sight; transport liveness assumes eventual successful sends.
//
// Both lanes are prepared on one hold of the line, from one read of the epoch, the seat and
// the machine, and the open notes are read once after (before, 2026-10-17, each lane read all
// of it on a hold of its own, every pass); a lane whose seat cannot be read, or whose prepare
// fails, is said and the other lane goes on. When the coordinator lane owes a send, the owner
// lane is prepared after that send on a hold of its own, so a same-recipient owner set folds
// into the coordinator's notice (v17-decisions-owed-folds-into-one-notice) and is suppressed
// while that notice's receipt is unknown. A send the bus refuses, and a fault of the sender's
// own (no channel: a seat with no holder; an unresolved identity), waits before it is tried
// again (busRetry: a minute, doubling, up to thirty; tla/DecisionsOwed.tla RefuseOwner models
// the owner lane's), and every lane's failure is said once until its text changes.
func (a *app) decisionPass(ctx context.Context) {
	login, loginErr := a.coordinatorBusLogin()
	jobs := map[string]sprint.Note{}
	failed := map[string]error{} // a lane whose prepare failed: the other goes on, as before
	var epoch uint64
	// prepare runs the prepare step of each lane in lanes on one hold of the line, and finds
	// each lane's owed delivery; with deferOwner, the owner lane is left for later when the
	// coordinator lane owes a send. It says whether the owner lane was left.
	prepare := func(lanes []string, fold *decisionFold, deferOwner bool) (deferred bool, err error) {
		err = a.onLine(ctx, func(st *store.Store) error {
			deferred = false
			es, err := st.EpochNow(ctx)
			if err != nil {
				return err
			}
			epoch = es.N
			// a seat with no holder is the "no channel" fault, which waits as a refusal does
			// (decisionSend): its delivery is still made, transferred and closed
			coordinator, coordinatorErr := st.B.Coordinator(ctx)
			sender := cmp.Or(login, coordinator, "nova-sprint")
			if loginErr != nil {
				sender = ""
			}
			machine, _, err := st.Machine(ctx)
			if err != nil {
				return err
			}
			// one step a lane: a step's notes of one type merge into one (sprint.MergeNotes), and
			// each lane keeps a delivery note of its own; the second step's load reads only what
			// changed since the first (the store's read twin)
			owes := map[string]bool{}
			var prepared []string
			for _, lane := range lanes {
				if lane == "owner" && deferOwner && owes["coordinator"] {
					deferred = true
					continue
				}
				prepared = append(prepared, lane)
				to, err := coordinator, coordinatorErr
				if err == nil {
					_, err = st.Run(ctx, store.Step{Verb: "decision escalation prepare", Load: []string{sprint.Work, sprint.Readers, sprint.Fleet}, Routes: true, Readers: true, Friends: true, Actor: sprint.MachineActor, Epoch: &epoch, Plan: func(s *sprint.Snapshot) sprint.Plan {
						delete(jobs, lane) // a plan made again (a retried step) starts over
						var laneFold *decisionFold
						if lane == "owner" {
							laneFold = fold
						}
						p := decisionPlan(s, lane, to, sender, login, loginErr, st.Names.Prefix, epoch, machine.StoppedBetween, jobs, laneFold)
						owes[lane] = jobs[lane].DecisionDelivery != nil || planOwesSend(p, lane)
						return p
					}})
				}
				if err != nil {
					failed[lane] = err
					delete(jobs, lane)
					owes[lane] = false
				}
			}
			all, err := st.B.OpenNotes(ctx)
			if err != nil {
				return err
			}
			for _, lane := range prepared {
				if failed[lane] != nil {
					continue
				}
				for _, o := range all {
					if d := o.Note.DecisionDelivery; o.Note.Type == sprint.NDecisionDelivery && d != nil && d.Lane == lane && d.SentAt.IsZero() {
						jobs[lane] = o.Note
						break
					}
				}
			}
			return nil
		})
		return deferred, err
	}
	send := func(lane string, err error) decisionFold {
		why := err
		if why == nil {
			why = failed[lane]
		}
		if why != nil {
			a.serveSay("DECISIONS OWED: prepare %s failed: %s", lane, why)
			return decisionFold{}
		}
		if job := jobs[lane]; job.DecisionDelivery != nil {
			return a.decisionSend(ctx, lane, job, login, loginErr, epoch)
		}
		a.coord.mu.Lock()
		delete(a.coord.decisions, lane)
		a.coord.mu.Unlock()
		return decisionFold{}
	}
	ownerDeferred, err := prepare([]string{"coordinator", "owner"}, nil, true)
	fold := send("coordinator", err)
	if ownerDeferred {
		_, err = prepare([]string{"owner"}, &fold, false)
	}
	send("owner", err)
}

// decisionFold is what the coordinator lane's send this pass tells the owner lane's prepare:
// the notice's recipient, set and token, whether a send was owed (attempted), whether its
// receipt was recorded (received), and whether the owner lane's set at the receipt was the
// same as the notice's, with no owner delivery pending (foldOwner), and that set's reports.
type decisionFold struct {
	to           string
	set          string
	token        string
	attempted    bool
	received     bool
	foldOwner    bool
	ownerReports []string
}

// planOwesSend says the plan makes or keeps a delivery note of the lane not yet sent.
func planOwesSend(p sprint.Plan, lane string) bool {
	for _, n := range append(append([]sprint.Note{}, p.Notes...), p.Updates...) {
		if d := n.DecisionDelivery; n.Type == sprint.NDecisionDelivery && d != nil && d.Lane == lane && d.SentAt.IsZero() {
			return true
		}
	}
	return false
}

// decisionPlan is one lane's part of the prepare step: its delivery note made, renewed,
// transferred or closed, and the note owed a send, if one is, put in jobs. fold, for the
// owner lane after a coordinator send this pass, folds a same-recipient owner set into the
// coordinator's notice: its receipt is copied to the owner lane, and no second notice is made.
func decisionPlan(s *sprint.Snapshot, lane, to, sender, login string, loginErr error, prefix string, epoch uint64, stopped func(time.Time, time.Time) time.Duration, jobs map[string]sprint.Note, fold *decisionFold) sprint.Plan {
	var p sprint.Plan
	all := append(append([]sprint.Open{}, s.Open...), s.Acked...)
	set, body, reports := sprint.DecisionBatch(s.Now, all, sprint.DefaultHeldDecisions(s), lane, stopped, s)
	var old *sprint.Note
	var oldOpen []sprint.Open
	for _, o := range s.Acked {
		if o.Note.Type == sprint.NDecisionDelivery && o.Note.DecisionDelivery != nil && o.Note.DecisionDelivery.Lane == lane {
			n := o.Note
			old = &n
			oldOpen = append(oldOpen, o)
		}
	}
	if old != nil {
		d := old.DecisionDelivery
		// A failed send keeps its exact payload, even if the open set changes.
		if d.SentAt.IsZero() {
			if d.To != to || loginErr == nil && coordinatorSenderRefusal(d.From, login) != "" {
				transferred := *d
				transferred.To = to
				transferred.From = sender
				transferred.Token = fmt.Sprintf("decisions:%x", sha256.Sum256([]byte(d.Token+"recipient="+to+s.Now.UTC().Format(time.RFC3339Nano))))
				transferred.RetryBefore = s.Now.Add(bus.DefaultTokenLife)
				transferred.UnknownReceipts++
				transferred.Fault = "earlier recipient receipt unknown; frozen obligation transferred to current recipient"
				old.DecisionDelivery = &transferred
				d = &transferred
				old.What = transferred.Fault + "; judgment IDs=" + strings.ReplaceAll(d.Set, "\x00", ",")
				p.Updates = []sprint.Note{*old}
			}
			if d.From == "" && loginErr == nil {
				resolved := *d
				resolved.From = sender
				old.DecisionDelivery = &resolved
				d = &resolved
				p.Updates = []sprint.Note{*old}
			}
			if !s.Now.Before(d.RetryBefore) {
				renewed := *d
				renewed.UnknownReceipts++
				renewed.Token = fmt.Sprintf("decisions:%x", sha256.Sum256([]byte(d.Token+s.Now.UTC().Format(time.RFC3339Nano))))
				renewed.RetryBefore = s.Now.Add(bus.DefaultTokenLife)
				renewed.Fault = "receipt unknown after token expiry; retrying with renewed token (duplicate identifiable by judgment IDs)"
				old.DecisionDelivery = &renewed
				old.What = renewed.Fault + "; judgment IDs=" + strings.ReplaceAll(renewed.Set, "\x00", ",")
				p.Updates = []sprint.Note{*old}
			}
			jobs[lane] = *old
			return p
		}
		// Owner cadence is global to the lane, including changed/empty sets
		// and recipient changes. Keep the receipt until the window ends.
		ownerFold := lane == "owner" && fold != nil && fold.attempted && fold.received && fold.foldOwner && fold.to == to
		if lane == "owner" && s.Now.Sub(d.SentAt) < 30*time.Minute && !ownerFold {
			return p
		}
		if set == "" {
			if d.UnknownReceipts > 0 {
				history := *old
				history.ID = ""
				history.Alias = ""
				history.Type = "decision escalation receipt history"
				p.Notes = append(p.Notes, history)
			}
			p.Closes = oldOpen
			return p
		}
		if lane == "coordinator" && d.To == to && d.Set == set {
			if !sprint.RepeatedDecisionSet(s, set) || s.Now.Sub(d.SentAt) < sprint.RepeatedDecisionEvery {
				return p
			}
		}
	}
	if set == "" {
		return p
	}
	if lane == "owner" && fold != nil && fold.attempted && fold.to == to && (fold.foldOwner || !fold.received && fold.set == set) {
		if !fold.received {
			return p
		}
		var coordinatorNote *sprint.Note
		for _, o := range s.Acked {
			d := o.Note.DecisionDelivery
			if o.Note.Type == sprint.NDecisionDelivery && d != nil && d.Lane == "coordinator" &&
				d.To == to && d.Set == set && d.Token == fold.token && !d.SentAt.IsZero() {
				n := o.Note
				coordinatorNote = &n
				break
			}
		}
		if coordinatorNote == nil {
			return p
		}
		folded := *coordinatorNote.DecisionDelivery
		folded.Lane = "owner"
		folded.Reports = fold.ownerReports
		folded.UnknownReceipts = 0
		folded.Fault = ""
		n := sprint.Note{
			Kind: sprint.Acknowledged, Type: sprint.NDecisionDelivery, SprintLevel: true,
			Who: sprint.MachineActor, At: s.Now, DecisionDelivery: &folded,
			What: "folded coordinator receipt; judgment IDs=" + strings.ReplaceAll(folded.Set, "\x00", ","),
		}
		if old != nil {
			if old.DecisionDelivery.UnknownReceipts > 0 {
				history := *old
				history.ID = ""
				history.Alias = ""
				history.Type = "decision escalation receipt history"
				p.Notes = append(p.Notes, history)
			}
			n.ID, n.Alias = old.ID, old.Alias
			p.Updates = append(p.Updates, n)
		} else {
			p.Notes = append(p.Notes, n)
		}
		return p
	}
	token := fmt.Sprintf("decisions:%x", sha256.Sum256([]byte(prefix+strconv.FormatUint(epoch, 10)+lane+to+set+s.Now.UTC().Format(time.RFC3339Nano))))
	d := &sprint.DecisionDelivery{Lane: lane, To: to, From: sender, Set: set, Body: body, Reports: reports, Token: token, PreparedAt: s.Now, RetryBefore: s.Now.Add(bus.DefaultTokenLife)}
	n := sprint.Note{Kind: sprint.Acknowledged, Type: sprint.NDecisionDelivery, SprintLevel: true, Who: sprint.MachineActor, At: s.Now, DecisionDelivery: d}
	if old != nil {
		if old.DecisionDelivery.UnknownReceipts > 0 {
			history := *old
			history.ID = ""
			history.Alias = ""
			history.Type = "decision escalation receipt history"
			p.Notes = append(p.Notes, history)
		}
		n.ID, n.Alias = old.ID, old.Alias
		p.Updates = append(p.Updates, n)
	} else {
		p.Notes = append(p.Notes, n)
	}
	return p
}

// decisionSend sends a lane's owed delivery and records its receipt, or its fault. A send the
// bus refused waits before the next (busRetry); while it waits, nothing is sent, recorded or
// said. A fault the note holds already is not recorded again. For the coordinator lane it
// says what the owner lane's prepare folds (decisionFold).
func (a *app) decisionSend(ctx context.Context, lane string, job sprint.Note, login string, loginErr error, epoch uint64) (fold decisionFold) {
	now := a.now()
	d := *job.DecisionDelivery
	a.coord.mu.Lock()
	retry := a.coord.decisions[lane]
	a.coord.mu.Unlock()
	if retry.token != d.Token {
		retry = decisionRetry{token: d.Token} // another payload: another send
	}
	if lane == "coordinator" {
		fold = decisionFold{to: d.To, set: d.Set, token: d.Token, attempted: true}
	}
	if !retry.due(now) {
		return fold
	}
	fault := ""
	var sendErr error
	// the sender's own faults stand until someone changes the configuration: they wait as a
	// refusal does (permanentFault); an expired token is renewed by the next look's prepare
	permanent := func(why string) { fault, sendErr = why, permanentFault{errors.New(why)} }
	switch {
	case d.To == "":
		permanent("no " + lane + " channel is configured")
	case loginErr != nil:
		permanent("sender identity unresolved: " + loginErr.Error())
	case coordinatorSenderRefusal(d.From, login) != "":
		permanent(coordinatorSenderRefusal(d.From, login))
	case !now.Before(d.RetryBefore):
		fault = "delivery receipt unknown: retry token expired; renewal due next look"
	default:
		if d.From == "" {
			d.From = login
		}
		// a send that went under its token before is a delivery (busMessageWent); one whose
		// token went with other arguments is another message, sent under a token of its own
		// (busRetoken), as the seat-silent raise is
		m := bus.Message{From: d.From, To: []string{d.To}, Kind: bus.KindBlocker, Subject: "nova-sprint: decisions owed", Body: d.Body, Token: d.Token}
		err := a.bus(ctx, m, func(s string) { a.serveSay("%s", s) })
		if busNeedsNewToken(err) {
			m.Token = busRetoken(d.Token, d.To, d.From, d.Body)
			err = a.bus(ctx, m, func(s string) { a.serveSay("%s", s) })
		}
		if err != nil && !busMessageWent(err) {
			fault, sendErr = err.Error(), err
		}
	}
	if fault == "" || fault != d.Fault {
		receiptApplied := false
		err := a.onLine(ctx, func(st *store.Store) error {
			receiptTo, err := st.B.Coordinator(ctx)
			if err != nil {
				return err
			}
			machine, _, err := st.Machine(ctx)
			if err != nil {
				return err
			}
			_, err = st.Run(ctx, store.Step{Verb: "decision escalation receipt", Actor: sprint.MachineActor, Epoch: &epoch, Plan: func(s *sprint.Snapshot) sprint.Plan {
				return decisionReceiptPlan(s, lane, job, d, receiptTo, fault, machine.StoppedBetween, &fold, &receiptApplied)
			}})
			return err
		})
		if err != nil {
			a.serveSay("DECISIONS OWED: receipt %s failed: %s", lane, err)
		} else if lane == "coordinator" && fault == "" && receiptApplied {
			fold.received = true
		}
	}
	a.coord.mu.Lock()
	defer a.coord.mu.Unlock()
	if fault == "" {
		delete(a.coord.decisions, lane)
		return fold
	}
	if sendErr == nil {
		sendErr = errors.New(fault)
	}
	say := retry.failed(now, sendErr)
	if a.coord.decisions == nil {
		a.coord.decisions = map[string]decisionRetry{}
	}
	a.coord.decisions[lane] = retry
	if say {
		when := "tried again at the next look"
		if !retry.next.IsZero() {
			when = "refused: tried again in " + retry.wait.String() + ", the wait doubling while it stands"
		}
		a.serveSay("DECISIONS OWED: %s delivery pending (%s; said once until it changes): %s", lane, when, fault)
	}
	return fold
}

// decisionReceiptPlan is the receipt step's Plan: it records the delivery's receipt on the
// note job names, and for a coordinator receipt whose recipient is still the same, folds the
// owner lane's set into the coordinator's notice (the owner lane's next prepare copies the
// receipt). The engine calls a step's Plan once per attempt, so the captured state this plan
// assigns is reset at the top of every call: a call that does not assign a field (a later
// attempt on a changed world) must leave it at its value before the step, never at an earlier
// attempt's.
func decisionReceiptPlan(s *sprint.Snapshot, lane string, job sprint.Note, d sprint.DecisionDelivery, receiptTo, fault string, stopped func(time.Time, time.Time) time.Duration, fold *decisionFold, receiptApplied *bool) sprint.Plan {
	fold.foldOwner = false
	fold.ownerReports = nil
	*receiptApplied = false
	var p sprint.Plan
	for _, o := range s.Acked {
		n := o.Note
		if n.ID != job.ID || n.DecisionDelivery == nil || n.DecisionDelivery.Token != d.Token {
			continue
		}
		next := *n.DecisionDelivery
		var ownerReports []string
		foldOwner := false
		if lane == "coordinator" && d.To == receiptTo {
			all := append(append([]sprint.Open{}, s.Open...), s.Acked...)
			ownerSet, _, reports := sprint.DecisionBatch(s.Now, all, sprint.DefaultHeldDecisions(s), "owner", stopped, s)
			ownerReports = reports
			ownerPending := false
			for _, pending := range s.Acked {
				pd := pending.Note.DecisionDelivery
				if pending.Note.Type == sprint.NDecisionDelivery && pd != nil && pd.Lane == "owner" && pd.SentAt.IsZero() {
					ownerPending = true
					break
				}
			}
			foldOwner = ownerSet == d.Set && !ownerPending
			fold.foldOwner = foldOwner
			fold.ownerReports = append([]string(nil), reports...)
		}
		next.Fault = fault
		if fault == "" {
			next.SentAt = s.Now
			if next.UnknownReceipts > 0 {
				next.Fault = fmt.Sprintf("%d earlier receipt(s) unknown; latest renewed notice accepted; duplicate identifiable by judgment IDs", next.UnknownReceipts)
			}
		}
		if next.Fault == n.DecisionDelivery.Fault && next.SentAt.Equal(n.DecisionDelivery.SentAt) {
			return p
		}
		n.DecisionDelivery = &next
		n.What = next.Fault + "; judgment IDs=" + strings.ReplaceAll(next.Set, "\x00", ",")
		p.Updates = append(p.Updates, n)
		*receiptApplied = true
		if fault == "" {
			for _, r := range s.Acked {
				coordinatorReport := lane == "coordinator" && slices.Contains(d.Reports, r.Note.ID)
				ownerReport := lane == "owner" && slices.Contains(d.Reports, r.Note.ID) ||
					lane == "coordinator" && foldOwner && slices.Contains(ownerReports, r.Note.ID)
				if !coordinatorReport && !ownerReport {
					continue
				}
				report := r.Note
				if coordinatorReport {
					report.DefaultCoordinatorTold = true
				}
				if ownerReport {
					report.DefaultOwnerTold = true
				}
				p.Updates = append(p.Updates, report)
			}
		}
		break
	}
	return p
}

// busMessageWent says the bus refused a send because the message already went under
// its token and the token has aged past its life (pkg/bus replay): the message is on
// the bus, and no retry will ever carry it again.
func busMessageWent(err error) bool {
	var r *bus.Refusal
	if !errors.As(err, &r) {
		return false
	}
	for _, p := range r.Problems {
		if strings.Contains(p, "the message went, and is not sent again") {
			return true
		}
	}
	return false
}
