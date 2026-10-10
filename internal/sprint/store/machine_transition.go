package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// machineTransition takes the same operation fence as work and read steps.
// Acquire advances its generation, so a worker that read RUNNING before STOP
// cannot commit afterward using its old read. A repeated STOP and a START also
// re-read the machine under this lock, never overwriting a newer stop reason.
func (st *Store) machineTransition(ctx context.Context, running bool, who, reason string, until time.Time) (before, after Machine, changed bool, err error) {
	if running {
		// Settle owed leases whose owner's beat after the STOP no longer names
		// the job, before the START fence re-reads the debt (tla/StopReturn.tla
		// SettleByBeat), so START never waits on a member's own receipt.
		if err := st.settleStopDebtByBeat(ctx); err != nil {
			return before, after, false, err
		}
	}
	pinned, err := st.pin(ctx)
	if err != nil {
		return before, after, false, err
	}
	r := pinned.retry(ctx)
	for r.next(pinned.attempts()) {
		// STOP records the same live owner leases that START later checks.
		s, gen, ferr := pinned.Fenced(ctx, []string{sprint.Fleet, sprint.Readers}, nil, nil)
		if ferr != nil {
			return before, after, false, ferr
		}
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return before, after, false, err
		}
		lock := OpRecord{ID: "machine-" + hex.EncodeToString(nonce[:]) + "-lock", Verb: "machine transition lock", At: pinned.now(), Lock: true}
		ok, aerr := pinned.B.Acquire(ctx, gen, lock)
		if aerr != nil {
			return before, after, false, aerr
		}
		if !ok {
			continue
		}
		before, _, err = pinned.Machine(ctx)
		if err == nil {
			after, changed, err = pinned.machineRecord(ctx, before, s, running, who, reason, until)
		}
		err = errors.Join(err, pinned.B.Release(context.WithoutCancel(ctx), lock, false))
		if err != nil {
			return before, before, false, err
		}
		return before, after, changed, nil
	}
	return before, after, false, fmt.Errorf("machine transition: the sprint kept changing under it (%d tries); nothing was changed; run it again", r.tries)
}

func (st *Store) machineRecord(ctx context.Context, before Machine, s *sprint.Snapshot, running bool, who, reason string, until time.Time) (after Machine, changed bool, err error) {
	if before.Running() == running && (before.Cause != "" || !running && (reason != "" || before.Reason != "")) {
		after = before
		after.StopIssued = !running
		if !running && len(before.StopDebt) == 0 {
			after.StopDebt = activeStopLeases(s)
		}
		after.Cause, after.Reason, after.Until = "", reason, until
		if reason != "" {
			after.Who = who
		}
		return after, false, st.putMachine(ctx, after)
	}
	if before.Running() == running {
		if !running {
			after = before
			after.StopIssued = true
			if len(before.StopDebt) == 0 {
				after.StopDebt = activeStopLeases(s)
			}
			return after, !before.StopIssued || len(after.StopDebt) > 0 && len(before.StopDebt) == 0, st.putMachine(ctx, after)
		}
		if kv, ok := st.B.(KV); ok {
			err = kv.ShowState(ctx, st.Names.View(), ViewState(before))
		}
		return before, false, err
	}
	if running {
		if err := unsettledStopDebt(before, s); err != nil {
			return before, false, err
		}
	}
	now := st.now()
	after = before
	after.Spans = append([]Span(nil), before.Spans...)
	if running {
		if after.RunSeq == ^uint64(0) {
			return before, false, errors.New("machine start generation is exhausted; nothing was changed")
		}
		after.RunSeq++
		if n := len(after.Spans); n > 0 && after.Spans[n-1].To.IsZero() {
			after.Spans[n-1].To = now
			after.StoppedFor += now.Sub(after.Spans[n-1].From)
		}
		after.State = Running
		after.StopIssued = false
		after.StopDebt = nil
	} else {
		after.StopIssued = true
		after.StopDebt = activeStopLeases(s)
		after.Spans = append(after.Spans, Span{From: now})
		if len(after.Spans) > MaxStopSpans {
			after.Spans = after.Spans[len(after.Spans)-MaxStopSpans:]
		}
		after.State = Stopped
	}
	after.Since, after.Who, after.Cause, after.Reason, after.Until = now, who, "", reason, until
	return after, true, st.putMachine(ctx, after)
}

