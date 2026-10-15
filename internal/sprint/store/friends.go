package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/mas-bandwidth/nova-tools/pkg/buildinfo"
)

// Sprint owns the roster, hold and work reports. Only nova-hook owns liveness.
const keyFriends = "friends"

// currentBuild is the build this process runs (buildinfo.Version): the build a friend's
// daemon is compared with when she comes up (sprint.FriendSeat.Current).
var currentBuild = buildinfo.Version("")

func friendBeatKey(friend string) string { return "friend-beat:" + friend }

// friendEntry is a friend's entry in the roster: the coordinator's hold,
// empty while released, and her width, how many jobs she works at once.
type friendEntry struct {
	Held  bool      `json:"held,omitempty"`
	At    time.Time `json:"at,omitempty"`
	By    string    `json:"by,omitempty"`
	Width int       `json:"width,omitempty"`
	// Class is her class: the tiers her nova-config row says she can do, sorted and
	// comma joined (friend level evens the friends of one class).
	Class string `json:"class,omitempty"`
	// Mode is her delivery mode, her nova-config row's (batch or one-shot),
	// which her daemon reads back from her beat; empty is batch.
	Mode string `json:"mode,omitempty"`
	// Streams and Kinds are her row's optional work restriction (nova-config's
	// streams and kinds, carried by friend sync): the comma lists the dealer
	// reads, empty for no restriction.
	Streams string `json:"streams,omitempty"`
	Kinds   string `json:"kinds,omitempty"`
	// ConfigDir is her row's config_dir, the directory a claude one-shot lane
	// runs with as CLAUDE_CONFIG_DIR, which her beat answers (row_config_dir=).
	ConfigDir string `json:"config_dir,omitempty"`
	Pronouns  string `json:"pronouns,omitempty"`
	// Session is her row's session (config.FriendSession): one-shot for a friend with no
	// live session, who runs as a member lane on her row; empty is live.
	Session string `json:"session,omitempty"`
	// TokenCap is her row's per-card token cap. TokenCapSet is whether friend
	// sync wrote one: a roster from before the field is unset, and her beat
	// answers the default. An explicit 0 is no cap; omitempty would drop that
	// 0, so the bool is the record that it was set.
	TokenCap    int64 `json:"token_cap,omitempty"`
	TokenCapSet bool  `json:"token_cap_set,omitempty"`
	// Roles is her row's roles, comma joined (builder, may-hold, reader): a read card is
	// dealt only to a friend whose roles name reader (sprint read_cards.go).
	Roles string `json:"roles,omitempty"`
	// Billing is her row's billing word, when it names one: a friend whose calls are
	// billed per use shows dollars on the tokens column, a subscription friend tokens.
	// The cost card adds the field; until it exists this is empty, which is a
	// subscription.
	Billing string `json:"billing,omitempty"`
	// Reason and Until are the hold's (friend down --reason --until, hold <friend>
	// --reason): why, and when the coordinator expects her back. Return is whether
	// the hold took her cards back (hold.go).
	Reason string    `json:"reason,omitempty"`
	Until  time.Time `json:"until,omitzero"`
	Return bool      `json:"return,omitempty"`
}

// FriendSpec is what friend sync knows of one friend: her name (a friend row
// of nova-config), her width and her class.
type FriendSpec struct {
	Name  string
	Width int
	Class string
	Mode  string // her delivery mode, config.FriendMode of her row
	// ConfigDir is her row's config_dir ("" when it names none).
	ConfigDir string
	Pronouns  string // The row's explicit pronouns, sent back by the beat.
	// Session is her row's session: config.FriendSessionOneShot for a friend with no live
	// session (a member lane on her row), else live ("").
	Session string
	// TokenCap is her row's per-card token cap. TokenCapSet is false for a
	// spec built before the field: her beat then answers the default, and an
	// explicit 0 (no cap) is TokenCapSet with TokenCap 0.
	TokenCap    int64
	TokenCapSet bool
	// Roles is her row's roles, comma joined: reader is the read cards'.
	Roles string
	// Billing is her row's billing word ("" when the row names none): a friend billed
	// per call shows dollars on the tokens column. The cost card adds the field.
	Billing string
	// Streams and Kinds are her row's optional work restriction, the comma
	// lists the dealer reads (sprint.Split; empty for no restriction).
	Streams string
	Kinds   string
}

