package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
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

// seatSilentTold is each raise of the judgment told to the owner, by its token, and the last
// look.
type seatSilentTold struct {
	mu     sync.Mutex
	told   map[string]bool
	warned map[string]bool
	lookAt time.Time
}

// defaultHostName is the server host's short name: its fleet machine row's name.
// tla/CoordinatorPass.tla PushesBoundedWhileUnchanged (the escalation's target)
func defaultHostName() string {
	h, _ := os.Hostname()
	return strings.SplitN(h, ".", 2)[0]
}

// hostName is this app's host name (its fleet machine row's name): the app's
// own, never a package global a parallel test can overwrite.
func (a *app) hostName() string {
	if a.hostNameFn != nil {
		return a.hostNameFn()
	}
	return defaultHostName()
}

// knownTarget says name is a nova-config friend row or a fleet (machine/friend) row:
// the only names the bus accepts.
func knownTarget(name string, friends []string, fleetRows []string) bool {
	if name == "" {
		return false
	}
	if slices.Contains(friends, name) {
		return true
	}
	for _, row := range fleetRows {
		if row == name || row == sprint.FriendRow(name) {
			return true
		}
		if f, ok := sprint.FriendOfRow(row); ok && f == name {
			return true
		}
	}
	return false
}

// ownerEscalationWarn is the WARN line when the owner or the sender is no known
// row, "" when both are verified. An unread snapshot verifies nothing: the
// escalation is treated as having no target (fail closed, never open).
// tla/CoordinatorPass.tla EscalatedOnce
func ownerEscalationWarn(owner string, snap *sprint.Snapshot, host string) string {
	if owner == "" {
		return ""
	}
	if snap == nil {
		return fmt.Sprintf("WARN owner escalation: the friend and machine rows could not be read: %s will not be told until they can", owner)
	}
	_, warn := escalationTarget(owner, snap, host)
	return warn
}

// escalationTarget is the one checked sender for an owner escalation: the
// server host's fleet machine row, verified against the snapshot's rows.
// Either endpoint unverified or unknown gives no sender and the WARN line:
// nothing is sent, and delivery resumes on its own once the row appears.
func escalationTarget(owner string, snap *sprint.Snapshot, host string) (string, string) {
	if snap == nil {
		return "", fmt.Sprintf("WARN owner escalation: the friend and machine rows could not be read: %s will not be told until they can", owner)
	}
	var friends []string
	for _, f := range snap.Friends {
		friends = append(friends, f.Name)
	}
	var rows []string
	if snap.Fleet != nil {
		rows = snap.Fleet.Rows()
	}
	if !knownTarget(owner, friends, rows) {
		return "", fmt.Sprintf("WARN owner escalation: %s is not a nova-config friend or machine row: the owner will not be told; set init --owner <row>", owner)
	}
	if host == "" || !knownTarget(host, friends, rows) {
		name := host
		if name == "" {
			name = "(no host name)"
		}
		return "", fmt.Sprintf("WARN owner escalation: %s is not a nova-config friend or machine row: the owner will not be told; set init --owner <row>", name)
	}
	return host, ""
}

// escalationSender is the verified bus sender: the server host's fleet
// machine row, "" when the host names no fleet row (or no snapshot could be
// read): an unverified sender is never guessed.
func escalationSender(snap *sprint.Snapshot, host string) string {
	if snap == nil || host == "" {
		return ""
	}
	var rows []string
	if snap.Fleet != nil {
		rows = snap.Fleet.Rows()
	}
	if knownTarget(host, nil, rows) {
		return host
	}
	return ""
}

// ownerKnown says the owner name itself is a friend or fleet row the bus
// accepts; tellSeatSilent sends nothing to an owner that fails this check
// (the sender is warned of at start by ownerEscalationWarn, and its row is
// the host's in production).
func ownerKnown(owner string, snap *sprint.Snapshot) bool {
	if snap == nil {
		return false // unread rows verify nothing: fail closed, send nothing
	}
	if owner == "" {
		return false
	}
	var friends []string
	for _, f := range snap.Friends {
		friends = append(friends, f.Name)
	}
	var rows []string
	if snap.Fleet != nil {
		rows = snap.Fleet.Rows()
	}
	return knownTarget(owner, friends, rows)
}

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

