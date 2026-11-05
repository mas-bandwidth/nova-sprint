package main

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-sprint/pkg/friend"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
)

// The friends table (docs/SPEC-SPRINT.md section 1; the owner, 2026-10-02: "add a
// friends table, above fleet and below merge. friends | status for now.
// up/down/held"; "friends should be configured in nova-config"; "you should use
// heartbeats from each friend to track their state, and sort them alphabetically,
// and then by status, like with fleet"; and the same day: "please give friends in
// the friends table the same ready, working, width, done, ok%, status that we have
// for machines, but no load, since they don't correspond to a machine (at the
// moment...)". The roster is nova-config's friend rows alone, copied into the store by
// friend sync, which also writes each friend's width. A friend's counts are her
// sprint cards' (the cards dealt to her fleet row friend.<name>, their states
// and their finish verdicts), read from the fleet table by where. A friend runs her
// own loop: she takes her cards (take --as friend.<name>), works them and finishes
// them (finish --as friend.<name>); nothing delivers or collects them (Glenn,
// 2026-11-05). The coordinator holds one with friend down and releases it with
// friend up. The
// status is the server's word, written by the tick from nova-hook and the sprint hold
// (store.PublishFriendStatus) and read where it is shown (store.FriendRows), never
// derived there, and sorted in the fleet's order.

// friendWords is how a friend's row comes about, in nova-sprint help and
// nova-sprint help friend.
func friendWords() string {
	return strings.TrimSpace(`
The friends: the friends table is nova-config's friend rows, copied into the
store by friend sync (--pg, else NOVA_PG_DSN, as nova-config takes it; with
--every <d> it syncs again each time d passes until interrupted, acting as the
seat each pass, docs/FRIENDS.md; friend sync
install --every <d> runs that loop as this machine's own service, a launchd agent
or a systemd user unit, and friend sync uninstall removes it): a friend
the store lacks is added, one nova-config no longer has is taken off with the friend's
beat, and a friend that stays keeps the friend's hold. The same sync writes each friend's width,
the jobs the friend works at once: the configured row's width (nova-config friend set
<friend> --width <n>, at least 1), `+fmt.Sprint(config.DefaultFriendWidth)+` when the row names none; and the friend's configured pronouns (pronouns: `+friend.PronounList()+`;
they/them when unstated). A row whose width is below 1 is refused with nothing changed. where counts the cards: ready, working,
width, done (ok and failed), ok% (ok over done, pooled in the footer) and
status, all from the friend's sprint cards — the cards dealt to the friend's fleet row
friend.<name>, their states and their finish verdicts.
Liveness comes only from nova-hook, and the status is the server's word: each tick
writes every friend's status (down whenever the hook record is down, held only when
the hook is up and sprint holds the friend, and up otherwise) into the store, and
where and the dashboard show that word and never work it out, so a reader with no bus
address shows what the server wrote. A friend the server has written no status for
shows down. friend hold <friend> --reason <text> sets the sprint hold. friend up releases
the sprint hold; it cannot prove the friend alive. A friend down shows
working 0: the friend's cards stay on the friend's row and count again when the friend is up; ready
and done are as they were. where draws the
friends between work and fleet in its default frame, which draws no merge
table; the friends table is drawn after merge only under where --all, up
first, then held, then down, each by name, with no load column.

WHO: friend <name> prefers a known friend while the friend is up with room.
WHO: only friend <name> waits for that friend alone. Other work, including
cards with no WHO line, goes first to subscription friends whose tiers cover
it, then to the fleet. Among eligible friends, most free room wins and name
breaks ties. The friend's room is twice the friend's width: the friend works at width
and the rest wait ready on the friend's row. nova-sprint unpin <id>... --reason <text> removes an
unstarted card's stored WHO choice without editing its brief; --stream <s>
selects a stream, and --dry-run only previews it. Work assigned to a friend
uses the friend's fleet row friend.<name>; presence never takes it back. friend take
takes back and marks what the friend has not started, and friend down takes back every card.
The seat hands a named ready card to a friend with friend give; the deal fills the rest.
A friend runs a take loop: nova-sprint take --as friend.<name> takes the next ready
card into working (and marks it started); friend stage <card>@<gen> --as friend.<name>
clones it at its carried head and writes JOB.md with the exact finish command; nova-sprint
finish --as friend.<name> <card>@<gen> --epoch <n> finishes it (--failed when it failed).
friend cards <friend> lists the cards on the friend's row with their briefs. card prints
who=; where counts it on that friend's row in the friends table.
`) + "\n"
}

