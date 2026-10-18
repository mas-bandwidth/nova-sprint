package main

import (
	"cmp"
	"context"
	"crypto/sha256"
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

// coordPushLook bounds WaitLog and remains the quiet-log retry backstop.
const coordPushLook = 5 * time.Second

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
}

// coordPushLoop wakes on each committed log event. The cursor is captured by
// readCoordLook on the line, before sending: a commit racing a send stays after
// it and wakes the next pass. Never hold the server line while blocking.
// tla/CoordinatorPush.tla LogPush, Look, PushedWithinOneEvent.
func (a *app) coordPushLoop(ctx context.Context) {
	for ctx.Err() == nil {
		if !a.coordinatorPass(ctx) {
			if ctx.Err() == nil {
				a.sleep(coordPushLook)
			}
			continue
		}
		if ctx.Err() != nil {
			return
		}
		st, err := a.store(common{redis: a.serveAddr})
		if err == nil {
			a.coord.mu.Lock()
			epoch, cursor := a.coord.logEpoch, a.coord.logCursor
			a.coord.mu.Unlock()
			_, _, err = st.WaitLog(ctx, epoch, cursor, coordPushLook)
		}
		if err != nil && ctx.Err() == nil {
			// A failed wait must not busy-spin or remove the existing retry backstop.
			a.sleep(coordPushLook)
		}
	}
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
	a.decisionPass(ctx)
	l, err := a.readCoordLook(ctx)
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
		if _, err := a.holdSprintPush(ctx, l.holder, false); err != nil {
			a.serveSay("COORDINATOR PUSH: unproving sprint push: %s", err)
		}
		return
	}
	sender := cmp.Or(login, l.holder)
	p := &a.coord
	p.mu.Lock()
	if p.holder != l.holder || p.epoch != l.epoch || p.told == nil {
		// Ordinary delivery keys reset for a new holder or epoch; state receipts remain.
		p.holder, p.epoch, p.told = l.holder, l.epoch, map[string]bool{}
		p.failed, p.heldAt, p.recoveryToken, p.recoveryBefore, p.recoveryFrom = false, time.Time{}, "", time.Time{}, ""
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
	due := p.heldAt.IsZero() || !now.Before(p.heldAt.Add(hook.RenewEvery))
	ok := !p.failed
	p.mu.Unlock()
	if due {
		if why, err := a.holdSprintPush(ctx, l.holder, ok); err != nil {
			a.serveSay("COORDINATOR PUSH: the sprint push of %s could not be held, tried again at the next look: %s", l.holder, oneline.Escape(err.Error()))
		} else {
			p.mu.Lock()
			p.heldAt = now
			say := why != p.heldWhy
			p.heldWhy = why
			p.mu.Unlock()
			if say {
				a.serveSay("COORDINATOR PUSH: %s", cmp.Or(why, "the sprint push of "+l.holder+" is held and proven"))
			}
		}
	}
	return

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
func (a *app) decisionPass(ctx context.Context) {
	type foldAttempt struct {
		to           string
		set          string
		token        string
		attempted    bool
		received     bool
		foldOwner    bool
		ownerReports []string
	}
	var coordinatorFold foldAttempt
	login, loginErr := a.coordinatorBusLogin()
	for _, lane := range []string{"coordinator", "owner"} {
		var job sprint.Note
		var epoch uint64
		err := a.onLine(ctx, func(st *store.Store) error {
			es, err := st.EpochNow(ctx)
			if err != nil {
				return err
			}
			epoch = es.N
			to, err := st.B.Coordinator(ctx)
			sender := cmp.Or(login, to, "nova-sprint")
			if loginErr != nil {
				sender = ""
			}
			if err != nil {
				return err
			}
			machine, _, err := st.Machine(ctx)
			if err != nil {
				return err
			}
			_, err = st.Run(ctx, store.Step{Verb: "decision escalation prepare", Load: []string{sprint.Work, sprint.Readers, sprint.Fleet}, Routes: true, Readers: true, Friends: true, Actor: sprint.MachineActor, Epoch: &epoch, Plan: func(s *sprint.Snapshot) sprint.Plan {
				var p sprint.Plan
				all := append(append([]sprint.Open{}, s.Open...), s.Acked...)
				set, body, reports := sprint.DecisionBatch(s.Now, all, sprint.DefaultHeldDecisions(s), lane, machine.StoppedBetween, s)
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
						job = *old
						return p
					}
					// Owner cadence is global to the lane, including changed/empty sets
					// and recipient changes. Keep the receipt until the window ends.
					ownerFold := lane == "owner" && coordinatorFold.attempted && coordinatorFold.received && coordinatorFold.foldOwner && coordinatorFold.to == to
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
				ownerFoldCandidate := coordinatorFold.foldOwner || !coordinatorFold.received && coordinatorFold.set == set
				if lane == "owner" && coordinatorFold.attempted && coordinatorFold.to == to && ownerFoldCandidate {
					if !coordinatorFold.received {
						return p
					}
					var coordinatorNote *sprint.Note
					for _, o := range s.Acked {
						d := o.Note.DecisionDelivery
						if o.Note.Type == sprint.NDecisionDelivery && d != nil && d.Lane == "coordinator" &&
							d.To == to && d.Set == set && d.Token == coordinatorFold.token && !d.SentAt.IsZero() {
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
					folded.Reports = coordinatorFold.ownerReports
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
				token := fmt.Sprintf("decisions:%x", sha256.Sum256([]byte(st.Names.Prefix+strconv.FormatUint(epoch, 10)+lane+to+set+s.Now.UTC().Format(time.RFC3339Nano))))
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
			}})
			if err != nil {
				return err
			}
			all, err := st.B.OpenNotes(ctx)
			if err != nil {
				return err
			}
			for _, o := range all {
				if d := o.Note.DecisionDelivery; o.Note.Type == sprint.NDecisionDelivery && d != nil && d.Lane == lane && d.SentAt.IsZero() {
					job = o.Note
					break
				}
			}
			return nil
		})
		if err != nil {
			a.serveSay("DECISIONS OWED: prepare %s failed: %s", lane, err)
			continue
		}
		if job.DecisionDelivery == nil {
			continue
		}
		d := *job.DecisionDelivery
		if lane == "coordinator" {
			coordinatorFold = foldAttempt{to: d.To, set: d.Set, token: d.Token, attempted: true}
		}
		fault := ""
		switch {
		case d.To == "":
			fault = "no " + lane + " channel is configured"
		case loginErr != nil:
			fault = "sender identity unresolved: " + loginErr.Error()
		case coordinatorSenderRefusal(d.From, login) != "":
			fault = coordinatorSenderRefusal(d.From, login)
		case !a.now().Before(d.RetryBefore):
			fault = "delivery receipt unknown: retry token expired; renewal due next look"
		default:
			if d.From == "" {
				d.From = login
			}
			if err := a.bus(ctx, bus.Message{From: d.From, To: []string{d.To}, Kind: bus.KindBlocker, Subject: "nova-sprint: decisions owed", Body: d.Body, Token: d.Token}, func(s string) { a.serveSay("%s", s) }); err != nil {
				fault = err.Error()
			}
		}
		receiptApplied := false
		err = a.onLine(ctx, func(st *store.Store) error {
			receiptTo, err := st.B.Coordinator(ctx)
			if err != nil {
				return err
			}
			machine, _, err := st.Machine(ctx)
			if err != nil {
				return err
			}
			_, err = st.Run(ctx, store.Step{Verb: "decision escalation receipt", Actor: sprint.MachineActor, Epoch: &epoch, Plan: func(s *sprint.Snapshot) sprint.Plan {
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
						ownerSet, _, reports := sprint.DecisionBatch(s.Now, all, sprint.DefaultHeldDecisions(s), "owner", machine.StoppedBetween, s)
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
						coordinatorFold.foldOwner = foldOwner
						coordinatorFold.ownerReports = append([]string(nil), reports...)
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
					receiptApplied = true
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
			}})
			return err
		})
		if err != nil {
			a.serveSay("DECISIONS OWED: receipt %s failed: %s", lane, err)
		} else if lane == "coordinator" && fault == "" && receiptApplied {
			coordinatorFold.received = true
		}
		if fault != "" {
			a.serveSay("DECISIONS OWED: %s delivery pending: %s", lane, fault)
		}
	}
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
