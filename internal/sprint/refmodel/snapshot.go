package refmodel

import (
	"maps"
	"slices"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

// Snapshot is the whole state one tick of the scanning machine decides from:
// the four tables and the judgments open on them, and the few facts the tick
// reads beside them, which the store keeps outside the tables. It holds what
// the tick reads and nothing it writes, so a snapshot is the machine's whole
// input, and equal snapshots are decided alike.
type Snapshot struct {
	// Tables is the four tables (unplaced records of the needs of waiting
	// primaries included, as the tick loads them), the open judgments and
	// the acknowledged conditions, the coordinator and the epoch. Its Now is
	// not read: the time is an argument of Decide.
	Tables *sprint.Snapshot
	// Running says the machine is RUNNING. A STOPPED machine moves nothing.
	Running bool
	// Since is when the machine last started: the first tick after it pushes
	// every reminder.
	Since time.Time
	// Stopped is the spans the machine was STOPPED: a deadline compares
	// running time, which leaves them out.
	Stopped []sprint.Span
	// Beats is each fleet member's last beat record; a member that never
	// beat has none. nil is none read, and the presence duty does nothing.
	Beats map[string]sprint.Beat
	// Goals is the sprint's people, their goals and routes.
	Goals sprint.Goals
	// Untold is the unknown machines that beat and whom the coordinator was
	// not told of yet.
	Untold []string
	// Friends is the friends' seats as the roster has them (name, width, status,
	// class): whom the deal gives a friend's card. nil is none
	// read, and no friend is dealt a card.
	Friends []sprint.FriendSeat
}

// Clone is a copy of the snapshot that shares nothing with it, so what is done
// to one is never seen in the other. Every duty decides on a clone, which is
// why deciding leaves the caller's snapshot as it was, whatever the planners
// do to what they read.
func (s Snapshot) Clone() Snapshot {
	c := s
	c.Tables = cloneTables(s.Tables)
	c.Stopped = slices.Clone(s.Stopped)
	c.Beats = cloneBeats(s.Beats)
	c.Goals = sprint.Goals{People: slices.Clone(s.Goals.People), Noted: maps.Clone(s.Goals.Noted)}
	c.Untold = slices.Clone(s.Untold)
	c.Friends = cloneFriends(s.Friends)
	return c
}

// needTables refuses a snapshot that lacks one of the four tables, in words,
// rather than let a planner find the nil.
func (s Snapshot) needTables() {
	t := s.Tables
	switch {
	case t == nil:
		panic("refmodel: the snapshot has no tables")
	case t.Work == nil || t.Readers == nil || t.Merge == nil || t.Fleet == nil:
		panic("refmodel: the snapshot lacks one of the four tables: the tick reads them all")
	}
}

// stopped is the running-time function the tick is given: the time the
// machine was STOPPED between two clock readings.
func (s Snapshot) stopped() func(from, to time.Time) time.Duration {
	return func(from, to time.Time) time.Duration { return sprint.StoppedBetween(s.Stopped, from, to) }
}

// tickReq is what the tick is given beside its tables.
func (s Snapshot) tickReq() sprint.TickReq {
	return sprint.TickReq{Who: sprint.MachineActor, Stopped: s.stopped(), Beats: s.Beats, Friends: s.Friends}
}

// cloneTables preserves the public input and starts fresh derived planner
// caches, so Snapshot.Clone shares no mutable state with its source.
func cloneTables(s *sprint.Snapshot) *sprint.Snapshot {
	if s == nil {
		return nil
	}
	c := &sprint.Snapshot{
		Now: s.Now, Epoch: s.Epoch, Prefix: s.Prefix, Cleared: s.Cleared,
		Coordinator: s.Coordinator, Actor: s.Actor, Self: s.Self, SeatGeneration: s.SeatGeneration,
		Work: cloneTable(s.Work), Readers: cloneTable(s.Readers), Merge: cloneTable(s.Merge), Fleet: cloneTable(s.Fleet),
		Open: cloneOpen(s.Open), Acked: cloneOpen(s.Acked), Answered: maps.Clone(s.Answered),
		QueueLen: s.QueueLen, Queue: cloneQueue(s.Queue), Held: maps.Clone(s.Held),
		Routes: slices.Clone(s.Routes), Tiers: maps.Clone(s.Tiers), Providers: maps.Clone(s.Providers),
		DecideBounce: s.DecideBounce, DecideReview: s.DecideReview, DecideScoreBar: s.DecideScoreBar,
		DecideAttemptNoResult: s.DecideAttemptNoResult, DecideAttemptNothingToDo: s.DecideAttemptNothingToDo, DecideGrade: s.DecideGrade,
		DecideGateFlaky: s.DecideGateFlaky, DecideGatePreexisting: s.DecideGatePreexisting,
		RulesOff: slices.Clone(s.RulesOff), GitIdentity: s.GitIdentity, GitIdentityUnapplied: s.GitIdentityUnapplied, MaxAttempts: s.MaxAttempts,
		ReaderStates: maps.Clone(s.ReaderStates), NoRoom: maps.Clone(s.NoRoom),
		Friends: cloneFriends(s.Friends), FriendTextPronouns: maps.Clone(s.FriendTextPronouns), Running: s.Running,
	}
	for tier, routes := range c.Tiers {
		c.Tiers[tier] = slices.Clone(routes)
	}
	return c
}

// CarriedProps is the table properties the model carries, each read by a duty to decide as
// the tick does: the status transitions' record (the presence duty) and the landing stall's
// facts and episode (the deadlines duty, sprint.TickLandingStall).
var CarriedProps = []string{sprint.PropStatusSeen, sprint.PropLastLanding, sprint.PropLandRefusal,
	sprint.PropLandingStallSince, sprint.PropLandingStallSaid, sprint.PropLandingStallBound, sprint.PropReadCards}

func cloneTable(t *sprint.Table) *sprint.Table {
	if t == nil {
		return nil
	}
	c := sprint.NewTable(t.Name)
	c.Epoch, c.Revision = t.Epoch, t.Revision
	c.SetRows(slices.Clone(t.Rows()))
	carried := map[string]string{}
	for _, name := range CarriedProps {
		if v, ok := t.Prop(name); ok {
			carried[name] = v
		}
	}
	if len(carried) > 0 {
		c.SetProps(carried)
	}
	for row, texts := range t.Texts {
		c.Texts[row] = maps.Clone(texts)
	}
	for _, card := range t.Cards() {
		c.Put(&sprint.Card{ID: card.ID, Row: card.Row, Col: card.Col, Score: card.Score, Rev: card.Rev, Fields: maps.Clone(card.Fields)})
	}
	return c
}

func cloneOpen(os []sprint.Open) []sprint.Open {
	if os == nil {
		return nil
	}
	out := make([]sprint.Open, len(os))
	for i, o := range os {
		n := o.Note
		n.Primaries, n.Decisions = slices.Clone(n.Primaries), slices.Clone(n.Decisions)
		n.Suspects, n.Needs = slices.Clone(n.Suspects), slices.Clone(n.Needs)
		out[i] = sprint.Open{Key: o.Key, Note: n}
	}
	return out
}

func cloneBeats(bs map[string]sprint.Beat) map[string]sprint.Beat {
	if bs == nil {
		return nil
	}
	out := make(map[string]sprint.Beat, len(bs))
	for m, b := range bs {
		out[m] = cloneBeat(b)
	}
	return out
}

// cloneBeat owns all readings and report values carried in Snapshot.Clone.
func cloneBeat(b sprint.Beat) sprint.Beat {
	b.Samples = slices.Clone(b.Samples)
	b.Live = slices.Clone(b.Live)
	b.LiveSeen = maps.Clone(b.LiveSeen)
	b.Meter.Ticks = clonePointer(b.Meter.Ticks)
	b.Meter.Files = clonePointer(b.Meter.Files)
	if b.Meter.Files != nil {
		b.Meter.Files.Top = slices.Clone(b.Meter.Files.Top)
	}
	b.Friend = clonePointer(b.Friend)
	if b.Friend != nil {
		b.Friend.Running = slices.Clone(b.Friend.Running)
		b.Friend.Working = clonePointer(b.Friend.Working)
		b.Friend.Queue = clonePointer(b.Friend.Queue)
		b.Friend.Width = clonePointer(b.Friend.Width)
		b.Friend.StopReturns = clonePointer(b.Friend.StopReturns)
		b.Friend.Paced = clonePointer(b.Friend.Paced)
	}
	return b
}

// cloneFriends owns the seat restrictions and nested beat in Snapshot.Clone.
func cloneFriends(fs []sprint.FriendSeat) []sprint.FriendSeat {
	out := slices.Clone(fs)
	for i := range out {
		out[i].Tiers = slices.Clone(out[i].Tiers)
		out[i].Roles = slices.Clone(out[i].Roles)
		out[i].Streams = slices.Clone(out[i].Streams)
		out[i].Kinds = slices.Clone(out[i].Kinds)
		out[i].Running = slices.Clone(out[i].Running)
		out[i].Beat = cloneBeat(out[i].Beat)
	}
	return out
}

// cloneQueue owns each queued property and batch entry in Snapshot.Clone.
func cloneQueue(q []sprint.QueuedChange) []sprint.QueuedChange {
	out := slices.Clone(q)
	for i := range out {
		out[i].Prop = clonePointer(out[i].Prop)
		out[i].Entry = cloneEntry(out[i].Entry)
	}
	return out
}

func cloneEntry(e *ntable.BatchMemberEntry) *ntable.BatchMemberEntry {
	out := clonePointer(e)
	if out == nil {
		return nil
	}
	out.Set = maps.Clone(out.Set)
	out.Unset = slices.Clone(out.Unset)
	out.Create = clonePointer(out.Create)
	out.Move = clonePointer(out.Move)
	if out.Move != nil {
		out.Move.Score = clonePointer(out.Move.Score)
	}
	out.Expect = clonePointer(out.Expect)
	if out.Expect != nil {
		out.Expect.Place = clonePointer(out.Expect.Place)
		out.Expect.Fields = maps.Clone(out.Expect.Fields)
		for key, guard := range out.Expect.Fields {
			guard.Equals = clonePointer(guard.Equals)
			guard.Absent = clonePointer(guard.Absent)
			guard.OneOf = slices.Clone(guard.OneOf)
			out.Expect.Fields[key] = guard
		}
	}
	return out
}

// clonePointer copies a present value while preserving the absent input.
func clonePointer[T any](p *T) *T {
	if p == nil {
		return nil
	}
	c := *p
	return &c
}
