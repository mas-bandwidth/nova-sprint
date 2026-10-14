package sprint

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/hostload"
)

// A fleet member's presence (docs/SPEC-SPRINT.md section 5, the fleet; the
// ordinary lapse is tla/DirtyTick.tla, Lapse). A member says
// it is there by beating: nova-sprint fleet beat, run on the machine every
// few seconds, writes its last beat time and its measured load. Its status is
// derived, never typed: up until it has missed MissedBeatsDown beat windows
// of BeatDeadline each in a row, down past that or when it has never beaten,
// except that a last beat at HeavyLoadPercent or above enters a bounded loaded
// interval before down. Its entry is the ordinary lapse boundary after that
// beat, not the tick that happens to observe it; the loaded interval lasts
// LoadedGrace. A hold is held whatever it beats. The tick applies a change of
// status with the moves of fleet down and fleet up.

const (
	// BeatDeadline is how long a beat is fresh, and so the length of one beat
	// window: a window with no beat after the last is one missed beat.
	BeatDeadline = 15 * time.Second
	// MissedBeatsDown is how many beat windows in a row a member misses before
	// it is down: one missed beat is a store round trip that timed out or a
	// slow machine, and marking a working member down on it withdrew its card
	// and lost the child's finish (the member's beat is one store call of
	// several seconds on a tailnet); three in a row is a member that is gone.
	// A beat between misses resets the count: it is derived from the last
	// beat, never stored.
	MissedBeatsDown = 3
	// HeavyLoadPercent is the saturation point at which the last beat marks a
	// member loaded rather than gone when its ordinary presence interval lapses.
	HeavyLoadPercent = 90.0
	// LoadedGrace is one more ordinary presence interval after a loaded member's
	// derived entry time; it bounds how long stale load can keep a dead member up.
	LoadedGrace = MissedBeatsDown * BeatDeadline
	// LoadWindow is the span of beats whose highest load the load cell shows.
	LoadWindow = 10 * time.Second
	// FriendBeatEvery is how often a friend's daemon beats (friend beat). The
	// beat is recorded and shown (her report, her load, its age), and it never
	// makes her up: it proves a daemon, not a session (FriendStatus).
	FriendBeatEvery = time.Second
	// FriendHeardUp and FriendDeafHold are the two windows of a friend's last-heard
	// time (tla/FriendHeard.tla; docs/SPEC-SPRINT.md section 1, a friend's health). The
	// owner, 2026-10-13, after friends working by hand on the bus read down for hours:
	// "any activity from the friend that proves their session is live = timeout resets
	// ... just like in a network protocol, any valid packet from that client resets the
	// timeout accum." She is up while she was last heard under FriendHeardUp ago; past
	// that she is deaf ("it's held during the 10 minutes before they actually go down"),
	// still shown up, dealt to, and keeping her taken cards, for FriendDeafHold more;
	// past both she is down. Any valid event of hers sets the time again.
	FriendHeardUp  = 10 * time.Minute
	FriendDeafHold = 10 * time.Minute
	// FriendBeatBound is how stale a friend's last beat may be before the tick treats
	// her as gone and hands her cards and reads back to the deal: her daemon alone runs
	// her lanes, so a beat this old is a daemon that has stopped (docs/SPEC-SPRINT.md
	// section 1, friend reconcile; a friend who stops beating is down).
	FriendBeatBound = 30 * time.Minute
)

// Held is the status of a member the coordinator holds down.
const Held = "held"

// HowGiven is a load given on the command line rather than measured.
const HowGiven = "given"

// LoadSample is one beat's load, a percent of all the machine's cores.
type LoadSample struct {
	At  time.Time `json:"at"`
	Pct float64   `json:"pct"`
}

