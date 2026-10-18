package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/config"
	"github.com/mas-bandwidth/nova-tools/pkg/friend"
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
// and their finish verdicts), read from the fleet table by where — never from
// her inbox/outbox directories, which are only the transport of her cards
// (friendcards.go). A friend's nova-friend daemon beats with friend beat, and
// nothing else does (docs/SPEC-FRIEND.md, "The beat comes from the daemon"); the
// coordinator holds one with friend down and releases it with friend up. The
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
seat each pass, the friend sync loop row's verb, docs/FRIENDS.md; friend sync
install --every <d> runs that loop as this machine's own service, a launchd agent
or a systemd user unit, and friend sync uninstall removes it): a friend
the store lacks is added, one nova-config no longer has is taken off with the friend's
beat, and a friend that stays keeps the friend's hold. The same sync writes each friend's width,
the jobs the friend works at once: the configured row's width (nova-config friend set
<friend> --width <n>, at least 1), `+fmt.Sprint(config.DefaultFriendWidth)+` when the row names none; and the friend's delivery mode (mode:
batch|one-shot, default batch), and configured pronouns (pronouns: `+friend.PronounList()+`;
they/them when unstated). A row whose width is below 1 is refused with nothing changed. where counts the cards: ready, working,
width, done (ok and failed), ok% (ok over done, pooled in the footer) and
status, all from the friend's sprint cards — the cards dealt to the friend's fleet row
friend.<name>, their states and their finish verdicts — never from the friend's
inbox/outbox directories (those are only the transport of the friend's cards, below).
Liveness comes only from nova-hook, and the status is the server's word: each tick
writes every friend's status (down whenever the hook record is down, held only when
the hook is up and sprint holds the friend, and up otherwise) into the store, and
where and the dashboard show that word and never work it out, so a reader with no bus
address shows what the server wrote. A friend the server has written no status for
shows down. friend beat carries work reports, never liveness. friend hold <friend> --reason <text> sets the sprint hold. friend up releases
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
breaks ties. In batch and one-shot mode alike the friend's room is twice the friend's width: the friend
works at width and queues the rest, and a lane freed by a finish takes the next card
at once. nova-sprint unpin <id>... --reason <text> removes an
unstarted card's stored WHO choice without editing its brief; --stream <s>
selects a stream, and --dry-run only previews it. Work assigned to a friend
uses the friend's fleet row friend.<name>; presence never takes it back. friend take
takes back and marks what the friend has not started, and friend down takes back every card.
The seat hands a named ready card to a friend with friend give; the deal fills the rest.
friend sync writes it as <friend>-working/inbox/<job>/BRIEF.md, the job
directory <card> at epoch 0 and <card>~<epoch> after a clear (its STATUS line
names the card, the branch to push and the report), and finishes it from
outbox/<job>/REPORT.md: Verdict: LAND with Head: <full sha> goes to review at
origin's tip of that branch when the tip is that Head (one git ls-remote), and
is refused, the card left working, when the tip is another sha (both named),
when origin has no branch of that name, when the tip cannot be read, or when
the card names no REPO: line; Verdict: HOLD, FAIL, FAILED or BROKEN is work
that came back failed, but one whose first paragraph names a brief defect (the
base lacks a PATHS file, a duplicate of landed work, a decision delivered, or
the label "brief defect") is the brief's: it counts on the stream and in
neither done nor ok%, and its judgment asks to re-cut the brief, never
rework; a LAND without a full sha, an empty report and any other verdict
finish the card failed too, each with the report's first paragraph. card
prints who=; where counts it on that friend's row in the friends table.
`) + "\n"
}

// friendVerbWords is what friend beat, friend down, and friend up say on -h.
// The friends section (friendWords) stays on nova-sprint help friend. A name
// the table lacks is refused and names friend sync; friend sync exits 3 when
// the config cannot be read or holds no friend row (docs/SPEC-SPRINT.md section 1).
func friendVerbWords(name string) string {
	switch name {
	case "friend beat":
		return "Report work, live lanes, load and row configuration. This report never determines liveness; nova-hook alone does. friend sync exits 3 if config cannot be read or holds no friend row.\n"
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
// friendSession is the roster's word for a friend row's session: sprint.FriendOneShot for
// a one-shot (a member lane on her row), "" for a live friend.
func friendSession(r config.Row) string {
	if config.FriendSession(r) == config.FriendSessionOneShot {
		return sprint.FriendOneShot
	}
	return ""
}

func (a *app) readFriends(ctx context.Context, pg string) ([]config.Row, error) {
	var rows []config.Row
	err := a.withConfig(ctx, pg, func(ctx context.Context, st config.Store) error {
		var err error
		rows, err = st.List(ctx, config.KindFriend)
		return err
	})
	return rows, err
}

// reportValue is the value of a report's first line whose key, after any markdown marks
// (#, *, -, _, spaces), is one of keys in any case: the rest of the line after the colon;
// false when no line has one.
func reportValue(report string, keys ...string) (string, bool) {
	for _, line := range strings.Split(report, "\n") {
		key, rest, ok := strings.Cut(strings.TrimLeft(line, "#*-_ \t"), ":")
		if ok && slices.Contains(keys, strings.ToLower(strings.TrimSpace(key))) {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
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
	root := fs.String("root", "", "the directory the friends' working directories are under, <root>/<friend>-working (else HOME); the sync delivers and collects each friend's sprint cards there and never writes elsewhere")
	every := fs.Duration("every", 0, "sync now and again each time this passes, until interrupted, as the sprint's coordinator seat when no --actor is given, read again each pass (default: once); friend sync install --every <d> runs it as this machine's service and friend sync uninstall removes it (-h of each), or the friend sync loop row runs it (docs/FRIENDS.md)")
	pos, err := parse(fs, args)
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	if len(pos) > 0 {
		return refuse(stderr, name, "takes no words, found "+oneline.Escape(pos[0]))
	}
	if *every < 0 {
		return refuse(stderr, name, "--every wants a duration above zero, or none for one pass, got "+every.String()+"; run: nova-sprint friend sync --every 15s")
	}
	if *every > 0 {
		return a.friendSyncLoop(*c, *pg, *root, *every, stdout, stderr)
	}
	code, _ := a.friendSyncPass(*c, *pg, *root, stdout, stderr)
	return code
}

// friendSyncPass is one pass of friend sync on a store it opens as c says: its exit
// code, and whether it changed anything (a friend added, taken off or updated, a card
// delivered or finished).
func (a *app) friendSyncPass(c common, pg, root string, stdout, stderr io.Writer) (int, bool) {
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
	if root == "" {
		root = a.getenv("HOME")
	}
	if root == "" {
		return refuse(stderr, name, "wants --root <dir>, the directory the friends' working directories are under (HOME is not set)"), false
	}
	// the specs: the roster and each friend's width, nothing read of her
	// working directory (the cards, below, are delivered and collected there,
	// and where counts them from the fleet table, never from the directory)
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
		specs = append(specs, store.FriendSpec{Name: n, Width: width, Class: friendClass(r), Mode: config.FriendMode(r), ConfigDir: r.Fields["config_dir"], Pronouns: config.FriendPronouns(r), Session: friendSession(r), TokenCap: config.FriendTokenCap(r), TokenCapSet: true, Roles: friendRoles(r), Billing: r.Fields["billing"], Streams: r.Fields["streams"], Kinds: r.Fields["kinds"]})
	}
	added, removed, updated, err := st.SyncFriends(ctx, specs)
	if err != nil {
		return a.readFailed(name, err, stderr), false
	}
	// the friends' sprint cards: each one dealt to her delivered into her inbox, each one
	// she reported on finished from her outbox (friendcards.go)
	delivered, finished := 0, 0
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
		if s.Session == sprint.FriendOneShot {
			continue // a one-shot is no bus name: a send to her is refused, never queued (her work travels in the card brief)
		}
		names = append(names, s.Name)
	}
	enrolled, err := a.busEnroll(ctx, names...)
	if len(enrolled) > 0 {
		say("FRIEND-SYNC BUS-NAMES added=" + strings.Join(enrolled, ",") + ": the bus store now knows the friend rows")
	}
	if err != nil {
		say("FRIEND-SYNC NOTE: the friend rows' names could not all be put on the bus store's roster (" + oneline.Escape(err.Error()) + ")")
	}
	for _, s := range specs {
		if s.Session == sprint.FriendOneShot {
			continue // no inbox, queue file, wake or start receipt is a one-shot's: her member reads the brief from the server
		}
		// her starts first, so a card she began is working before its report is read
		if err := a.friendStartsOf(ctx, st, s.Name, filepath.Join(root, s.Name+"-working"), say); err != nil {
			fmt.Fprint(stderr, friend.Pronouns(s.Pronouns).Sprintf("%s %s: the sprint cards of %s cannot be read for {their} starts: %s; the friends table is synced; run: nova-sprint friend sync\n", prog, name, s.Name, oneline.Escape(err.Error())))
			return 1, false
		}
		d, f, err := a.friendCardsOf(ctx, st, s.Name, filepath.Join(root, s.Name+"-working"), say)
		delivered, finished = delivered+d, finished+f
		if err != nil {
			fmt.Fprintf(stderr, "%s %s: the sprint cards of %s cannot be delivered or collected: %s; the friends table is synced; run: nova-sprint friend sync\n", prog, name, s.Name, oneline.Escape(err.Error()))
			return 1, false
		}
	}
	line := fmt.Sprintf("FRIEND-SYNC OK added=%s removed=%s updated=%s friends=%d", orDashStr(strings.Join(added, ","), "-"), orDashStr(strings.Join(removed, ","), "-"), orDashStr(strings.Join(updated, ","), "-"), len(rows))
	if delivered+finished > 0 {
		line += fmt.Sprintf(" delivered=%d finished=%d", delivered, finished)
	}
	if len(added)+len(removed)+len(updated)+delivered+finished == 0 {
		line += ": nothing to do, the friends table already matches the config"
	}
	sayOK(stdout, c.json, name, line, map[string]any{"added": orEmpty(added), "removed": orEmpty(removed), "updated": orEmpty(updated), "friends": len(rows),
		"delivered": delivered, "finished": finished, "cards": orEmpty(said)})
	return 0, len(added)+len(removed)+len(updated)+delivered+finished+len(enrolled) > 0
}

// friendJobWalk bounds the walk of one job's directory for her first write: a checkout of
// this size is read whole, and a larger one up to it.
var friendJobWalk = friend.ActivityLimits{Files: 20000, Time: 250 * time.Millisecond}

// friendJobBegun is why friend sync reads her job as begun, "" while it is not: her report
// in outbox/<job>/REPORT.md, or a write under jobs/<job> after its staging (its JOB.md, which
// her daemon writes after the clone: friend.Stage). A job not staged is not begun.
func friendJobBegun(dir, job string, now func() time.Time) string {
	if fi, err := os.Lstat(filepath.Join(dir, "outbox", job, "REPORT.md")); err == nil && fi.Mode().IsRegular() {
		return "the friend's report is in outbox/" + job
	}
	jobDir := filepath.Join(dir, friend.JobsDir, job)
	staged, err := os.Lstat(filepath.Join(jobDir, friend.JobFile))
	if err != nil || !staged.Mode().IsRegular() {
		return ""
	}
	if friend.NewestWrite(os.DirFS(jobDir), []string{"."}, now, friendJobWalk).After(staged.ModTime()) {
		return "a write under " + friend.JobsDir + "/" + job + " after its staging"
	}
	return ""
}

// friendStartsOf is friend sync's start receipt for one friend's cards (docs/SPEC-SPRINT.md
// section 1, a friend's card is working once she starts it): each card on her row, ready, or
// working with no start of hers (her finish's next or a take-back's next moved it there),
// whose job she has begun (friendJobBegun) is started (sprint.FriendStart, by id at its
// generation): working, its deadline from now, one line each. A start refused is said on a
// line and read again next sync. It runs before her cards are collected, so a report on a
// card she began and the deal held ready is read on a working card.
func (a *app) friendStartsOf(ctx context.Context, st *store.Store, name, dir string, say func(string)) error {
	row := sprint.FriendRow(name)
	cards, err := st.ReadCells(ctx, sprint.Fleet, row, sprint.Ready, sprint.Working)
	if err != nil || len(cards) == 0 {
		return err
	}
	cards = slices.DeleteFunc(cards, func(c *sprint.Card) bool {
		return c.F("kind") != "work" || c.Col == sprint.Working && c.F(sprint.FieldStarted) == strconv.Itoa(max(c.Int("gen"), 1))
	})
	if len(cards) == 0 {
		return nil
	}
	packets, err := st.Packets(ctx, cards)
	if err != nil {
		return err
	}
	for i, p := range packets {
		if p.Kind == "read" || !sprint.ValidCardID(p.Card) {
			continue
		}
		job := friendJobOf(p)
		why := friendJobBegun(dir, job, a.now)
		if why == "" {
			continue
		}
		c := cards[i]
		r := sprint.FriendStartReq{Friend: name, IDs: []string{c.ID}, Gens: map[string]int{c.ID: max(c.Int("gen"), 1)}, Why: map[string]string{c.ID: why}}
		step := store.FriendStartStep(r)
		step.Actor, step.Epoch = st.Actor, &p.Epoch
		res, err := st.Run(ctx, step)
		if err != nil {
			return err
		}
		if len(res.Refused) > 0 {
			say(fmt.Sprintf("FRIEND-CARD NOTE friend=%s card=%s job=%s: begun (%s), and not started: %s; the next sync reads it again", name, c.ID, oneline.Field(job), why, oneline.Escape(res.Refused[0].Why)))
			continue
		}
		say(fmt.Sprintf("FRIEND-CARD STARTED friend=%s card=%s job=%s: %s", name, c.ID, oneline.Field(job), why))
	}
	return nil
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
	build := fs.String("build", "", "the build the friend's daemon runs (its version line's build): a friend come up is told to update when it is not the server's")
	started := fs.String("started", "", "when the friend's daemon started, RFC3339: its generation; a start not seen before raises a status judgment")
	present := fs.String("present", "", "when the friend's daemon sent the friend the present on its start, RFC3339: the snap-to-present step of a friend come up")
	running := fs.String("running", "", "the cards the friend is running now, comma separated (work card ids, the friend's job names or primaries): friend take and friend down leave them with the friend")
	live := fs.String("live", "", "the friend's live set: <card>@<gen> for each lane child the friend runs and <card>@<gen>:held for each finished report the friend's outbox holds, comma separated, or "+sprint.LiveNone+" for none; the evidence for working: a card working on the friend's row that the friend's live set has not named for "+sprint.LiveGrace.String()+" goes back ready, RUNNING or STOPPED; a beat without it leaves the friend's row unreconciled")
	working := fs.String("working", "", "how many jobs the friend is working now, as the friend's daemon counts them")
	stopReturns := fs.String("stop-returns", "", "how many stop-returns the friend's lanes still owe after the machine's stop (section 14): start waits for zero")
	queue := fs.String("queue", "", "how many jobs the friend holds queued, as the friend's daemon counts them")
	width := fs.String("width", "", "the friend's width as the friend's daemon has it (the deal's is the roster's: friend up --width)")
	load := fs.String("load", "", "the friend's load as a percent, as fleet beat --load gives a machine's")
	active := fs.String("active", "", "the newest write under the friend's working directory and outbox, as the friend's daemon found it, RFC3339: the friend's session's last activity")
	until := fs.String("until", "", "the friend's daemon's word that the friend is down until then, RFC3339: the friend's harness at its usage limit or out of credits")
	reason := fs.String("reason", "", "why the friend is down until --until, as the friend's daemon read it")
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
	var liveSet []string
	if *live != "" { // the empty set is "-" (sprint.LiveNone)
		v, err := sprint.ParseLive(*live)
		if err != nil {
			return refuse(stderr, name, err.Error())
		}
		liveSet = v
	}
	b, err := st.FriendBeatLive(ctx, friend, rep, given, liveSet)
	if err != nil {
		// a friend the roster lacks is a definite pre-write refusal: the store
		// answered and nothing was written, so it is refused/no; any other error
		// is a store that did not confirm the beat, so it is unknown
		if store.IsNoFriend(err) || store.IsOneShot(err) {
			a.outcome, a.changed = "refused", "no"
		} else {
			a.outcome, a.changed = "unknown", "unknown"
		}
		fmt.Fprintf(stderr, "%s %s: %s\n", prog, name, oneline.Escape(err.Error()))
		return 1
	}
	line, facts := "FRIEND-BEAT OK "+friend+" at="+b.At.Format(time.RFC3339), map[string]any{"friend": friend, "at": b.At}
	// her row as friend sync last wrote it, so her daemon reads her mode and width from its beat
	if spec, err := st.FriendSpecOf(ctx, friend); err == nil {
		mode := spec.Mode
		if mode == "" {
			mode = config.DefaultFriendMode
		}
		line += fmt.Sprintf(" row_mode=%s row_width=%d", mode, spec.Width)
		facts["row_mode"], facts["row_width"] = mode, spec.Width
		pronouns := spec.Pronouns
		if pronouns == "" {
			pronouns = config.DefaultFriendPronouns
		}
		line += " row_pronouns=" + pronouns
		facts["row_pronouns"] = pronouns
		if spec.ConfigDir != "" {
			line += " row_config_dir=" + spec.ConfigDir
			facts["row_config_dir"] = spec.ConfigDir
		}
		// always, including 0: a missing word is the default cap, and 0 is none
		capN := spec.TokenCap
		if !spec.TokenCapSet {
			capN = config.DefaultFriendTokenCap
		}
		line += fmt.Sprintf(" row_token_cap=%d", capN)
		facts["row_token_cap"] = capN
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
	// the machine's word, so her daemon cancels its lanes on STOPPED and starts nothing
	// (docs/SPEC-SPRINT.md section 14, stop cancels jobs; nova-tools pkg/friend stop.go)
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