// tellSeatSilent sends each raise of NSeatSilent and NNeedBlocked not yet told to the sprint's owner on the
// bus, and says it on the server's log; a sprint with no owner is said on the log alone.
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
	groups, err := st.OpenGroups(ctx)
	if err != nil {
		a.serveSay("SEAT SILENT: the open judgments could not be read: %s", oneline.Escape(err.Error()))
		return
	}
	es, err := st.EpochNow(ctx)
	if err != nil {
		a.serveSay("SEAT SILENT: the epoch could not be read: %s", oneline.Escape(err.Error()))
		return
	}
	// the escalation's snapshot, read once a look: its friends and fleet rows are
	// the names the bus accepts (ownerEscalationWarn). An unread snapshot
	// verifies nothing: send nothing this look and try again at the next.
	read, err := st.DoctorRead(ctx)
	if err != nil {
		a.serveSay("SEAT SILENT: the escalation's rows could not be read, tried again at the next look: %s", oneline.Escape(err.Error()))
		return
	}
	snap := read.Snap
	owner, ownerErr := a.owner(ctx, st)
	sender, warn := "", ""
	if ownerErr == nil && owner != "" {
		sender, warn = escalationTarget(owner, snap, a.hostName())
	}
	open := map[string]bool{}
	for _, g := range groups {
		if g.Type != sprint.NSeatSilent && g.Type != sprint.NSeatNotMoving && g.Type != sprint.NNeedBlocked {
			continue
		}
		token := seatSilentToken(st.Names.Prefix, es.N, g.ID, g.Before)
		open[token] = true
		a.seatSilent.mu.Lock()
		told := a.seatSilent.told[token]
		a.seatSilent.mu.Unlock()
		if told {
			continue
		}
		if ownerErr != nil {
			a.serveSay("SEAT SILENT %s: the owner could not be read, tried again at the next look: %s", g.ID, oneline.Escape(ownerErr.Error()))
			continue
		}
		if owner == "" {
			a.serveSay("SEAT SILENT %s raise=%d: %s", g.ID, g.Before, oneline.Quote(g.What))
			a.serveSay("SEAT SILENT %s: the sprint names no owner: told on this line only", g.ID)
		} else if warn != "" {
			// no target that exists: send nothing, say so once a raise; the
			// tick keeps the one judgment (ownerNoTargetConds)
			a.seatSilent.mu.Lock()
			if a.seatSilent.warned == nil {
				a.seatSilent.warned = map[string]bool{}
			}
			already := a.seatSilent.warned[token]
			a.seatSilent.warned[token] = true
			a.seatSilent.mu.Unlock()
			if !already {
				a.serveSay("SEAT SILENT %s: %s", g.ID, warn)
			}
			continue
		} else {
			a.serveSay("SEAT SILENT %s raise=%d: %s", g.ID, g.Before, oneline.Quote(g.What))
			m := bus.Message{From: sender, To: []string{owner}, Kind: bus.KindBlocker, Subject: g.Type, Body: g.What, Token: token}
			if err := a.bus(ctx, m, func(l string) { a.serveSay("%s", l) }); err != nil {
				a.serveSay("SEAT SILENT %s: the push to %s failed, tried again at the next look under the same token: %s", g.ID, owner, oneline.Escape(err.Error()))
				continue
			}
		}
		a.seatSilent.mu.Lock()
		if a.seatSilent.told == nil {
			a.seatSilent.told = map[string]bool{}
		}
		a.seatSilent.told[token] = true
		a.seatSilent.mu.Unlock()
	}
	// forget every raise no longer open: its judgment closed, or raised again since
	a.seatSilent.mu.Lock()
	for token := range a.seatSilent.told {
		if !open[token] {
			delete(a.seatSilent.told, token)
		}
	}
	for token := range a.seatSilent.warned {
		if !open[token] {
			delete(a.seatSilent.warned, token)
		}
	}
	a.seatSilent.mu.Unlock()
}
