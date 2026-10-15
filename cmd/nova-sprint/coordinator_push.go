package main

import (
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus"
	"github.com/mas-bandwidth/nova-tools/hook"
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
// Every coordPushLook the server looks, on its line, at the inbox, the epoch and the
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
//     once, then at most once a sprint.JudgmentWarnEvery while the set stands
//     (sprint.JudgmentWarnings, sprint.JudgmentWarnDue).
//   - tells a late tick: the tick running TickLatePush late or more is one message, at
//     most once a sprint.PassEvery (lateWatch, pushlate.go).
//   - holds the sprint push: every hook.RenewEvery, when the holder's record is hooked as
//     the coordinator (the server joins a record, and never arms, hooks or unhooks one),
//     the sprint push's lease is renewed, and proven while the last send to the holder did
//     not fail (unproven while it did). Only the session's answer to nova-hook's challenge
//     proves the coordinator is there; the sprint push proves the way to the session.

// coordPushLook is how often the server looks for what is new for the coordinator.
const coordPushLook = 5 * time.Second

// coordPushMax is the most groups one message names in full; the rest are counted.
const coordPushMax = 20

// coordPush is the server's memory of what it told the coordinator: the holder and the
// epoch it is for, the delivery keys told, and the warning's and the late tick's state.
type coordPush struct {
	mu             sync.Mutex
	holder         string
	epoch          uint64
	told           map[string]bool
	firstSeen      map[string]time.Time
	warned         map[string]bool
	warnedAt       time.Time
	warnSet        string
	late           lateWatch
	recoveryBefore time.Time // last safe retry of the transport recovery send
	recoveryToken  string    // one transport recovery send per failed episode
	failed         bool      // the last send to the holder failed: the sprint push is unproven
	heldAt         time.Time // the sprint push's last renewal
	heldWhy        string    // what the last renewal said, said again only when it changes
}

// coordPushLoop is the server's look, every coordPushLook, for the life of the server.
func (a *app) coordPushLoop(ctx context.Context) {
	t := time.NewTicker(coordPushLook)
	defer t.Stop()
	for {
		a.coordinatorPass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// coordLook is what one look read on the line.
type coordLook struct {
	holder  string
	scope   string
	epoch   uint64
	groups  []sprint.Group
	open    []sprint.Open
	machine string
	stopped func(time.Time, time.Time) time.Duration
}

// readCoordLook reads the look on the line: the seat's holder, the epoch, the inbox and
// the machine's line.
func (a *app) readCoordLook(ctx context.Context) (coordLook, error) {
	var l coordLook
	err := a.onLine(ctx, func(st *store.Store) error {
		var err error
		if l.holder, err = st.B.Coordinator(ctx); err != nil || l.holder == "" {
			return err
		}
		es, err := st.EpochNow(ctx)
		if err != nil {
			return err
		}
		l.epoch = es.N
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
func (a *app) coordinatorPass(ctx context.Context) {
	l, err := a.readCoordLook(ctx)
	if err != nil {
		a.serveSay("COORDINATOR PUSH: the inbox could not be read, tried again at the next look: %s", oneline.Escape(err.Error()))
		return
	}
	if l.holder == "" {
		return
	}
	now := a.now()
	p := &a.coord
	p.mu.Lock()
	if p.holder != l.holder || p.epoch != l.epoch || p.told == nil {
		// Ordinary delivery keys reset for a new holder or epoch; state receipts remain.
		p.holder, p.epoch, p.told = l.holder, l.epoch, map[string]bool{}
		p.failed, p.heldAt, p.recoveryToken, p.recoveryBefore = false, time.Time{}, "", time.Time{}
		p.firstSeen, p.warned, p.warnedAt, p.warnSet = map[string]time.Time{}, nil, time.Time{}, ""
	}
	sends := coordFresh(p.told, l, now)
	var warn string
	var warnSet string
	var warnSeen map[string]bool
	if ws := sprint.JudgmentWarnings(coordWarningGroups(l), now, p.firstSeen); len(ws) > 0 {
		seen := make(map[string]bool, len(ws))
		for _, w := range ws {
			seen[w.ID] = true
		}
		if sprint.JudgmentWarnDue(ws, p.warned, p.warnedAt, now) {
			set := sprint.JudgmentWarningSet(ws)
			warn = set + "\n"
			if set != p.warnSet {
				warn = sprint.JudgmentWarningText(ws)
			}
			warnSet = set
		} else {
			p.warned = seen
		}
		warnSeen = seen
	} else {
		p.warnSet, p.warned, p.warnedAt = "", nil, time.Time{}
	}
	late := p.late.step(l.machine, now)
	p.mu.Unlock()

	failed := false
	attemptedHolder := false
	send := func(to string, m bus.Message) bool {
		m.From, m.To = prog, []string{to}
		if to == l.holder {
			attemptedHolder = true
		}
		if err := a.bus(ctx, m, func(s string) { a.serveSay("%s", s) }); err != nil {
			a.serveSay("COORDINATOR PUSH to=%s: %q could not be sent, sent again at the next look: %s", to, m.Subject, oneline.Escape(err.Error()))
			if to == l.holder {
				failed = true
			}
			return false
		}
		return true
	}
	candidates := map[string]sprint.CoordinatorDelivery{}
	for _, d := range sends {
		if d.note != "" {
			candidates[d.note] = sprint.CoordinatorDelivery{To: d.to, Token: d.token, Subject: d.subject, Body: d.text, Episode: d.episode, CoveredBy: d.coveredBy, RetryBefore: now.Add(bus.DefaultTokenLife)}
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
	for _, d := range sends {
		frozen, found := prepared[d.note]
		if d.note != "" {
			if !found || frozen.Sent {
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
		if !send(d.to, bus.Message{Kind: d.kind, Subject: subject, Body: d.text, Token: d.token}) {
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
			p.warnSet, p.warnedAt, p.warned = warnSet, now, warnSeen
			p.mu.Unlock()
		}
	}
	if late != "" {
		send(l.holder, bus.Message{Kind: bus.KindBlocker, Subject: "nova-sprint: the machine's tick runs late", Body: late})
	}
	p.mu.Lock()
	recover := p.failed && len(sends) == 0 && warn == "" && late == ""
	recoveryToken, recoveryBefore := p.recoveryToken, p.recoveryBefore
	p.mu.Unlock()
	if recover && (recoveryBefore.IsZero() || !now.Before(recoveryBefore)) {
		failed, attemptedHolder = true, true
		a.serveSay("COORDINATOR PUSH: recovery receipt unknown; retry window ended, inspect the bus before restoring delivery")
	} else if recover {
		send(l.holder, bus.Message{Kind: bus.KindStatus, Token: recoveryToken, Subject: "nova-sprint: coordinator delivery restored", Body: "The previous delivery attempt failed; its work is resolved. This one recovery message confirms that coordinator bus delivery works again."})
	}
	p.mu.Lock()
	if attemptedHolder {
		p.failed = failed
		if failed && p.recoveryToken == "" {
			p.recoveryBefore = now.Add(bus.DefaultTokenLife)
			p.recoveryToken = fmt.Sprintf("recovery:%x", sha256.Sum256([]byte(l.scope+l.holder+strconv.FormatUint(l.epoch, 10)+now.UTC().Format(time.RFC3339Nano))))
		}
		if !failed {
			p.recoveryToken, p.recoveryBefore = "", time.Time{}
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
}

// coordSend is one message the look sends: to whom, its kind, its text, and the groups
// and keys it tells.
type coordSend struct {
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
	return append(out, state...)
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