// Beat is a member's presence record: its last beat, to the second, the
// highest load of its beats within LoadWindow of it, how the last one was
// measured, those beats' loads, the measuring state the next beat starts
// from, and the machine's logical cores, which a member with the default width
// takes half of (WidthOfCores; fleet sync).
type Beat struct {
	At      time.Time      `json:"at"`
	Load    float64        `json:"load"`
	Cores   int            `json:"cores,omitempty"`
	How     string         `json:"how,omitempty"`
	Samples []LoadSample   `json:"samples,omitempty"`
	Meter   hostload.State `json:"meter"`
	// Friend is what a friend's beat reports of her work (friend beat); nil on a
	// machine's beat.
	Friend *FriendReport `json:"friend,omitempty"`
	// Proof is a friend's session's last proof as her beat carried it (friend beat
	// --pong: her session's answer to a SESSION CHECK, or its own bus message), zero
	// when her beat carried none; the friend beat record keeps it under "pong".
	Proof time.Time `json:"pong,omitzero"`
	// StopReturns is how many stop-returns a member's lanes still owe after the machine's
	// stop (fleet beat --stop-returns; docs/SPEC-SPRINT.md section 14, stop cancels jobs):
	// start waits for zero. A friend's count is her report's (FriendReport.StopReturns).
	StopReturns int `json:"stop_returns,omitempty"`
	// NoRoom is a member's or reader's word that it starts no card, and why (fleet beat or
	// queue --no-room: its free disk under its floor, its usage source's rest); "" while it
	// starts cards. While its beat is fresh the deal gives it no card and the ask asks it no
	// read (NoRoomNow), so a card is not dealt to a machine that hands it straight back.
	NoRoom string `json:"no_room,omitempty"`
	// Live is the worker's live set as this beat carried it (fleet beat --live, friend beat
	// --live; live.go): <card>@<gen> for each child it runs, <card>@<gen>:held for each
	// finished report its outbox holds. LiveKnown says the beat carried one (an empty set
	// included); a beat without it (a worker from before v1.2.6) carries none, and its row
	// is not reconciled. LiveSince is when the beats began carrying one, and LiveSeen each
	// entry's last sighting, both kept by the store from beat to beat.
	Live      []string             `json:"live,omitempty"`
	LiveKnown bool                 `json:"live_known,omitempty"`
	LiveSince time.Time            `json:"live_since,omitzero"`
	LiveSeen  map[string]time.Time `json:"live_seen,omitempty"`
	// Draining is a member's or reader's word that it drains (fleet beat or queue --draining:
	// its binary was replaced under it): it takes no new card and stops when its running
	// cards end, which can be hours. DrainingSince is when the first beat that said so was
	// written, kept from beat to beat while it drains; a beat without it clears both. While
	// its beat is fresh the server holds it at no room (NoRoomNow): the deal gives it no card,
	// the ask asks it no read, and the tick's level returns what it was dealt and has not
	// taken (drainReturns; tla/NoRoom.tla, NeverDealtToDraining).
	Draining      bool      `json:"draining,omitempty"`
	DrainingSince time.Time `json:"draining_since,omitzero"`
}

// DrainingPrefix begins the no-room word the server gives a member or reader whose fresh beat
// says it drains (DrainWord): the deal, the level and the ask read it as any no-room word, and
// IsDrainWord tells it from a member's own (its disk under its floor).
const DrainingPrefix = "draining since "

// DrainWord is the no-room word of a member or reader draining since since.
func DrainWord(since time.Time) string {
	return DrainingPrefix + stamp(since) + ": its binary was replaced; it takes no new card and stops when its running cards end"
}

// IsDrainWord says the no-room word is a drain's (DrainWord).
func IsDrainWord(why string) bool { return strings.HasPrefix(why, DrainingPrefix) }

// NoRoomNow is the word of a beat fresh at now that its member or reader starts no card,
// "" when it says none or is not fresh. A beat that says it drains is a drain's word
// (DrainWord), its own no-room word after it when it says one too.
func NoRoomNow(b Beat, now time.Time) string {
	if !b.Fresh(now) {
		return ""
	}
	if b.Draining {
		since := b.DrainingSince
		if since.IsZero() {
			since = b.At
		}
		if b.NoRoom != "" {
			return DrainWord(since) + "; " + b.NoRoom
		}
		return DrainWord(since)
	}
	return b.NoRoom
}