// friendVerbWords is what friend beat, friend down, and friend up say on -h.
// The friends section (friendWords) stays on nova-sprint help friend. A name
// the table lacks is refused and names friend sync; friend sync exits 3 when
// the config cannot be read or holds no friend row (docs/SPEC-SPRINT.md section 1).
func friendVerbWords(name string) string {
	switch name {
	case "friend beat":
		return "Report work, load and row configuration. This report never determines liveness; nova-hook alone does. friend sync exits 3 if config cannot be read or holds no friend row.\n"
	case "friend down":
		return "hold <friend> (friend hold <friend> --reason <text>) holds the friend in sprint. --reason <text> and --until <RFC3339> describe the hold. friend sync exits 3 if config cannot be read or holds no friend row. The dashboard shows held only while nova-hook says up; down takes precedence.\n"
	case "friend up":
		return "unhold <friend> releases the sprint hold. Liveness still comes from nova-hook. friend sync exits 3 if config cannot be read or holds no friend row.\n"
	}
	return ""
}

// friendsFn reads nova-config's friend rows (friend sync, friends clean), given
// the address of the config store (its --pg, else NOVA_PG_DSN).
type friendsFn func(ctx context.Context, pg string) ([]config.Row, error)

// readFriends is the real friendsFn: the friend rows of Postgres, by
// config.ResolveDSN, bounded.
func (a *app) readFriends(ctx context.Context, pg string) ([]config.Row, error) {
	var rows []config.Row
	err := a.withConfig(ctx, pg, func(ctx context.Context, st config.Store) error {
		var err error
		rows, err = st.List(ctx, config.KindFriend)
		return err
	})
	return rows, err
}

