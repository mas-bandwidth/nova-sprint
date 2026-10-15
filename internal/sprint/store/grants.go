package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

const keyGrants = "grants"

func (st *Store) Grants(ctx context.Context) (sprint.Grants, error) {
	var g sprint.Grants
	err := st.getJSON(ctx, keyGrants, &g)
	return g, err
}

// changeGrants journals the complete record, receipt, log and judgments before
// any mutation. Existing finishOp repairs this non-lock operation after a crash;
// Release commits it atomically. No bus write joins this store transaction.
// tla/BenchGrant.tla Add, Extend, End, Wake, Ack, GiveUp and Prune.
func (st *Store) changeGrants(ctx context.Context, step Step, fn func(*sprint.Grants, *sprint.Snapshot) (*sprint.Grant, []sprint.Note, bool, error)) (sprint.Grant, Result, error) {
	var out sprint.Grant
	res := Result{Verb: step.Verb, Args: step.Args}
	if step.CallerOp != "" && !callerOpWord(step.CallerOp) {
		return out, res, fmt.Errorf("operation id %q is not a valid word", step.CallerOp)
	}
	pinned, err := st.pin(ctx)
	if err != nil {
		return out, res, err
	}
	replayed := func() (bool, error) {
		if step.CallerOp == "" {
			return false, nil
		}
		var done bool
		res, done, err = pinned.callerOp(ctx, step, res)
		if done && err == nil && len(res.Said) > 0 {
			err = json.Unmarshal([]byte(res.Said[0]), &out)
		}
		return done, err
	}
	if done, err := replayed(); done || err != nil {
		return out, res, err
	}
	tries := pinned.retry(ctx)
	for tries.next(pinned.attempts()) {
		snap, gen, err := pinned.Fenced(ctx, step.Load, nil, nil)
		if err != nil {
			return out, res, err
		}
		// Fenced may just have recovered this caller's commit.
		if done, err := replayed(); done || err != nil {
			return out, res, err
		}
		gs, err := pinned.Grants(ctx)
		if err != nil {
			return out, res, err
		}
		changed, notes, write, err := fn(&gs, snap)
		if err != nil {
			return out, res, err
		}
		if changed != nil {
			out = *changed
		}
		if !write {
			return out, res, nil
		}
		id := sprint.OpFamily("grant-"+pinned.newID(), pinned.epoch)
		res.Op, res.Moved, res.Notes = id, []string{step.Verb}, len(notes)
		if changed != nil {
			raw, _ := json.Marshal(out)
			res.Said = []string{string(raw)}
		}
		raw, err := json.Marshal(res)
		if err != nil {
			return out, res, err
		}
		op := OpRecord{ID: id, Verb: step.Verb, At: snap.Now, Grants: &gs, CallerOp: step.CallerOp, Result: string(raw), Notes: notes,
			Log: []sprint.Line{{Kind: "note", Epoch: pinned.epoch, At: snap.Now, Actor: pinned.Actor, Verb: step.Verb, Op: id}}}
		for i := range op.Notes {
			op.Notes[i].ID = fmt.Sprintf("%s.n%d", id, i+1)
		}
		if recordSize(op) > MaxOpRecord {
			return out, res, fmt.Errorf("grant record exceeds the store operation bound of %d bytes", MaxOpRecord)
		}
		ok, err := pinned.B.Acquire(ctx, gen, op)
		if err != nil {
			return out, res, fmt.Errorf("%w: grant acquisition: %v", ErrUnknown, err)
		}
		if !ok {
			res = Result{Verb: step.Verb, Args: step.Args}
			continue
		}
		if err := pinned.B.Release(ctx, op, true); err != nil {
			return out, res, fmt.Errorf("%w: grant commit: %v", ErrUnknown, err)
		}
		return out, res, nil
	}
	return out, res, fmt.Errorf("grant: the sprint kept changing; nothing was changed")
}

