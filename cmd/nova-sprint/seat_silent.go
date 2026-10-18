package main

import (
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
)

// The seat is not answering (internal/sprint/seat_answers.go; docs/SPEC-SPRINT.md section 8,
// "A judgment that holds a card"): the judgment reaches the seat's hook as every judgment
// does, and the owner on the bus too, so a seat whose hook has lapsed, or that is busy
// elsewhere, never leaves a card held unseen. The run loop looks after each tick, at most
// every seatSilentLook, and sends each raise of the judgment (its first, and each raise
// again: Group.Before) to the owner's name. A raise is marked told only once its send
// succeeded: a failed send is tried again at the next look, under the same bus token
// (seatSilentToken), so a send whose response was lost, or a server restarted between a
// send and its mark, never puts a second message on the bus (pkg/bus/token.go: the bus
// answers a token it has seen with the message it sent). Raises of judgments no longer open
// are forgotten at each look.

// seatSilentLook is the least time between two looks for the judgment.
const seatSilentLook = 5 * time.Second

// seatSilentTold keeps each raise's delivery and advisory marks, and each failed send’s retry state.
type seatSilentTold struct {
	mu      sync.Mutex
	told    map[string]bool
	lookAt  time.Time
	retry   map[string]busRetry
	advised map[string]bool
}

// busRetryFirst and busRetryCap bound the wait before a send the bus refused is sent again:
// the first refusal waits busRetryFirst, each refusal after doubles it, up to busRetryCap. A
// refusal (a recipient the bus does not know, 2026-10-17: "glenn is no known name") is
// refused again until someone changes the roster; before, it was sent again at every look and
// every coordinator pass, two thirds of the server's log.
const (
	busRetryFirst = time.Minute
	busRetryCap   = 30 * time.Minute
)

// busRetry is one send's failures: when it is sent again (zero: at the next look), the wait
// that led there, and the failure last said, said once until its text changes.
type busRetry struct {
	next time.Time
	wait time.Duration
	said string
}

// busRefused says the send is refused for what it is, so that the same send is refused again:
// a bus.Refusal (a recipient the bus does not know, ...), or a fault of the sender's own that
// no retry changes (permanentFault: no channel configured, a sender identity unresolved).
// Not a store or a network down, and not a send that went under its token before
// (busMessageWent), which is a delivery. The wait is not cut short when the cause is fixed
// (a name added to the roster): the next send goes when the wait ends, at most busRetryCap
// later, or at once on a server restart.
func busRefused(err error) bool {
	var r *bus.Refusal
	var p permanentFault
	if err != nil && strings.Contains(err.Error(), busRedisEnv+" is not set") {
		return true // the server's own environment names no bus (sendBus): it stands until set
	}
	return errors.As(err, &p) || errors.As(err, &r) && !busMessageWent(err) && !busNeedsNewToken(err)
}

// permanentFault is a failed send whose cause is the sender's own and stands until someone
// changes the configuration: it waits as a refusal does.
type permanentFault struct{ error }

// busNeedsNewToken says the bus refused a send because its token went before with other
// arguments, or names a record that is none (pkg/bus replay): the send is another message,
// and wants a token of its own.
func busNeedsNewToken(err error) bool {
	var r *bus.Refusal
	if err == nil || !errors.As(err, &r) {
		return false
	}
	for _, p := range r.Problems {
		if strings.Contains(p, "with other arguments") || strings.Contains(p, "has a record that is not one") {
			return true
		}
	}
	return false
}

// busRetoken is the token of a send whose own token went with other arguments: the token and
// a digest of what the message says, so the same message is the same send again.
func busRetoken(token string, parts ...string) string {
	t := token + ":" + fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, "\x00"))))[:16]
	if len(t) > bus.MaxToken {
		t = t[len(t)-bus.MaxToken:]
	}
	return t
}

// failed records a failed send at now: a refusal waits, the wait doubling from busRetryFirst to
// busRetryCap; another failure is sent again at the next look. It says whether the failure is
// one not said before (its text changed), to be said.
func (r *busRetry) failed(now time.Time, err error) (say bool) {
	if busRefused(err) {
		r.wait = min(max(r.wait*2, busRetryFirst), busRetryCap)
		r.next = now.Add(r.wait)
	} else {
		r.wait, r.next = 0, time.Time{}
	}
	why := err.Error()
	say = why != r.said
	r.said = why
	return say
}

// due says the send may be tried at now.
func (r busRetry) due(now time.Time) bool { return !now.Before(r.next) }