// activeStopLeases captures StopReturn.tla's cancellation debt at the STOP
// fence (docs/SPEC-SPRINT.md section 14).
func activeStopLeases(s *sprint.Snapshot) []StopLease {
	var active []StopLease
	for _, table := range []struct {
		t    *sprint.Table
		name string
		col  string
	}{{s.Fleet, sprint.Fleet, sprint.Working}, {s.Readers, sprint.Readers, sprint.Reading}} {
		if table.t == nil {
			continue
		}
		for _, row := range table.t.Rows() {
			for _, c := range table.t.Cell(row, table.col) {
				active = append(active, StopLease{Table: table.name, Row: row, ID: c.ID, Gen: max(c.Int("gen"), 1)})
			}
		}
	}
	return active
}

// unsettledStopDebt enforces StopReturn.tla ExplicitStart against each exact
// same-owner return receipt (docs/SPEC-SPRINT.md section 14).
func unsettledStopDebt(m Machine, s *sprint.Snapshot) error {
	var open []string
	for _, d := range m.StopDebt {
		if !stopDebtReturned(d, s) {
			open = append(open, d.Row+":"+d.ID+"@"+fmt.Sprint(d.Gen))
		}
	}
	// Only pre-upgrade records lack a persisted STOP-issued flag and debt.
	// New records use StopDebt as the sole lease authority; an old live lease
	// without a receipt remains a safety refusal during migration.
	if !m.StopIssued {
		for _, a := range activeStopLeases(s) {
			open = append(open, a.Row+":"+a.ID+"@"+fmt.Sprint(a.Gen))
		}
	}
	if len(open) == 0 {
		return nil
	}
	shown := open
	if len(shown) > 8 {
		shown = shown[:8]
	}
	return fmt.Errorf("%d STOP-owned work/read jobs lack same-owner cancellation receipts (%s): cancel their child processes, then stop-return each on its same owner row before start", len(open), strings.Join(shown, ", "))
}

// stopDebtReturned says the owner's same-owner, next-generation return receipt
// for d is in place on the table as read (tla/StopReturn.tla Returned).
func stopDebtReturned(d StopLease, s *sprint.Snapshot) bool {
	if d.Gen < 1 || d.Table != sprint.Fleet && d.Table != sprint.Readers {
		return false
	}
	t := s.Fleet
	ready := sprint.Ready
	if d.Table == sprint.Readers {
		t, ready = s.Readers, sprint.Asked
	}
	var c *sprint.Card
	if t != nil {
		c = t.Card(d.ID)
	}
	return c != nil && c.Placed() && c.Row == d.Row && c.Col == ready && c.Int("stopped_from_gen") == d.Gen && c.Int("gen") == d.Gen+1
}