// DrainingFrom is the drain stamp of a beat written now with draining said or not, from the
// beat before it (prev): its first drain beat stamps now, a later one keeps the stamp, and a
// beat that does not say it clears it.
func DrainingFrom(prev Beat, draining bool, now time.Time) (bool, time.Time) {
	switch {
	case !draining:
		return false, time.Time{}
	case prev.Draining && !prev.DrainingSince.IsZero():
		return true, prev.DrainingSince
	}
	return true, now.UTC().Truncate(time.Second)
}

// FriendReport is what a friend's machinery reports with her beat, as a machine's beat
// reports its load (the beat's Load, given): the cards she is running (work card ids or her
// job names), which friend take and friend down leave with her (FriendTake), and her own
// counts as her daemon keeps them (working, queued, her width), each absent when not
// reported. They are her word, shown beside the table's counts, which stay the sprint's
// own (her row's cards) and her width the roster's; her last activity is the file system's.
type FriendReport struct {
	Running []string `json:"running,omitempty"`
	Working *int     `json:"working,omitempty"`
	Queue   *int     `json:"queue,omitempty"`
	Width   *int     `json:"width,omitempty"`
	// StopReturns is how many stop-returns her lanes still owe after the machine's stop
	// (friend beat --stop-returns; docs/SPEC-SPRINT.md section 14, stop cancels jobs):
	// start waits for zero. Absent when not reported.
	StopReturns *int `json:"stop_returns,omitempty"`
	// Active is the newest write under her working directory and outbox as her daemon
	// last walked them (friend beat --active), zero when it reported none: the signal that
	// her session moves, which a daemon pong does not say (docs/SPEC-FRIEND.md, last
	// session activity).
	Active time.Time `json:"active,omitzero"`
	// Paced and Window are her lanes' effective width under her subscription windows' pacing
	// and those windows' use as her harness last reported it ("5h 62% 7d 31%"), as her daemon
	// last read them (docs/SPEC-FRIEND.md, subscription pacing); absent when it reported none.
	Paced  *int   `json:"paced,omitempty"`
	Window string `json:"window,omitempty"`
	// Until and Reason are her daemon's word that she is down until then and why (friend
	// beat --until --reason: her harness at its usage limit or out of credits,
	// docs/SPEC-FRIEND.md, limits-mean-down-w-r5.w1~15); zero and empty while she is up.
	Until  time.Time `json:"until,omitzero"`
	Reason string    `json:"reason,omitempty"`
	// Build, Started and Present are her daemon's own facts (friend beat --build --started
	// --present): the build it runs (its version line's build), when it started, which is its
	// generation, and when it sent her the present on its start (the note that snaps her to
	// now and skips what is old); each absent when it reported none. A start the status
	// transitions have not seen is a new generation of her daemon (StatusTransitions).
	Build   string    `json:"build,omitempty"`
	Started time.Time `json:"started,omitzero"`
	Present time.Time `json:"present,omitzero"`
}

// SaysDown says the beat is her daemon's word that she is down (FriendReport.Until):
// however fresh, it never makes her up.
func (b Beat) SaysDown() bool { return b.Friend != nil && !b.Friend.Until.IsZero() }

// Beaten says the member has beaten at least once.
func (b Beat) Beaten() bool { return !b.At.IsZero() }

// Fresh says the last beat is within BeatDeadline of now.
func (b Beat) Fresh(now time.Time) bool {
	return b.Beaten() && now.Sub(b.At) <= BeatDeadline
}

// Missed is how many beat windows of BeatDeadline went by after the last beat
// with no beat: 0 while it is fresh, 1 until two windows have passed, and so
// on; a member that never beat has missed none (it is down by never beating).
func (b Beat) Missed(now time.Time) int {
	if !b.Beaten() || b.Fresh(now) {
		return 0
	}
	return int((now.Sub(b.At) - 1) / BeatDeadline)
}

// LoadedSince is when the last beat's heavy-load evidence enters its extra
// presence interval: the ordinary lapse boundary, whether or not a tick ran
// there (docs/SPEC-SPRINT.md section 5; tla/DirtyTick.tla, Lapse).
func (b Beat) LoadedSince() (time.Time, bool) {
	if !b.Beaten() || b.Load < HeavyLoadPercent {
		return time.Time{}, false
	}
	return b.At.Add(MissedBeatsDown * BeatDeadline), true
}