// seatSilentToken is the bus token of one raise of the judgment: the sprint (its key
// prefix), its epoch, the judgment's id and the raise, so the same raise is the same send
// across retries and restarts, and a raise of another sprint or epoch is another.
// tla/SprintRules.tla PutMsg
func seatSilentToken(prefix string, epoch uint64, id string, raise int) string {
	clean := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r) {
				return r
			}
			return '_'
		}, s)
	}
	t := "seat-silent:" + clean(prefix) + ":" + strconv.FormatUint(epoch, 10) + ":" + clean(id) + ":" + strconv.Itoa(raise)
	if len(t) > bus.MaxToken {
		t = t[len(t)-bus.MaxToken:]
	}
	return t
}

// tellSeatSilent sends each raise of NSeatSilent and NNeedBlocked not yet told to whoever holds the
// coordinator seat at push time, on the bus, and says it on the server's log; a sprint whose
// coordinator's login is unresolved or whose seat no one holds is said on the log alone, and the
// raise is tried again at the next look (docs/SPEC-SPRINT.md section 8,
// owner-pushes-route-to-coordinator).
// tla/SprintRules.tla SendOK
// tla/NeedsBlocked.tla BlockedDependentsCovered
func (a *app) tellSeatSilent(ctx context.Context, st *store.Store) {
	now := a.now()
	a.seatSilent.mu.Lock()
	if !a.seatSilent.lookAt.IsZero() && now.Sub(a.seatSilent.lookAt) < seatSilentLook && now.After(a.seatSilent.lookAt) {
		a.seatSilent.mu.Unlock()
		return
	}
	a.seatSilent.lookAt = now
	a.seatSilent.mu.Unlock()
	// The open notes are read once, at the sprint's epoch as it is now, for the groups and
	// the owner's receipts both; a look with no judgment of the kinds it tells reads nothing
	// more (before, 2026-10-17, every look read the open notes twice, the work table, the
	// machine and the epoch). The epoch is read first: the run loop's store may be pinned to
	// an epoch a clear has left, or not pinned at all, and its backend then names another
	// epoch's keys (store.OpenGroups pins for the same reason).
	es, err := st.EpochNow(ctx)
	if err != nil {
		a.serveSay("SEAT SILENT: the epoch could not be read: %s", oneline.Escape(err.Error()))
		return
	}
	backend := st.B
	if es.N != 0 {
		backend = backend.AtEpoch(es.N, false)
	}
	all, readErr := backend.OpenNotes(ctx)
	if readErr != nil {
		a.serveSay("SEAT SILENT: the open judgments could not be read: %s", oneline.Escape(readErr.Error()))
		return
	}
	judgments, _ := sprint.SplitOpen(all)
	tells, hasSeatSilent := false, false
	for _, o := range judgments {
		tells = tells || o.Note.Type == sprint.NSeatSilent || o.Note.Type == sprint.NNeedBlocked
		hasSeatSilent = hasSeatSilent || o.Note.Type == sprint.NSeatSilent
	}
	if !tells {
		a.seatSilent.mu.Lock()
		a.seatSilent.told, a.seatSilent.retry, a.seatSilent.advised = nil, nil, nil // every raise is closed: forgotten
		a.seatSilent.mu.Unlock()
		return
	}
	standby := ""
	var standbyErr error
	if hasSeatSilent {
		shapes, settingsErr := backend.Shapes(ctx, []string{st.Names.Table(sprint.Work)})
		if settingsErr != nil || len(shapes) != 1 {
			if settingsErr == nil {
				settingsErr = fmt.Errorf("work table shape count: %d", len(shapes))
			}
			standbyErr = settingsErr
			a.serveSay("SEAT SILENT: the standby seat setting could not be read: %s", oneline.Escape(settingsErr.Error()))
		} else {
			standby = shapes[0].Props[sprint.PropSeatSilentTo]
			if standby == sprint.ReadTierDefault {
				standby = ""
			}
		}
	}
	// the groups as store.OpenGroups makes them, from the notes read above (the printed
	// commands, which read the work table, are not used here)
	groups := sprint.Inbox(sprint.InboxReq{Now: now, Open: judgments, Prefix: st.Names.Prefix, Epoch: es.N})
	// Suppress the legacy alarm only after a successful folded owner delivery
	// covered every overdue card-holding judgment represented by this group.
	machine, _, machineErr := st.Machine(ctx)
	if machineErr != nil {
		a.serveSay("DECISIONS OWED: clock read failed; preserving legacy owner alarm: %s", machineErr)
	}
	received := map[string]bool{}
	for _, o := range all {
		if d := o.Note.DecisionDelivery; o.Note.Type == sprint.NDecisionDelivery && d != nil && d.Lane == "owner" && !d.SentAt.IsZero() {
			for _, id := range strings.Split(d.Set, "\x00") {
				received[id] = true
			}
		}
	}
	open := map[string]bool{}
	for _, g := range groups {
		if g.Type != sprint.NSeatSilent && g.Type != sprint.NNeedBlocked {
			continue
		}
		token := seatSilentToken(st.Names.Prefix, es.N, g.ID, g.Before)
		open[token] = true
		if g.Type == sprint.NSeatSilent && readErr == nil && machineErr == nil {
			relevant, covered := false, true
			groupCards := map[string]bool{}
			for _, id := range g.Primaries {
				groupCards[id] = true
			}
			for _, o := range all {
				n := o.Note
				if n.Kind != sprint.Judgment || !sprint.HoldsACard(n.Type) {
					continue
				}
				age, limit := sprint.DecisionAge(now, n, machine.StoppedBetween)
				if limit <= 0 || age < limit {
					continue
				}
				cards := n.Primaries
				if len(cards) == 0 {
					if n.Card != "" {
						cards = []string{n.Card}
					} else {
						cards = n.Subjects()
					}
				}
				for _, card := range cards {
					if groupCards[card] {
						relevant = true
						covered = covered && received[n.ID]
						break
					}
				}
			}
			if relevant && covered {
				continue
			}
		}
		if g.Type == sprint.NSeatSilent && standbyErr != nil {
			continue // keep this raise for a later look; other groups still go to the holder
		}
		a.seatSilent.mu.Lock()
		told := a.seatSilent.told[token]
		retry, retrying := a.seatSilent.retry[token]
		a.seatSilent.mu.Unlock()
		if told || !retry.due(now) {
			continue
		}
		if !retrying {
			a.serveSay("SEAT SILENT %s raise=%d: %s", g.ID, g.Before, oneline.Quote(g.What))
		}
		login, loginErr := a.coordinatorBusLogin()
		holder, err := backend.Coordinator(ctx)
		if err != nil {
			a.serveSay("SEAT SILENT %s: the coordinator could not be read, tried again at the next look: %s", g.ID, oneline.Escape(err.Error()))
			continue
		}
		if holder == "" {
			a.serveSay("SEAT SILENT %s: no coordinator seat is held, tried again at the next look", g.ID)
			continue
		}
		if g.Type == sprint.NSeatSilent && (standby == "" || standby == holder) {
			a.seatSilent.mu.Lock()
			if a.seatSilent.advised == nil {
				a.seatSilent.advised = map[string]bool{}
			}
			advise := !a.seatSilent.advised[token]
			a.seatSilent.advised[token] = true
			a.seatSilent.mu.Unlock()
			if advise {
				a.serveSay("SEAT SILENT %s: no standby seat hears this alarm, only the silent seat; run: nova-sprint set --seat-silent-to <bus name>", g.ID)
			}
		}
		if loginErr != nil {
			a.serveSay("SEAT SILENT %s: the coordinator's bus login is unresolved, tried again at the next look: %s", g.ID, oneline.Escape(loginErr.Error()))
			continue
		}
		to := []string{holder}
		if g.Type == sprint.NSeatSilent && standby != "" && standby != holder {
			to = append(to, standby)
		}
		m := bus.Message{From: cmp.Or(login, holder), To: to, Kind: bus.KindBlocker, Subject: g.Type, Body: g.What, Token: token}
		err = a.bus(ctx, m, func(l string) { a.serveSay("%s", l) })
		if busNeedsNewToken(err) {
			// the raise's token went with other arguments (its text or its recipient changed
			// under it): this is another message, sent under a token of its own
			m.Token = busRetoken(token, strings.Join(to, "\x00"), g.Type, g.What)
			err = a.bus(ctx, m, func(l string) { a.serveSay("%s", l) })
		}
		if err != nil && !busMessageWent(err) {
			say := retry.failed(now, err)
			a.seatSilent.mu.Lock()
			if a.seatSilent.retry == nil {
				a.seatSilent.retry = map[string]busRetry{}
			}
			a.seatSilent.retry[token] = retry
			a.seatSilent.mu.Unlock()
			if say {
				when := "at the next look"
				if !retry.next.IsZero() {
					when = "in " + retry.wait.String() + ", the wait doubling while the bus refuses it,"
				}
				a.serveSay("SEAT SILENT %s: the push to %s failed, tried again %s under the same token (said once until it changes): %s", g.ID, holder, when, oneline.Escape(err.Error()))
			}
			continue
		}
		a.seatSilent.mu.Lock()
		if a.seatSilent.told == nil {
			a.seatSilent.told = map[string]bool{}
		}
		a.seatSilent.told[token] = true
		delete(a.seatSilent.retry, token)
		a.seatSilent.mu.Unlock()
	}
	// forget every raise no longer open: its judgment closed, or raised again since
	a.seatSilent.mu.Lock()
	for token := range a.seatSilent.told {
		if !open[token] {
			delete(a.seatSilent.told, token)
		}
	}
	for token := range a.seatSilent.retry {
		if !open[token] {
			delete(a.seatSilent.retry, token)
		}
	}
	for token := range a.seatSilent.advised {
		if !open[token] {
			delete(a.seatSilent.advised, token)
		}
	}
	a.seatSilent.mu.Unlock()
}