func (a *app) cmdFriendSync(args []string, stdout, stderr io.Writer) int {
	const name = "friend sync"
	// install and uninstall put the --every loop in place as this machine's service
	// (friendsync_install.go)
	if len(args) > 0 && args[0] == "install" {
		return a.cmdFriendSyncInstall(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "uninstall" {
		return a.cmdFriendSyncUninstall(args[1:], stdout, stderr)
	}
	fs, c := a.verbSetup(name)
	pg := fs.String("pg", "", "the config store, Postgres postgres://user@host:port/db with no password (else NOVA_PG_DSN; the password from the variable NOVA_PG_PASSWORD_ENV names), as nova-config takes it")
	every := fs.Duration("every", 0, "sync now and again each time this passes, until interrupted, as the sprint's coordinator seat when no --actor is given, read again each pass (default: once); friend sync install --every <d> runs it as this machine's service and friend sync uninstall removes it (-h of each), or the friend sync loop row runs it (docs/FRIENDS.md)")
	// --root is retired with the friend delivery (2026-11-05): friend sync reads and writes no
	// friend's directory. It is accepted and ignored, said once, so a loop row or a unit that
	// still passes it keeps syncing after the adopt and is fixed by dropping it from the row.
	root := fs.String("root", "", "retired: friend sync writes no friend's directory; accepted and ignored so a loop row that still passes it keeps running (drop it from the row: nova-config loop set <row> --argv ...)")
	pos, err := parse(fs, args)
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	if len(pos) > 0 {
		return refuse(stderr, name, "takes no words, found "+oneline.Escape(pos[0]))
	}
	if *root != "" {
		fmt.Fprintf(stderr, "%s %s: NOTE --root is retired and ignored (friend sync writes no friend's directory); drop it from the loop row\n", prog, name)
	}
	if *every < 0 {
		return refuse(stderr, name, "--every wants a duration above zero, or none for one pass, got "+every.String()+"; run: nova-sprint friend sync --every 15s")
	}
	if *every > 0 {
		return a.friendSyncLoop(*c, *pg, *every, stdout, stderr)
	}
	code, _ := a.friendSyncPass(*c, *pg, stdout, stderr)
	return code
}

// friendSyncPass is one pass of friend sync on a store it opens as c says: its exit
// code, and whether it changed anything (a friend added, taken off or updated, or a
// friend row's name put on the bus store).
func (a *app) friendSyncPass(c common, pg string, stdout, stderr io.Writer) (int, bool) {
	const name = "friend sync"
	st, err := a.store(c)
	if err != nil {
		return refuse(stderr, name, err.Error()), false
	}
	ctx := context.Background()
	rows, err := a.friends(ctx, pg)
	if err != nil {
		fmt.Fprintf(stderr, "%s %s: the config cannot be read: %s; nothing was changed\n", prog, name, oneline.WithRemedy(err.Error(), "nova-config friend list"))
		return exitCannotRead, false
	}
	if len(rows) == 0 {
		fmt.Fprintf(stderr, "%s %s: the config holds no friend row, and syncing to none would take every friend off; is this the fleet's config? run: nova-config friend list; nothing was changed\n", prog, name)
		return exitCannotRead, false
	}
	// the specs: the roster and each friend's width; where counts her cards from the
	// fleet table
	specs := make([]store.FriendSpec, 0, len(rows))
	for _, r := range rows {
		n, width := r.Name, config.FriendWidth(r)
		if !sprint.ValidID(n) {
			fmt.Fprintf(stderr, "%s %s: a friend name wants letters, digits, _ and -: %s; fix the friend row in nova-config; nothing was changed\n", prog, name, oneline.Escape(n))
			return 1, false
		}
		if width < 1 {
			fmt.Fprintf(stderr, "%s %s: friend %s has width %d, and a friend's width is at least 1; run: nova-config friend set %s --width <n>; nothing was changed\n", prog, name, n, width, n)
			return 1, false
		}
		specs = append(specs, store.FriendSpec{Name: n, Width: width, Class: friendClass(r), Pronouns: config.FriendPronouns(r), Roles: friendRoles(r), Billing: r.Fields["billing"], Streams: r.Fields["streams"], Kinds: r.Fields["kinds"]})
	}
	added, removed, updated, err := st.SyncFriends(ctx, specs)
	if err != nil {
		return a.readFailed(name, err, stderr), false
	}
	var said []string
	say := func(l string) {
		said = append(said, l)
		if !c.json {
			fmt.Fprintln(stdout, l)
		}
	}
	// every friend row is a known name of the bus store each pass, so a row just
	// added can be written to before any card is dealt her (docs/FRIENDS.md)
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		names = append(names, s.Name)
	}
	enrolled, err := a.busEnroll(ctx, names...)
	if len(enrolled) > 0 {
		say("FRIEND-SYNC BUS-NAMES added=" + strings.Join(enrolled, ",") + ": the bus store now knows the friend rows")
	}
	if err != nil {
		say("FRIEND-SYNC NOTE: the friend rows' names could not all be put on the bus store's roster (" + oneline.Escape(err.Error()) + ")")
	}
	line := fmt.Sprintf("FRIEND-SYNC OK added=%s removed=%s updated=%s friends=%d", orDashStr(strings.Join(added, ","), "-"), orDashStr(strings.Join(removed, ","), "-"), orDashStr(strings.Join(updated, ","), "-"), len(rows))
	if len(added)+len(removed)+len(updated) == 0 {
		line += ": nothing to do, the friends table already matches the config"
	}
	sayOK(stdout, c.json, name, line, map[string]any{"added": orEmpty(added), "removed": orEmpty(removed), "updated": orEmpty(updated), "friends": len(rows),
		"notes": orEmpty(said)})
	return 0, len(added)+len(removed)+len(updated)+len(enrolled) > 0
}

func (a *app) cmdFriendBeat(args []string, stdout, stderr io.Writer) int {
	return a.friendBeat(context.Background(), args, func(c common) (*store.Store, error) { return a.store(c) }, stdout, stderr)
}

// friendBeat is friend beat on the store open gives it: the verb's (a.store), and the
// server's beat lane's (servelanes.go), which answer alike. It reads nothing of the app
// but its environment, so the beat lane runs it beside the line.
func (a *app) friendBeat(ctx context.Context, args []string, open func(common) (*store.Store, error), stdout, stderr io.Writer) int {
	const name = "friend beat"
	fs, c := a.verbSetup(name)
	build := fs.String("build", "", "the build the friend's tools run (their version line's build): a friend come up is told to update when it is not the server's")
	started := fs.String("started", "", "when the friend's session started, RFC3339: its generation; a start not seen before raises a status judgment")
	present := fs.String("present", "", "when the friend's session was sent the present on its start, RFC3339: the snap-to-present step of a friend come up")
	running := fs.String("running", "", "the cards the friend is running now, comma separated (work card ids, the friend's job names or primaries): friend take and friend down leave them with the friend")
	working := fs.String("working", "", "how many jobs the friend is working now, as she counts them (the deal counts her working cell, never this)")
	stopReturns := fs.String("stop-returns", "", "how many stop-returns the friend still owes after the machine's stop (section 14): start waits for zero")
	queue := fs.String("queue", "", "how many jobs the friend holds queued, as she counts them")
	width := fs.String("width", "", "the friend's width as she has it (the deal's is the roster's: friend up --width)")
	load := fs.String("load", "", "the friend's load as a percent, as fleet beat --load gives a machine's")
	active := fs.String("active", "", "the newest write under the friend's working directory, RFC3339: the friend's session's last activity")
	until := fs.String("until", "", "the friend's word that she is down until then, RFC3339: her harness at its usage limit or out of credits")
	reason := fs.String("reason", "", "why the friend is down until --until")
	friend, code := oneFriend(name, fs, args, stderr)
	if code != 0 {
		return code
	}
	rep := sprint.FriendReport{Running: sprint.Split(*running)}
	if b := strings.TrimSpace(*build); b != "" {
		rep.Build = oneline.Field(b)
	}
	for _, t := range []struct {
		flag, text string
		to         *time.Time
	}{{"--started", *started, &rep.Started}, {"--present", *present, &rep.Present}} {
		if t.text == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339, t.text)
		if err != nil {
			return refuse(stderr, name, t.flag+" wants an RFC3339 time, found "+oneline.Escape(t.text))
		}
		*t.to = at.UTC().Truncate(time.Second)
	}
	switch {
	case *until != "":
		at, err := time.Parse(time.RFC3339, *until)
		if err != nil {
			return refuse(stderr, name, "--until wants an RFC3339 time, found "+oneline.Escape(*until))
		}
		rep.Until, rep.Reason = at.UTC().Truncate(time.Second), oneline.Escape(*reason)
	case *reason != "":
		return refuse(stderr, name, "--reason says why the friend is down, and wants --until")
	}
	if *active != "" {
		at, err := time.Parse(time.RFC3339, *active)
		if err != nil {
			return refuse(stderr, name, "--active wants an RFC3339 time, found "+oneline.Escape(*active))
		}
		rep.Active = at.UTC().Truncate(time.Second)
	}
	for _, n := range []struct {
		flag, text string
		min        int
		to         **int
	}{{"--working", *working, 0, &rep.Working}, {"--queue", *queue, 0, &rep.Queue}, {"--width", *width, 1, &rep.Width}, {"--stop-returns", *stopReturns, 0, &rep.StopReturns}} {
		if n.text == "" {
			continue
		}
		v, err := strconv.Atoi(n.text)
		if err != nil || v < n.min {
			return refuse(stderr, name, fmt.Sprintf("%s wants a whole number of at least %d, found %s", n.flag, n.min, oneline.Escape(n.text)))
		}
		*n.to = &v
	}
	if !rep.Until.IsZero() && rep.Working == nil {
		zero := 0
		rep.Working = &zero // a friend down works nothing, and the store keeps no report with no count
	}
	var given *float64
	if *load != "" {
		v, err := strconv.ParseFloat(strings.TrimSuffix(*load, "%"), 64)
		if err != nil || v < 0 {
			return refuse(stderr, name, "--load wants a percent, found "+oneline.Escape(*load))
		}
		given = &v
	}
	c.orActor(friend)
	st, err := open(*c)
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	b, err := st.FriendBeatReport(ctx, friend, rep, given)
	if err != nil {
		// a friend the roster lacks is a definite pre-write refusal: the store
		// answered and nothing was written, so it is refused/no; any other error
		// is a store that did not confirm the beat, so it is unknown
		if store.IsNoFriend(err) {
			a.outcome, a.changed = "refused", "no"
		} else {
			a.outcome, a.changed = "unknown", "unknown"
		}
		fmt.Fprintf(stderr, "%s %s: %s\n", prog, name, oneline.Escape(err.Error()))
		return 1
	}
	line, facts := "FRIEND-BEAT OK "+friend+" at="+b.At.Format(time.RFC3339), map[string]any{"friend": friend, "at": b.At}
	// her row as friend sync last wrote it: her width and pronouns
	if spec, err := st.FriendSpecOf(ctx, friend); err == nil {
		line += fmt.Sprintf(" row_width=%d", spec.Width)
		facts["row_width"] = spec.Width
		pronouns := spec.Pronouns
		if pronouns == "" {
			pronouns = config.DefaultFriendPronouns
		}
		line += " row_pronouns=" + pronouns
		facts["row_pronouns"] = pronouns
	}
	for _, n := range []struct {
		key string
		v   *int
	}{{"working", rep.Working}, {"queue", rep.Queue}, {"width", rep.Width}, {"stop_returns", rep.StopReturns}} {
		if n.v != nil {
			line += fmt.Sprintf(" %s=%d", n.key, *n.v)
			facts[n.key] = *n.v
		}
	}
	if given != nil {
		line += fmt.Sprintf(" load=%.1f%%", *given)
		facts["load"] = *given
	}
	if !rep.Active.IsZero() {
		line += " active=" + rep.Active.Format(time.RFC3339)
		facts["active"] = rep.Active
	}
	for _, d := range []struct {
		key string
		at  time.Time
	}{{"started", rep.Started}, {"present", rep.Present}} {
		if !d.at.IsZero() {
			line += " " + d.key + "=" + d.at.Format(time.RFC3339)
			facts[d.key] = d.at
		}
	}
	if rep.Build != "" {
		line += " build=" + rep.Build
		facts["build"] = rep.Build
	}
	if len(rep.Running) > 0 {
		line += " running=" + strings.Join(rep.Running, ",")
		facts["running"] = rep.Running
	}
	if !rep.Until.IsZero() {
		line += " until=" + rep.Until.Format(time.RFC3339)
		facts["until"], facts["reason"] = rep.Until, rep.Reason
	}
	// the machine's word, so she stops her cards on STOPPED and starts nothing
	// (docs/SPEC-SPRINT.md section 14, stop cancels jobs)
	if word := machineWord(ctx, st); word != "" {
		line += " machine=" + word
		facts["machine"] = word
	}
	// the beat ran and wrote: it is accepted, and it changed the tables (her row
	// and the beat's evidence), so the worker reads the field, not the words
	a.outcome, a.changed = "accepted", "yes"
	facts["outcome"], facts["changed"] = a.outcome, a.changed
	sayOK(stdout, c.json, name, line, facts)
	return 0
}