func (st *Store) ChangeGrant(ctx context.Context, r sprint.GrantRequest, caller string) (sprint.Grant, Result, error) {
	step := Step{Verb: "grant " + r.Op, CallerOp: caller, Args: ArgsOf(r), Load: []string{sprint.Fleet}}
	return st.changeGrants(ctx, step, func(gs *sprint.Grants, s *sprint.Snapshot) (*sprint.Grant, []sprint.Note, bool, error) {
		coord, err := st.B.Coordinator(ctx)
		if err != nil {
			return nil, nil, false, err
		}
		if coord == "" || st.Actor != coord {
			return nil, nil, false, fmt.Errorf("grant is the coordinator's alone: %s", coord)
		}
		member := s.Fleet.HasRow(r.Grantee) || s.Fleet.HasRow(sprint.FriendRow(r.Grantee))
		if err := r.Validate(member); err != nil {
			return nil, nil, false, err
		}
		if r.Op == "add" {
			gs.Next++
			g := sprint.Grant{ID: fmt.Sprintf("g%d", gs.Next), Bench: r.Bench, Grantee: r.Grantee, By: st.Actor, Granted: s.Now, Deadline: s.Now.Add(r.For), Changed: s.Now}
			gs.Items = append(gs.Items, g)
			return &g, nil, true, nil
		}
		for i := range gs.Items {
			g := &gs.Items[i]
			if g.ID != r.ID {
				continue
			}
			if !g.Ended.IsZero() {
				return nil, nil, false, fmt.Errorf("grant %s has ended", r.ID)
			}
			g.KeepOwed(s.Now)
			if r.Op == "extend" {
				g.Deadline = s.Now.Add(r.For)
			} else {
				g.Ended = s.Now
			}
			g.Changed = s.Now
			return g, nil, true, nil
		}
		return nil, nil, false, fmt.Errorf("no grant is %s; run: nova-sprint grant list", r.ID)
	})
}

// PrepareGrantDeliveries freezes identities and payload before sending. The
// server calls it on its line on every wake, including STOPPED.
func (st *Store) PrepareGrantDeliveries(ctx context.Context, sender string, senderReady bool, life, cleanup time.Duration) ([]sprint.Grant, error) {
	// Like Tick, this server duty outlives clears; caller mutations retain their
	// original pinned epoch, but global grant housekeeping follows the current one.
	current, err := st.repin(ctx)
	if err != nil {
		return nil, err
	}
	var pending []sprint.Grant
	_, _, err = current.changeGrants(ctx, Step{Verb: "grant expiry"}, func(gs *sprint.Grants, s *sprint.Snapshot) (*sprint.Grant, []sprint.Note, bool, error) {
		pending = nil
		holder, err := st.B.Coordinator(ctx)
		if err != nil {
			return nil, nil, false, err
		}
		from := sender
		if from == "" {
			from = holder
		}
		var notes []sprint.Note
		var keep []sprint.Grant
		write := false
		for _, g := range gs.Items {
			if g.Prunable(s.Now, cleanup) {
				write = true
				continue
			}
			if senderReady && g.Due(s.Now) && g.Delivery == nil && from != "" && holder != "" {
				g.KeepOwed(s.Now)
				to, seat := grantBusRecipient(g.Grantee), grantBusRecipient(holder)
				if to != seat {
					to += "," + seat
				}
				g.Delivery = &sprint.CoordinatorDelivery{From: from, To: to, Token: fmt.Sprintf("bench-grant.%s.%d", g.ID, g.Expiry().Unix()), RetryBefore: s.Now.Add(life), Subject: fmt.Sprintf("BENCH GRANT EXPIRED %s grantee=%s deadline=%s", g.Bench, g.Grantee, g.Expiry().UTC().Format(time.RFC3339))}
				g.Changed, write = s.Now, true
			}
			if d := g.Delivery; d != nil && !d.Sent && !g.Judged {
				if !s.Now.Before(d.RetryBefore) {
					g.Judged, g.Changed, write = true, s.Now, true
					notes = append(notes, sprint.Note{Kind: sprint.Judgment, Type: sprint.NTimer, At: s.Now, Who: sprint.MachineActor, To: holder, Primaries: []string{"bench-grant:" + g.ID}, Count: 1, What: "bench grant " + g.ID + " receipt unknown: " + d.Subject, Decisions: []string{"ack"}})
				} else {
					pending = append(pending, g)
				}
			}
			keep = append(keep, g)
		}
		gs.Items = keep
		return nil, notes, write, nil
	})
	return pending, err
}

func (st *Store) MarkGrantSent(ctx context.Context, id, token string) error {
	// Clear may occur while the bus send is outside the sprint line.
	current, err := st.repin(ctx)
	if err != nil {
		return err
	}
	_, _, err = current.changeGrants(ctx, Step{Verb: "grant sent"}, func(gs *sprint.Grants, s *sprint.Snapshot) (*sprint.Grant, []sprint.Note, bool, error) {
		for i := range gs.Items {
			g := &gs.Items[i]
			if g.ID == id && g.Delivery != nil && g.Delivery.Token == token && !g.Delivery.Sent && !g.Judged {
				g.Delivery.Sent, g.Changed = true, s.Now
				return nil, nil, true, nil
			}
		}
		return nil, nil, false, nil
	})
	return err
}

// A sprint friend row names its owner on the bus; preserve the typed Grantee
// in the grant and subject, and freeze canonical, deduplicated recipients.
func grantBusRecipient(name string) string {
	if friend, ok := sprint.FriendOfRow(name); ok {
		return friend
	}
	return name
}