// Loaded says the member is in the bounded grace after its ordinary presence
// boundary. The entry derives from the last beat, so repeated ticks cannot
// restart its grace (docs/SPEC-SPRINT.md section 5; tla/DirtyTick.tla, Lapse).
func (b Beat) Loaded(now time.Time) bool {
	since, ok := b.LoadedSince()
	return ok && now.After(since) && !now.After(since.Add(LoadedGrace))
}

// Alive says the member has missed fewer than MissedBeatsDown windows, or is
// inside its bounded loaded interval (docs/SPEC-SPRINT.md section 5;
// tla/DirtyTick.tla, Lapse).
func (b Beat) Alive(now time.Time) bool {
	return b.Beaten() && (b.Missed(now) < MissedBeatsDown || b.Loaded(now))
}

// NextBeat is the record after a beat at now with the load pct: the beat at
// the second, the samples of the last LoadWindow with this one in place of any
// of the same second, and their highest as the load. Two beats in the same
// second with the same load write the same record.
func NextBeat(prev Beat, now time.Time, pct float64, how string, meter hostload.State) Beat {
	at := now.UTC().Truncate(time.Second)
	b := Beat{At: at, How: how, Meter: meter}
	for _, s := range prev.Samples {
		if s.At.After(at.Add(-LoadWindow)) && s.At.Before(at) {
			b.Samples = append(b.Samples, s)
		}
	}
	b.Samples = append(b.Samples, LoadSample{At: at, Pct: float64(int64(pct*10+0.5)) / 10})
	for _, s := range b.Samples {
		b.Load = max(b.Load, s.Pct)
	}
	return b
}

// MemberStatus is a member's derived status at now: held while the
// coordinator holds it, else up while it has missed fewer than
// MissedBeatsDown beat windows, else down.
func MemberStatus(ctl *Card, b Beat, now time.Time) string {
	return PresenceStatus(ctl.F("held") != "", b, now)
}

// PresenceStatus is the one rule of a fleet member's status at now: held while
// the coordinator holds it (fleet down), else up while it has missed fewer
// than MissedBeatsDown beat windows of BeatDeadline, else down (never beaten,
// or lapsed). // tla/DirtyTick.tla Hold
func PresenceStatus(held bool, b Beat, now time.Time) string {
	switch {
	case held:
		return Held
	case b.Alive(now):
		return Up
	}
	return Down
}

// The phases of a friend's presence (tla/FriendHeard.tla). Five internal words, three
// shown: up, deaf and asleep are the machine's, and the tables, the dashboard, friend
// health and every JSON row show only up, down and held ("only show up/down -- if you
// are deaf, but up, that's fine", the owner, 2026-10-13): deaf shows and behaves as up,
// asleep as down. The phase stays in the evidence text, the logs and the model.
const (
	// Deaf is a friend last heard between FriendHeardUp and FriendHeardUp+FriendDeafHold
	// ago: still dealt to, her taken cards kept, a seat judgment naming how to re-arm
	// her receiver (deafConds); she is up again at her next valid event and down when
	// FriendDeafHold passes with none.
	Deaf = "deaf"
	// Asleep is a down friend whose daemon answers and whose session does not: a fresh
	// beat, or a daemon pong the coordinator observed (DaemonPong). It is the label of
	// a down, so no deal and her cards go back; a wake she answers is a valid event and
	// brings her straight up ("if you are asleep, you are down", the owner, 2026-10-13).
	Asleep = DaemonPong
)

// FriendStatus is the one rule of a friend's status at now, in the words the tables
// show: held while the coordinator holds her (friend down); else down while her last beat
// says so (SaysDown: her harness at its limit, until when and why; a beat can say down,
// never up); else up while she is up or deaf, down when she is down or asleep
// (FriendPhase, the last-heard rule). Her beat itself, whoever sends it, is never
// evidence: a daemon or a loop beating for her says an app is open, not that her session
// can work. Releasing a hold (friend up) is no evidence either.
func FriendStatus(f FriendPresence, now time.Time) string {
	status, _ := FriendEvidence(f, now)
	return status
}