// RestrictionWhy explains why the configured stream and kind do not fit this friend: ""
// when they do (sprint.FriendRestrictionWhy).
func (s FriendSpec) RestrictionWhy(stream, kind string) string {
	return sprint.FriendRestrictionWhy(sprint.Split(s.Streams), sprint.Split(s.Kinds), stream, kind)
}

// FriendRow is one row of the friends table as where draws it: the counts of
// her sprint cards (filled by where from her fleet row), her width and her
// status (filled by FriendRows from the roster and her beat).
type FriendRow struct {
	Pronouns string `json:"-"` // Text rendering uses the synced choice without changing the view API.
	Name     string `json:"name"`
	Ready    int    `json:"ready"`
	Working  int    `json:"working"`
	// Held and Stale are her row's working cards her live set names finished, the report
	// waiting, and those it names neither way (sprint.LiveCountKeys; v1.2.6): Working is
	// then her live runs alone.
	Held  int `json:"held,omitempty"`
	Stale int `json:"stale,omitempty"`
	// DealtFleet is the fleet's cards among them: work cards whose primary carries no
	// WHO line (sprint.FriendsDealtFleet, from the tick's where record, up to a tick behind).
	DealtFleet int    `json:"dealt_fleet"`
	Width      int    `json:"width"`
	OK         int    `json:"ok"`
	Failed     int    `json:"failed"`
	Redealt    int    `json:"redealt"`
	Status     string `json:"status"`
	Class      string `json:"class,omitempty"`
	Mode       string `json:"mode,omitempty"`
	// Session is sprint.FriendOneShot for a one-shot friend (her status is her member's beat), "" for a live one.
	Session string `json:"session,omitempty"`
	// Roles is her row's roles, comma joined (reader: she is dealt read cards).
	Roles string `json:"roles,omitempty"`
	// Streams and Kinds are her work restriction as the deal reads it, never a
	// column where draws: hidden from the row's JSON as the lock keeps the
	// friends table's shape.
	Streams string `json:"-"`
	Kinds   string `json:"-"`
	// Load and Report are what her last beat reported (friend beat --load, and
	// sprint.FriendReport), absent when it reported none.
	Load   float64              `json:"load,omitempty"`
	Report *sprint.FriendReport `json:"report,omitempty"`
	// Active is the newest write her daemon found under her working directory and
	// outbox (sprint.FriendReport.Active), zero when it reported none: the last
	// session activity column.
	Active time.Time `json:"active,omitzero"`
	// Tokens is the sum of her cards' usage records' input, output, cache read and
	// cache write, and Charged the dollars those records charged (actual where
	// reported, else predicted), both filled by FriendRows from her fleet row; a
	// subscription friend's cell shows Tokens compact, an api-billed friend's shows
	// Charged to the cent, rounded up (the friends table's tokens column).
	Tokens  int64  `json:"tokens"`
	Charged string `json:"charged,omitempty"`
	// Billing is her row's billing word, "" when it names none (a subscription).
	Billing string `json:"billing,omitempty"`
	// Beat is when her last beat came, zero when she has never beaten: how stale her
	// report is (view coordinator).
	Beat       time.Time `json:"beat,omitzero"`
	Evidence   string    `json:"evidence,omitempty"`
	SprintHeld bool      `json:"-"`
	Unhooked   bool      `json:"-"`
	// Reason and Until say why she is held or down and when she is expected
	// back, from the sprint hold or nova-hook reason.
	Reason string    `json:"reason,omitempty"`
	Until  time.Time `json:"until,omitzero"`
}