// settleStopDebt takes each returned lease off a STOPPED machine's debt, under
// the same operation fence as STOP, START and every step (tla/StopReturn.tla
// Settle). It runs after each stop-return, a replay included: the receipt is
// read in place before any later step can move the card (the debt still pins
// it until this write), so the owner's acknowledgement is recorded durably and
// a coordinator step (hold, fleet down) may move the returned card before
// START without erasing it. A lease without its receipt stays owed.
func (st *Store) settleStopDebt(ctx context.Context) error {
	pinned, err := st.pin(ctx)
	if err != nil {
		return err
	}
	r := pinned.retry(ctx)
	for r.next(pinned.attempts()) {
		s, gen, ferr := pinned.Fenced(ctx, []string{sprint.Fleet, sprint.Readers}, nil, nil)
		if ferr != nil {
			return ferr
		}
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return err
		}
		lock := OpRecord{ID: "machine-settle-" + hex.EncodeToString(nonce[:]) + "-lock", Verb: "machine settle lock", At: pinned.now(), Lock: true}
		ok, aerr := pinned.B.Acquire(ctx, gen, lock)
		if aerr != nil {
			return aerr
		}
		if !ok {
			continue
		}
		m, _, err := pinned.Machine(ctx)
		if err == nil && !m.Running() && m.StopIssued && len(m.StopDebt) > 0 {
			var owed []StopLease
			for _, d := range m.StopDebt {
				if !stopDebtReturned(d, s) {
					owed = append(owed, d)
				}
			}
			if len(owed) < len(m.StopDebt) {
				m.StopDebt = owed
				err = pinned.putMachine(ctx, m)
			}
		}
		return errors.Join(err, pinned.B.Release(context.WithoutCancel(ctx), lock, false))
	}
	return fmt.Errorf("stop-return settle: the sprint kept changing under it (%d tries); the return is recorded on its card and START still accepts it; run the stop-return again to settle it", r.tries)
}

// settleStopDebtByBeat returns every owed fleet lease whose owner has beaten
// since the STOP and whose beat no longer names the job, without waiting for
// the owner's own stop-return receipt (tla/StopReturn.tla SettleByBeat). The
// card is returned with a recorded reason (a stop-return whose after hook
// settles the lease off the machine record, settleStopDebt). A lease whose job
// the beat still names stays owed (SettleByBeat's named guard), and an owner
// that has not beaten since the STOP is reported to the seat. It runs on
// START, before the machine record's debt check, so START never waits on a
// member's own receipt; a machine owner's beat names no job, so its lease
// stays owed until it returns it.
func (st *Store) settleStopDebtByBeat(ctx context.Context) error {
	m, _, err := st.Machine(ctx)
	if err != nil {
		return err
	}
	if m.Running() || !m.StopIssued || len(m.StopDebt) == 0 {
		return nil
	}
	s, err := st.Load(ctx, tables(sprint.Fleet, sprint.Readers), nil)
	if err != nil {
		return err
	}
	var settle []StopLease
	var reported []string
	for _, d := range m.StopDebt {
		if d.Table != sprint.Fleet {
			continue
		}
		name, friend := sprint.FriendOfRow(d.Row)
		var b sprint.Beat
		if friend {
			if b, err = st.FriendBeatOf(ctx, name); err != nil {
				return err
			}
		} else {
			var mbs map[string]sprint.Beat
			if mbs, err = st.Beats(ctx, []string{d.Row}); err != nil {
				return err
			}
			b = mbs[d.Row]
		}
		if !b.Beaten() || !b.At.After(m.Since) {
			reported = append(reported, d.Row)
			continue
		}
		if !friend {
			continue // a machine's beat names no job: its lease stays owed until it returns it
		}
		if c := s.Fleet.Card(d.ID); c != nil && sprint.FriendBeatNamesJob(b, c, s.Epoch) {
			continue
		}
		settle = append(settle, d)
	}
	for _, d := range settle {
		if _, err := st.Run(ctx, StopReturnStep(sprint.StopReturnReq{
			As:     d.Row,
			IDs:    []string{d.ID},
			Gens:   map[string]int{d.ID: d.Gen},
			Reason: "the owner's beat after the STOP no longer names it",
		})); err != nil {
			return err
		}
	}
	if len(reported) > 0 {
		slices.Sort(reported)
		to, err := st.B.Coordinator(ctx)
		if err != nil {
			return err
		}
		_, err = st.Run(ctx, Step{Verb: "tick stop debt beat", Actor: sprint.MachineActor,
			Plan: func(s *sprint.Snapshot) sprint.Plan {
				n := sprint.Note{Kind: sprint.Happened, Type: sprint.NStopDebtBeat, Who: sprint.MachineActor, At: s.Now, To: to,
					What: fmt.Sprintf("STOP debt: %s have not beaten since the STOP, so their leases stay owed until they stop-return them", strings.Join(reported, ", ")),
					Hint: "run: nova-sprint stop-return --as <owner> <card>@<gen> --reason '<observed child exit>'"}
				return sprint.Plan{Notes: []sprint.Note{n}}
			}})
		if err != nil {
			return err
		}
	}
	return nil
}