// FriendEvidence is FriendStatus with the evidence it rests on, as her row names it: for
// up, the event and its age ("heard 3m0s ago", "session pong 3m0s ago", "finish 12m0s
// ago"); for deaf, shown up, "deaf: last heard 12m0s ago ..." with the time left; for
// down, what is missing and the age of the last event, and the beat's age when she beats,
// so a row read down while her daemon beats says why; for a beat that says down, until
// when and why; for held, "held". The word is up, down or held only (FriendPhase has the
// internal phase). tla/FriendHeard.tla Shown.
func FriendEvidence(f FriendPresence, now time.Time) (string, string) {
	phase, evidence := FriendPhase(f, now)
	switch phase {
	case Deaf:
		return Up, evidence
	case Asleep:
		return Down, evidence
	}
	return phase, evidence
}

// FriendPhase is the one rule of a friend's presence at now: the phase (Held, Up, Deaf,
// Asleep or Down) and the evidence it rests on. One last-heard time per friend: the latest
// of her recorded Heard (every valid event the server saw of her: a bus message sent or
// received, a take, a finish, a session proof, a pong), her coordinator-observed session
// pong under the current seat generation, the session proof her beat carries while that
// beat is fresh, and her last finish. A hold and a beat that says down win over all of it,
// exactly as before. Then her age since that time:
//
//   - under FriendHeardUp: up;
//   - under FriendHeardUp+FriendDeafHold: deaf, shown up, behaving as up;
//   - past that, or never heard: down, labelled asleep while her daemon answers (a fresh
//     beat, or a daemon pong the coordinator observed under the current seat) and her
//     session does not.
//
// A friend therefore reaches down only through deaf, and deaf lasts FriendDeafHold at most
// (tla/FriendHeard.tla: NoUpToDown, DeafBounded). Her beat is no event: a daemon beating
// for a dead session proves an app is open (asleep, then), and it is the beat alone that a
// pong or proof she carried is dropped for once the daemon has stopped beating
// FriendBeatBound ago, since her daemon alone runs her lanes; a recorded Heard is not
// dropped with it, so a friend who works by hand after her old daemon stopped is up.
func FriendPhase(f FriendPresence, now time.Time) (string, string) {
	if f.Held {
		return Held, "held"
	}
	if f.Beat.SaysDown() {
		return Down, beatSaysDownWhy(f.Beat)
	}
	stopped := f.Beat.Beaten() && now.Sub(f.Beat.At) > FriendBeatBound
	at, word := lastHeard(f, now, stopped)
	if !at.IsZero() {
		age := now.Sub(at)
		switch {
		case age < FriendHeardUp:
			return Up, word + " " + ago(age)
		case age < FriendHeardUp+FriendDeafHold:
			return Deaf, fmt.Sprintf("deaf: last heard %s (%s), nothing since; still dealt to, the cards kept, and down in %s without a valid event; %s",
				ago(age), word, (FriendHeardUp + FriendDeafHold - age).Truncate(time.Second), cameUp)
		}
	}
	if stopped {
		return Down, "daemon stopped beating " + ago(now.Sub(f.Beat.At)) + ": down since " + stamp(f.Beat.At)
	}
	why := "no session evidence: nothing heard from the friend within " + (FriendHeardUp + FriendDeafHold).String() +
		" (a bus message sent or received, a take, a finish, a session proof or an answered wake ping)"
	if at.IsZero() {
		why += ", never heard"
	} else {
		why += ", last heard " + ago(now.Sub(at)) + " (" + word + ")"
	}
	asleep := f.Beat.Fresh(now) ||
		f.Health.State == DaemonPong && f.Health.Generation == f.Generation && !f.Health.Seen.IsZero() && now.Sub(f.Health.Seen) < FriendHeardUp+FriendDeafHold
	if f.Beat.Beaten() {
		why += "; beat " + ago(now.Sub(f.Beat.At)) + " is not evidence"
	}
	if asleep {
		return Asleep, "asleep: the friend's daemon answers and the friend's session does not; " + why + "; wake the friend: " + cameUp
	}
	return Down, why + "; " + cameUp
}

// cameUp is how a friend comes up, in the words her row and a refused take carry: any
// valid event of the friend's resets the time.
const cameUp = "any valid event brings the friend up at once: nova-bus send or recv as the friend, nova-sprint take or finish as the friend, a session proof"