// roster is the friends record, by name; empty when there is none.
func (st *Store) roster(ctx context.Context) (map[string]friendEntry, KV, error) {
	kv, err := st.rootKV()
	if err != nil {
		return nil, nil, err
	}
	out := map[string]friendEntry{}
	raw, ok, err := kv.GetKey(ctx, keyFriends)
	if err != nil || !ok {
		return out, kv, err
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, kv, fmt.Errorf("the friends record cannot be read (%v); run: nova-sprint friend sync", err)
	}
	return out, kv, nil
}

func putRoster(ctx context.Context, kv KV, r map[string]friendEntry) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return kv.SetKey(ctx, keyFriends, string(b))
}

// NoFriendError is the refusal of a name the roster lacks: a definite
// pre-write refusal, so the verb that meets it reports refused/no, never
// unknown (every-worker-verb-answers-with-a-typed-outcome finding 1).
type NoFriendError struct {
	Friend string
	Names  []string
}

func (e *NoFriendError) Error() string {
	names := e.Names
	if len(names) == 0 {
		names = []string{"none"}
	}
	return fmt.Sprintf("no friend %s on the friends table (friends: %s): its row is nova-config's friend row; run: nova-sprint friend sync", e.Friend, strings.Join(names, ","))
}

// OneShotError is the refusal of a friend daemon's verb for a one-shot friend: no daemon or
// friend lane serves her; her row's member does (sprint.OneShotLaneRefusal).
type OneShotError struct{ Friend string }

func (e *OneShotError) Error() string { return sprint.OneShotLaneRefusal(e.Friend) }

// IsOneShot says err is, or wraps, a OneShotError.
func IsOneShot(err error) bool {
	var o *OneShotError
	return errors.As(err, &o)
}

// IsNoFriend says err is, or wraps, a NoFriendError.
func IsNoFriend(err error) bool {
	var n *NoFriendError
	return errors.As(err, &n)
}

// noFriend is the refusal of a name the roster lacks.
func noFriend(r map[string]friendEntry, friend string) error {
	return &NoFriendError{Friend: friend, Names: slices.Sorted(maps.Keys(r))}
}