// stopWithCause serializes the tick's DONE and funds stops with a manual STOP.
// A tick that observed RUNNING before an operator's STOP re-reads it after the
// operator's fenced write, leaving that later explicit reason intact. Its
// observed START generation also prevents it from stopping a later restart.
func (st *Store) stopWithCause(ctx context.Context, cause string, runSeq uint64) (before, after Machine, changed bool, err error) {
	pinned, err := st.pin(ctx)
	if err != nil {
		return before, after, false, err
	}
	r := pinned.retry(ctx)
	for r.next(pinned.attempts()) {
		s, gen, ferr := pinned.Fenced(ctx, []string{sprint.Fleet, sprint.Readers}, nil, nil)
		if ferr != nil {
			return before, after, false, ferr
		}
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return before, after, false, err
		}
		lock := OpRecord{ID: "machine-cause-" + hex.EncodeToString(nonce[:]) + "-lock", Verb: "machine cause lock", At: pinned.now(), Lock: true}
		ok, aerr := pinned.B.Acquire(ctx, gen, lock)
		if aerr != nil {
			return before, after, false, aerr
		}
		if !ok {
			continue
		}
		before, _, err = pinned.Machine(ctx)
		if err == nil {
			if !before.Running() || before.RunSeq != runSeq {
				after = before
			} else {
				now := pinned.now()
				after = before
				after.StopIssued = true
				after.StopDebt = activeStopLeases(s)
				after.Spans = append(append([]Span(nil), before.Spans...), Span{From: now})
				if len(after.Spans) > MaxStopSpans {
					after.Spans = after.Spans[len(after.Spans)-MaxStopSpans:]
				}
				after.State, after.Since, after.Who, after.Cause = Stopped, now, sprint.MachineActor, cause
				err = pinned.putMachine(ctx, after)
				changed = err == nil
			}
		}
		err = errors.Join(err, pinned.B.Release(context.WithoutCancel(ctx), lock, false))
		if err != nil {
			return before, before, false, err
		}
		return before, after, changed, nil
	}
	return before, after, false, fmt.Errorf("machine cause: the sprint kept changing under it (%d tries); nothing was changed; run it again", r.tries)
}

// clearDoneCause is the post-add counterpart to stopWithCause. The add has
// committed, but a later operator STOP must not be replaced by an old DONE
// record that the post-add hook observed before acquiring the machine fence.
func (st *Store) clearDoneCause(ctx context.Context) error {
	pinned, err := st.pin(ctx)
	if err != nil {
		return err
	}
	r := pinned.retry(ctx)
	for r.next(pinned.attempts()) {
		_, gen, err := pinned.Fenced(ctx, nil, nil, nil)
		if err != nil {
			return err
		}
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return err
		}
		lock := OpRecord{ID: "machine-undone-" + hex.EncodeToString(nonce[:]) + "-lock", Verb: "machine undone lock", At: pinned.now(), Lock: true}
		ok, err := pinned.B.Acquire(ctx, gen, lock)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		m, _, err := pinned.Machine(ctx)
		if err == nil && m.Done() {
			m.Cause = ""
			err = pinned.putMachine(ctx, m)
		}
		err = errors.Join(err, pinned.B.Release(context.WithoutCancel(ctx), lock, false))
		return err
	}
	return fmt.Errorf("machine undone: the sprint kept changing under it (%d tries); nothing was changed; run it again", r.tries)
}