// lastHeard is the latest event of the friend at or before now, with the word her row
// says it in. A time after now is no evidence (a proof from the future renews nothing).
// With stopped set (her daemon stopped beating FriendBeatBound ago) the pong, proof and
// finish a daemon-run friend carried are dropped; her recorded Heard is kept. A tie is
// worded by the more specific event: pong, proof, finish, then heard.
func lastHeard(f FriendPresence, now time.Time, stopped bool) (time.Time, string) {
	var at time.Time
	var word string
	take := func(t time.Time, w string) {
		if !t.IsZero() && !t.After(now) && t.After(at) {
			at, word = t, w
		}
	}
	if !stopped {
		if f.Health.State == Up && f.Health.Generation == f.Generation {
			take(f.Health.Seen, "session pong")
		}
		if f.Beat.Fresh(now) {
			take(f.Beat.Proof, "session proof")
		}
		take(f.Finished, "finish")
	}
	take(f.Heard, "heard")
	return at, word
}

// beatSaysDownWhy is why a beat that says down puts her down (SaysDown): until
// when, and the reason her daemon gave.
func beatSaysDownWhy(b Beat) string {
	why := "beat says down until " + b.Friend.Until.UTC().Format(time.RFC3339)
	if b.Friend.Reason != "" {
		why += ": " + b.Friend.Reason
	}
	return why
}

// ago is an age as a row says it, to the second: "3m0s ago", or "in the
// future" for a stamp after now, which is no evidence.
func ago(d time.Duration) string {
	if d < 0 {
		return "in the future"
	}
	return d.Truncate(time.Second).String() + " ago"
}

// LoadText is the load cell: the highest load of the last LoadWindow with
// one decimal and a percent sign while the beat is fresh, else empty, except
// that a stale heavy-load beat is retained and marked "loaded" for LoadedGrace
// so the fleet distinguishes its grace from down. Beside it, while the
// machine's open file descriptors are over the warn bound, it shows their
// level (fd.go FilesText).
func LoadText(b Beat, now time.Time) string {
	if !b.Fresh(now) && !b.Loaded(now) {
		return ""
	}
	load := fmt.Sprintf("%.1f%%", b.Load)
	if b.Loaded(now) {
		load += " loaded"
	}
	if f := FilesText(b, now); f != "" {
		load += " fds " + f
	}
	return load
}

// TickPresence applies the changes of the members' derived status (T0), every
// member whose status changes in the one plan (the design's R1 and R2, v2.1
// section 2.3: each member seen or down is its own key, none waits behind
// another's; the owner's rule: every row of every table
// moves every tick, never a row at a time). Every member that should be down
// and is up goes down, its unfinished work cards dealt round the members up
// after the plan (downPlan: the loads and the rolling index shared across
// every down member, so the cards of all of them go round the fleet together)
// or withdrawn when none is; every member that should be up and is not comes
// up, up to TickMaxMoves of them. With no member going down, the first up
// levels the ready queues once over every member up after the plan; with
// downs, the level part of the same tick evens the queues after the deal. So
// a fleet that beats before start is up, whole, at the first tick, and a
// fleet that loses several machines at once redeals all their cards in that
// tick. The binding gives it the beats; with none given it does nothing. Each
// status that changed, a member's or a friend's, raises its one judgment in the
// same plan (StatusTransitions, judgments_status.go). A member up and pegged with no
// card working for the pegged bound raises its one judgment of the episode here too
// (tickPegged, pegged.go).
func TickPresence(s *Snapshot, r TickReq) (Plan, int) {
	p, due := presence(s, r) // the rest are due: the next ticks apply them
	t := StatusTransitions(s, r)
	p.Units = append(p.Units, t.Units...)
	p.Notes = append(p.Notes, t.Notes...)
	p.Closes = append(p.Closes, t.Closes...)
	p.Updates = append(p.Updates, t.Updates...)
	p.Props = append(p.Props, t.Props...)
	g, gdue := tickPegged(s, r)
	p.Units = append(p.Units, g.Units...)
	p.Notes = append(p.Notes, g.Notes...)
	p.Closes = append(p.Closes, g.Closes...)
	p.Updates = append(p.Updates, g.Updates...)
	p.Props = append(p.Props, g.Props...)
	return p, due + gdue
}