// SyncFriends makes the roster the friends given (nova-config's friend rows,
// each with her width): a friend it lacks is added, released, at her width; a
// friend it has that the specs lack is taken off with her beat; a friend that
// stays keeps her hold, and her width is set from the spec. It writes nothing
// when there is nothing to change, and says who was added, who taken off and
// who stayed with a width that changed, each in name order.
func (st *Store) SyncFriends(ctx context.Context, specs []FriendSpec) (added, removed, updated []string, err error) {
	r, kv, err := st.roster(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	want := map[string]FriendSpec{}
	rosterChanged := false
	for _, s := range specs {
		want[s.Name] = s
		e, had := r[s.Name]
		switch {
		case !had:
			added = append(added, s.Name)
			rosterChanged = true
		case e.Width != s.Width || e.Class != s.Class || e.Mode != s.Mode || e.ConfigDir != s.ConfigDir || e.Pronouns != s.Pronouns || e.Session != s.Session || e.TokenCap != s.TokenCap || e.TokenCapSet != s.TokenCapSet || e.Roles != s.Roles || e.Billing != s.Billing || e.Streams != s.Streams || e.Kinds != s.Kinds:
			updated = append(updated, s.Name)
			rosterChanged = true
		}
		e.Width, e.Class, e.Mode, e.ConfigDir, e.Pronouns, e.Session, e.TokenCap, e.TokenCapSet, e.Roles, e.Billing, e.Streams, e.Kinds = s.Width, s.Class, s.Mode, s.ConfigDir, s.Pronouns, s.Session, s.TokenCap, s.TokenCapSet, s.Roles, s.Billing, s.Streams, s.Kinds
		r[s.Name] = e
	}
	for n := range r {
		if _, ok := want[n]; !ok {
			removed = append(removed, n)
			delete(r, n)
			rosterChanged = true
		}
	}
	slices.Sort(added)
	slices.Sort(removed)
	slices.Sort(updated)
	if !rosterChanged {
		return nil, nil, nil, nil
	}
	if err := putRoster(ctx, kv, r); err != nil {
		return nil, nil, nil, err
	}
	if len(removed) > 0 {
		keys := make([]string, 0, len(removed))
		for _, n := range removed {
			keys = append(keys, st.Names.Key(friendBeatKey(n)))
		}
		if _, err := st.B.DeleteKeys(ctx, keys); err != nil {
			return added, removed, updated, err
		}
	}
	return added, removed, updated, nil
}

// FriendBeat writes one beat of the friend at the store's clock, to the
// second, reporting nothing more; a friend the roster lacks is refused and
// nothing is written.
func (st *Store) FriendBeat(ctx context.Context, friend string) (sprint.Beat, error) {
	return st.FriendBeatReport(ctx, friend, sprint.FriendReport{}, nil)
}

// FriendBeatReport is FriendBeat with what her machinery reports of her work
// (sprint.FriendReport: the cards she is running, which friend take and friend
// down keep with her, and her own counts) and her load (nil: none), kept on the
// beat until the next replaces it.
func (st *Store) FriendBeatReport(ctx context.Context, friend string, rep sprint.FriendReport, load *float64) (sprint.Beat, error) {
	b, err := st.FriendBeatLive(ctx, friend, rep, load, nil)
	return b, err
}

// FriendBeatLive is the work report with her live set (friend beat --live;
// sprint.ParseLive): nil when the beat carries none, which leaves her row unreconciled
// (sprint.LiveReturns); its entries' sightings carried from the record before.
func (st *Store) FriendBeatLive(ctx context.Context, friend string, rep sprint.FriendReport, load *float64, live []string) (sprint.Beat, error) {
	r, kv, err := st.roster(ctx)
	if err != nil {
		return sprint.Beat{}, err
	}
	e, ok := r[friend]
	if !ok {
		return sprint.Beat{}, noFriend(r, friend)
	}
	if e.Session == sprint.FriendOneShot {
		// a one-shot friend has no daemon and no friend lane: her row's member beats
		// (fleet beat friend.<name>), and her status is that beat (friendRows)
		return sprint.Beat{}, &OneShotError{Friend: friend}
	}
	now := st.now().UTC().Truncate(time.Second)
	var prev sprint.Beat
	if vals, oks, err := getKeys(ctx, kv, []string{friendBeatKey(friend)}); err != nil {
		return sprint.Beat{}, err
	} else if len(oks) == 1 && oks[0] {
		_ = json.Unmarshal([]byte(vals[0]), &prev) // ignored: an unreadable record holds no check and no proof
	}
	b := sprint.Beat{At: now}
	if len(rep.Running) > 0 || rep.Working != nil || rep.Queue != nil || rep.Width != nil || !rep.Active.IsZero() {
		b.Friend = &rep // a beat that reports nothing carries no report
	}
	if rep.Build != "" || !rep.Started.IsZero() || !rep.Present.IsZero() {
		b.Friend = &rep // her daemon's own facts are a report (sprint.StatusTransitions reads them)
	}
	if load != nil {
		b.Load, b.How = *load, sprint.HowGiven
	}
	b.Live, b.LiveKnown, b.LiveSince, b.LiveSeen = sprint.NextLive(prev, now, live)
	out, err := json.Marshal(b)
	if err != nil {
		return b, err
	}
	return b, kv.SetKey(ctx, friendBeatKey(friend), string(out))
}

// SetFriendHeld holds the friend (friend down, with why and until when the
// coordinator expects her back, each "" or zero for none) or releases the hold
// (friend up), by the coordinator who, and sets her width when width is above
// zero (friend up --width; friend sync sets nova-config's again); a friend the
// roster lacks is refused.
func (st *Store) SetFriendHeld(ctx context.Context, friend string, held bool, who, reason string, until time.Time, width int) error {
	r, kv, err := st.roster(ctx)
	if err != nil {
		return err
	}
	e, ok := r[friend]
	if !ok {
		return noFriend(r, friend)
	}
	e.Held, e.At, e.By, e.Reason, e.Until = false, time.Time{}, "", "", time.Time{}
	if held {
		e.Held, e.At, e.By, e.Reason, e.Until = true, st.now().UTC().Truncate(time.Second), who, reason, until.UTC().Truncate(time.Second)
	}
	if width > 0 {
		e.Width = width
	}
	r[friend] = e
	return putRoster(ctx, kv, r)
}

// FriendRows is the friends table at now: every friend of the roster with her
// width and her status from nova-hook and the sprint hold, in the fleet table's order
// (FleetOrder: up, asleep, then held, then down, each by name). The counts
// (ready, working, ok, failed) are her sprint cards', filled by where from her
// fleet row (friend.<name>); her tokens are those cards' usage records, summed
// here from the fleet table (sprint.FriendTokensFromCards). Three reads: the
// roster, every friend's work report and the authoritative nova-hook record,
// and, when the roster has a friend, the fleet table's cards for
// the token sum. A store that keeps no records has no friends.
func (st *Store) FriendRows(ctx context.Context, now time.Time) ([]FriendRow, error) {
	rows, _, err := st.friendRows(ctx, now)
	if err != nil || len(rows) == 0 {
		return rows, err
	}
	fleet, err := st.friendFleet(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		row := sprint.FriendRow(rows[i].Name)
		cards := make([]*sprint.Card, 0, 4)
		for _, col := range []string{sprint.Ready, sprint.Working, sprint.DoneOK, sprint.DoneFailed} {
			cards = append(cards, fleet.Cell(row, col)...)
		}
		rows[i].Redealt = len(fleet.Cell(row, sprint.Redealt))
		rows[i].Tokens = sprint.FriendTokensFromCards(cards)
		rows[i].Charged = sprint.FriendChargedFromCards(cards)
	}
	return rows, nil
}

// friendFleet is the fleet table's cards, read once for the friends table's token
// sum: a friend's cards are on her row (sprint.FriendRow) in ready, working and
// the done cells. An empty table (a store whose fleet was never initialised) is
// no cards, not an error.
func (st *Store) friendFleet(ctx context.Context) (*sprint.Table, error) {
	loaded, err := st.Load(ctx, []string{sprint.Fleet}, nil)
	if err != nil {
		return nil, err
	}
	if loaded.Fleet == nil {
		return sprint.NewTable(sprint.Fleet), nil
	}
	return loaded.Fleet, nil
}

// friendRows is FriendRows and, by name, why each friend is not up
// (absent while she is up): the words a take refused for her names
// (FriendSeats).
func (st *Store) friendRows(ctx context.Context, now time.Time) ([]FriendRow, map[string]string, error) {
	r, kv, err := st.roster(ctx)
	if kv == nil {
		return nil, nil, nil // a store that keeps no records: no friend
	}
	if err != nil || len(r) == 0 {
		return nil, nil, err
	}
	names := slices.Sorted(maps.Keys(r))
	keys := make([]string, 0, len(names))
	for _, n := range names {
		keys = append(keys, friendBeatKey(n))
	}
	vals, oks, err := getKeys(ctx, kv, keys)
	if err != nil {
		return nil, nil, err
	}
	var hooks []hook.Status
	live := slices.DeleteFunc(slices.Clone(names), func(n string) bool { return r[n].Session == sprint.FriendOneShot })
	if st.ReadFriendHooks != nil && len(live) > 0 {
		hooks, _, err = st.ReadFriendHooks(ctx, live...)
		if err != nil {
			return nil, nil, fmt.Errorf("friend hook record: %w", err)
		}
	}
	byName := make(map[string]hook.Status, len(hooks))
	for _, h := range hooks {
		byName[h.Name] = h
	}
	// a one-shot friend's status is her member's beat on her row (beat:friend.<name>), the
	// fleet's one presence rule (sprint.PresenceStatus): she has no hook, lease or link
	members, err := st.oneShotBeats(ctx, kv, names, r)
	if err != nil {
		return nil, nil, err
	}
	rows := map[string]FriendRow{}
	status := map[string]string{}
	whys := map[string]string{}
	for i, n := range names {
		var b sprint.Beat
		if i < len(oks) && oks[i] {
			_ = json.Unmarshal([]byte(vals[i]), &b)
		}
		if r[n].Session == sprint.FriendOneShot {
			mb := members[n]
			word := sprint.PresenceStatus(r[n].Held, mb, now)
			row := FriendRow{Name: n, Pronouns: r[n].Pronouns, Width: r[n].Width, Status: word, Class: r[n].Class, Mode: r[n].Mode, Session: sprint.FriendOneShot, Roles: r[n].Roles, Billing: r[n].Billing, Streams: r[n].Streams, Kinds: r[n].Kinds, Load: mb.Load, Beat: mb.At, SprintHeld: r[n].Held}
			switch word {
			case sprint.Up:
				row.Evidence = "member beat"
			case sprint.Held:
				row.Reason, row.Until = r[n].Reason, r[n].Until
				row.Evidence = "held"
				whys[n] = "held by the coordinator (friend down)"
			default:
				row.Reason = "no member beat on " + sprint.FriendRow(n)
				row.Evidence = row.Reason
				whys[n] = row.Reason
			}
			rows[n], status[n] = row, word
			continue
		}
		h := byName[n]
		word := sprint.FriendHookStatus(h, r[n].Held)
		row := FriendRow{Name: n, Pronouns: r[n].Pronouns, Width: r[n].Width, Status: word, Class: r[n].Class, Mode: r[n].Mode, Roles: r[n].Roles, Billing: r[n].Billing, Streams: r[n].Streams, Kinds: r[n].Kinds, Load: b.Load, Report: b.Friend, Beat: b.At, SprintHeld: r[n].Held, Unhooked: h.State == hook.StateUnhooked}
		if b.Friend != nil {
			row.Active = b.Friend.Active
		}
		if word == sprint.Held {
			row.Reason, row.Until = r[n].Reason, r[n].Until
			row.Evidence = "held"
			whys[n] = "held by the coordinator (friend down)"
		}
		if word == sprint.Down {
			row.Reason = h.Down
			if row.Reason == "" {
				row.Reason = h.Reason
			}
			if row.Reason == "" {
				row.Reason = "hook " + h.State
			}
			if h.State == "" {
				row.Reason = "no hook record"
			}
			row.Evidence = row.Reason
			whys[n] = row.Reason
			if h.State == "" {
				row.Reason = ""
			}
		}
		if word == sprint.Up {
			row.Evidence = "nova-hook proven"
		}
		rows[n], status[n] = row, word
	}
	out := make([]FriendRow, 0, len(names))
	for _, n := range FleetOrder(names, status) {
		out = append(out, rows[n])
	}
	return out, whys, nil
}

// oneShotBeats is each one-shot friend's member beat (beat:friend.<name>, written by fleet
// beat as for any member), by name, in one read; the zero beat for one that never beat.
func (st *Store) oneShotBeats(ctx context.Context, kv KV, names []string, r map[string]friendEntry) (map[string]sprint.Beat, error) {
	var ones, keys []string
	for _, n := range names {
		if r[n].Session == sprint.FriendOneShot {
			ones = append(ones, n)
			keys = append(keys, beatKey(sprint.FriendRow(n)))
		}
	}
	out := make(map[string]sprint.Beat, len(ones))
	if len(ones) == 0 {
		return out, nil
	}
	vals, oks, err := getKeys(ctx, kv, keys)
	if err != nil {
		return nil, err
	}
	for i, n := range ones {
		var b sprint.Beat
		if i < len(oks) && oks[i] {
			_ = json.Unmarshal([]byte(vals[i]), &b) // an unreadable beat is none: the member reads down until it beats again
		}
		out[n] = b
	}
	return out, nil
}

// friendNames is every friend of the roster, for teardown; none when the
// store keeps no records or the record cannot be read.
func (st *Store) friendNames(ctx context.Context) []string {
	r, _, err := st.roster(ctx)
	if err != nil {
		return nil
	}
	return slices.Sorted(maps.Keys(r))
}

// FriendSeats returns every friend of the roster as a FriendSeat (with Name, Width, Status,
// Class, Mode, and Running: the cards her last beat names running).
func (st *Store) FriendSeats(ctx context.Context, now time.Time) ([]sprint.FriendSeat, error) {
	rows, whys, err := st.friendRows(ctx, now)
	if err != nil {
		return nil, err
	}
	seats := make([]sprint.FriendSeat, len(rows))
	for i, r := range rows {
		seats[i] = sprint.FriendSeat{Name: r.Name, Pronouns: r.Pronouns, Width: r.Width, Status: r.Status, Class: r.Class, Mode: r.Mode, Roles: sprint.Split(r.Roles), Streams: sprint.Split(r.Streams), Kinds: sprint.Split(r.Kinds), Why: whys[r.Name]}
		if r.Reason != "" && seats[i].Why != "" {
			seats[i].Why += ": " + r.Reason
		}
		if r.Report != nil {
			seats[i].Running, seats[i].Active = r.Report.Running, r.Report.Active
		}
		seats[i].Beat = sprint.Beat{At: r.Beat, Friend: r.Report, Load: r.Load}
		seats[i].Evidence, seats[i].Current = r.Evidence, currentBuild
		seats[i].Unhooked = r.Unhooked
		seats[i].Held = r.SprintHeld
		seats[i].Session = r.Session
	}
	return seats, nil
}

// friendSeats is every friend of the roster as the tick's deal and level see her
// (sprint.TickDeal, sprint.FriendLevel, and the attempt cap's deal, sprint.AttemptCapDeal): her name, width and status at now and what her
// beat names running, read every tick while the roster has a friend, whether or not a
// card is ready: the deal offers every ready card to the friends first, a queued change or
// a dependency resolution can make work ready later in the same tick, and a friend coming
// up is levelled on the same tick (docs/SPEC-SPRINT.md, WHO preference, and section 1,
// friend-deal-idle-lanes-first.w1); nil when it has none.
// The snapshot no longer gates the read; it stays in the signature for its callers.
func (st *Store) friendSeats(ctx context.Context, _ *sprint.Snapshot, now time.Time) ([]sprint.FriendSeat, error) {
	return st.FriendSeats(ctx, now)
}

// FriendNames is every friend of the roster in name order (the friends table's rows), for
// add's hold of a WHO line's name; none when the store keeps no records.
func (st *Store) FriendNames(ctx context.Context) ([]string, error) {
	r, kv, err := st.roster(ctx)
	if kv == nil || err != nil {
		return nil, err
	}
	return slices.Sorted(maps.Keys(r)), nil
}

// FriendSpecs reads the synced roster once for admission checks: every friend with her
// width, class, mode and work restriction, for add's and brief's hold of a named friend's
// streams and kinds (FriendSpec.RestrictionWhy); none when the store keeps no records.
func (st *Store) FriendSpecs(ctx context.Context) ([]FriendSpec, error) {
	r, _, err := st.roster(ctx)
	if err != nil {
		return nil, err
	}
	names := slices.Sorted(maps.Keys(r))
	out := make([]FriendSpec, 0, len(names))
	for _, name := range names {
		e := r[name]
		out = append(out, FriendSpec{Name: name, Width: e.Width, Class: e.Class, Mode: e.Mode, ConfigDir: e.ConfigDir, Pronouns: e.Pronouns, Session: e.Session,
			TokenCap: e.TokenCap, TokenCapSet: e.TokenCapSet, Streams: e.Streams, Kinds: e.Kinds})
	}
	return out, nil
}

// FriendBeatOf is the friend's last beat; the zero beat when she has never beaten.
func (st *Store) FriendBeatOf(ctx context.Context, friend string) (sprint.Beat, error) {
	var b sprint.Beat
	kv, err := st.rootKV()
	if err != nil || kv == nil {
		return b, err
	}
	raw, ok, err := kv.GetKey(ctx, friendBeatKey(friend))
	if err != nil || !ok {
		return b, err
	}
	// ignored: an unreadable record is no beat, which the next beat replaces (FriendRows)
	_ = json.Unmarshal([]byte(raw), &b)
	return b, nil
}

// FriendStartStep is friend sync's start receipt for one friend's cards (sprint.FriendStart;
// docs/SPEC-SPRINT.md section 1, a friend's card is working once she starts it): each card
// she has begun moves to working on her row, its deadline from now, or is stamped started
// where it is working. It runs as friend sync, the coordinator's verb.
func FriendStartStep(r sprint.FriendStartReq) Step {
	return Step{Named: len(r.IDs) > 0, Args: ArgsOf(r), Verb: "friend sync", Load: tables(sprint.Fleet), Extras: sprint.NamedExtras(sprint.Fleet, r.IDs), StartsWork: true,
		Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.FriendStart(s, r) }}
}