// cmdFriendHold is friend down (held) and friend up (the hold released, and her width set
// by --width, as fleet up --width sets a member's).
func (a *app) cmdFriendHold(held bool, args []string, stdout, stderr io.Writer) int {
	name := map[bool]string{true: "friend down", false: "friend up"}[held]
	fs, c := a.verbSetup(name)
	var width, reason, until *string
	if held {
		reason = fs.String("reason", "", "why the friend is held, shown on the friend's row (the friend's model allowance ran out)")
		until = fs.String("until", "", "when you expect the friend back, RFC3339, shown on the friend's row")
	} else {
		width = fs.String("width", "", fmt.Sprintf("the friend's width: the jobs the friend works at once; the deal holds the friend at %d times that, ready and working; 1 to %d (default: as it is; friend sync sets it to the friend's nova-config row's again)", sprint.DealAhead, sprint.MaxWidth))
	}
	friend, code := oneFriend(name, fs, args, stderr)
	if code != 0 {
		return code
	}
	w := 0
	if width != nil && *width != "" {
		var err error
		if w, err = sprint.ParseWidth(*width); err != nil {
			return refuse(stderr, name, "--width: "+err.Error())
		}
	}
	var why string
	var back time.Time
	if held {
		why = strings.TrimSpace(*reason)
		if *until != "" {
			var err error
			if back, err = time.Parse(time.RFC3339, *until); err != nil {
				return refuse(stderr, name, "--until wants an RFC3339 time")
			}
		}
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	ctx := context.Background()
	if err := st.SetFriendHeld(ctx, friend, held, c.actor, why, back, w); err != nil {
		fmt.Fprintf(stderr, "%s %s: %s\n", prog, name, oneline.Escape(err.Error()))
		return 1
	}
	if held {
		// a held friend behaves as a held machine: what she has not started goes back to
		// ready for the friends' deal, what she has started stays with her and finishes
		started, err := a.friendStarted(ctx, st, friend)
		if err != nil {
			return a.readFailed(name, err, stderr)
		}
		say := "friend " + friend + " held"
		if why != "" {
			say += " reason=" + oneline.Field(why)
		}
		if !back.IsZero() {
			say += " until=" + back.UTC().Format(time.RFC3339)
		}
		c.says = []string{say} // a hold keeps no card, started or not (sprint.FriendTake)
		return a.runStep(name, *c, st, store.FriendTakeStep(sprint.FriendTakeReq{Friend: friend, All: true, Hold: true, Started: started, Who: c.actor}), stdout, stderr)
	}
	line, facts := token(name)+" OK "+friend+" held="+fmt.Sprint(held), map[string]any{"friend": friend, "held": held}
	if w > 0 {
		line += " width=" + strconv.Itoa(w)
		facts["width"] = w
	}
	sayOK(stdout, c.json, name, line, facts)
	return 0
}

// oneFriend is the one friend a verb names, or its refusal.
func oneFriend(verbName string, fs flagSet, args []string, stderr io.Writer) (string, int) {
	pos, err := parse(fs, args)
	if err != nil {
		return "", refuse(stderr, verbName, err.Error())
	}
	if len(pos) != 1 {
		return "", refuse(stderr, verbName, "wants one friend, a friend row of nova-config")
	}
	if !sprint.ValidID(pos[0]) {
		return "", refuse(stderr, verbName, "a friend name wants letters, digits, _ and -: "+pos[0])
	}
	return pos[0], 0
}

// orEmpty is the list, or an empty one in place of nil (JSON [] rather than null).
func orEmpty(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

// friendClass is a friend row's class: the tiers it says she can do, sorted and comma
// joined; "" when it names none.
func friendClass(r config.Row) string {
	tiers := sprint.Split(r.Fields["tiers"])
	slices.Sort(tiers)
	return strings.Join(slices.Compact(tiers), ",")
}

// friendRoles is the friend row's roles, sorted and comma joined: reader is the read
// cards' role (sprint read_cards.go; nova-config friend set <f> --roles builder,reader).
func friendRoles(r config.Row) string {
	words := sprint.Split(r.Fields["roles"])
	slices.Sort(words)
	return strings.Join(slices.Compact(words), ",")
}