func presence(s *Snapshot, r TickReq) (Plan, int) {
	if r.Beats == nil {
		return Plan{}, 0
	}
	var live, ups, downs []string
	for _, m := range s.Members() {
		ctl := s.MemberCtl(m)
		if ctl == nil {
			continue
		}
		want := MemberStatus(ctl, r.Beats[m], s.Now) == Up
		have := ctl.F("status") == Up
		switch {
		case want && have:
			live = append(live, m)
		case want:
			ups = append(ups, m)
		case have:
			downs = append(downs, m)
		}
	}
	if len(ups) == 0 && len(downs) == 0 {
		return Plan{}, 0
	}
	n := min(len(ups), TickMaxMoves)
	// every member up after the plan: the receivers of the downs' cards, and
	// the members the first up levels over
	all := append(append([]string(nil), live...), ups[:n]...)
	// every placement goes round the fleet from the deal's index and moves it
	// (round.go), written with the plan
	rr := dealRoundWith(s, all...)
	moves := roundMoves{}
	var p Plan
	add := func(q Plan) {
		p.Rows = append(p.Rows, q.Rows...)
		p.Units = append(p.Units, q.Units...)
		p.Refused = append(p.Refused, q.Refused...)
	}
	receivers := orderLike(s.Members(), all, "")
	q, widths := memberLoads(s, receivers), memberWidths(s, receivers)
	for _, m := range downs {
		why := "no beat for " + (MissedBeatsDown * BeatDeadline).String()
		if r.Beats[m].Load >= HeavyLoadPercent && r.Beats[m].Beaten() {
			why = "no beat for " + (MissedBeatsDown*BeatDeadline + LoadedGrace).String() + " after its loaded interval"
		}
		switch {
		case s.MemberCtl(m).F("held") != "":
			why = "held"
		case !r.Beats[m].Beaten():
			why = "it has never beaten"
		}
		// a member down because it stopped beating is the machine's down, held
		// by the machine for the fault: the tick clears the mark itself once the
		// member beats and finishes a card cleanly (faultClear;
		// tla/DealFill.tla FaultHold)
		add(downPlan(s, FleetReq{Op: "down", Member: m, Who: r.who(), Live: receivers, Why: why, HeldBy: HeldByFault}, receivers, rr, moves, q, widths))
	}
	for i, m := range ups[:n] {
		// the first up levels the queues over every member up after the plan
		// when no member went down; the others are their control cards and
		// notifications alone
		levelWith := []string{}
		if i == 0 && len(downs) == 0 {
			levelWith = all
		}
		add(fleetStepPlan(s, FleetReq{Op: "up", Member: m, Who: r.who(), Live: levelWith, Why: "it beats"}, rr, moves))
	}
	p = Lawful(p)
	roundWrites(&p, rr, moves)
	return p, len(ups) - n
}

// StrangerNotes is the plan that tells the coordinator of each unknown machine
// that beats and was not told of yet: one happened notification for each name,
// in name order, none for a machine that has a row in the fleet by now.
func StrangerNotes(s *Snapshot, names []string) Plan {
	var p Plan
	for _, m := range slices.Sorted(slices.Values(names)) {
		if s.Fleet.HasRow(m) {
			continue
		}
		p.Notes = append(p.Notes, Note{Kind: Happened, Type: NUnknownMachine, Who: MachineActor, At: s.Now,
			What: "an unknown machine is beating: " + m + "; add it with nova-sprint fleet up " + m})
	}
	return p
}

// FriendDownWhy is why FriendStatus does not say up at now, in the words a take refused
// for her names (takeOne): held by the coordinator; her beat says down, until when and why; or the session evidence she lacks as
// FriendEvidence names it (her beat is never evidence). "" while she is up.
func FriendDownWhy(f FriendPresence, now time.Time) string {
	if f.Held {
		return "held by the coordinator (friend down)"
	}
	word, why := FriendEvidence(f, now)
	if word == Up {
		return ""
	}
	return why
}