// FriendSpecOf is what friend sync last wrote of the friend: her width and
// her delivery mode as her nova-config row has them, which her beat answers
// so her daemon reads her row without reaching the config store; a friend the
// roster lacks is refused.
func (st *Store) FriendSpecOf(ctx context.Context, friend string) (FriendSpec, error) {
	r, _, err := st.roster(ctx)
	if err != nil {
		return FriendSpec{}, err
	}
	e, ok := r[friend]
	if !ok {
		return FriendSpec{}, noFriend(r, friend)
	}
	return FriendSpec{Name: friend, Width: e.Width, Class: e.Class, Mode: e.Mode, ConfigDir: e.ConfigDir, Pronouns: e.Pronouns, Session: e.Session, TokenCap: e.TokenCap, TokenCapSet: e.TokenCapSet, Roles: e.Roles, Billing: e.Billing, Streams: e.Streams, Kinds: e.Kinds}, nil
}

// FriendBeats is every friend of the roster with her last beat (a zero beat
// for one who never beat), for the seat check (nova-sprint seat check): the
// ages the friends table derives its status from, read as beats.
func (st *Store) FriendBeats(ctx context.Context) (map[string]sprint.Beat, error) {
	r, kv, err := st.roster(ctx)
	if kv == nil || err != nil || len(r) == 0 {
		return map[string]sprint.Beat{}, err
	}
	names := slices.Sorted(maps.Keys(r))
	keys := make([]string, 0, len(names))
	for _, n := range names {
		keys = append(keys, friendBeatKey(n))
	}
	vals, oks, err := getKeys(ctx, kv, keys)
	if err != nil {
		return nil, err
	}
	out := make(map[string]sprint.Beat, len(names))
	for i, n := range names {
		var b sprint.Beat
		if i < len(oks) && oks[i] {
			_ = json.Unmarshal([]byte(vals[i]), &b) // ignored: an unreadable record is no beat
		}
		out[n] = b
	}
	return out, nil
}
