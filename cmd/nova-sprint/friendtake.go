package main

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	toolsfriend "github.com/mas-bandwidth/nova-tools/pkg/friend"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
)

// A friend's cards taken back (docs/SPEC-SPRINT.md section 1, a friend's card taken back;
// sprint.FriendTake): friend take <friend> <id>... takes back the cards named that she has
// not started and refuses the rest (the card friend-take-partial.w1; --all-or-nothing takes
// none when one is refused; a named card in flight wants --working), friend take
// --all-unstarted only the cards dealt and not taken (never a working one), and friend down
// every one as it holds her. What she has started is read here, outside the step: her beat's running cards and a
// push on each card's branch (one git ls-remote each, the tip friend sync reads); the step
// is a function of the tables and that set.

// friendTakeWords is what friend take says on -h.
const friendTakeWords = "friend take takes back and marks cards dealt to the friend: each goes back to ready, withdrawn from the friend's row (no failure, no bound spent: the card records \"taken back by the coordinator: <reason>\"), and the next tick deals the same card at its next generation to another friend up with room, never back to the friend until friend give (a friend's card is never a machine's); each card taken is said as \"<id>: back to ready, not dealt to <friend> again until friend give\" and the summary counts them (marked=<n>); a card whose WHO line pins the friend waits until it is given back to the friend (friend give), briefed for another friend or dropped. The selectors: --all-unstarted takes ONLY the cards dealt to the friend and not taken: ready in the friend's queue with no take recorded (taken, first_taken), no progress, no report and no carried head, and not started (no push on its branch, not named running by the friend's beat); it never takes a working card, and says each card the friend keeps (\"friend <f> keeps <id>: <why>\"). <id>... takes the cards named that are dealt to the friend and not taken; a named card in flight (working, or with a take, progress, report or carried head recorded) is refused unless --working is given, and with --working it is withdrawn mid-flight and one WARNING line counts the working cards withdrawn. --working is for named cards only, never with --all-unstarted. A card is refused, one REFUSED line each, when it is not dealt to that friend, when the friend has started it (a push on its branch, the friend's beat naming it running), or finished (in review or later), even with --working; the others named are taken, and the exit is 1 when any is refused. --all-or-nothing takes none when one is refused. --dry-run lists each card that would move (WOULD), each the friend keeps and each refused, and writes nothing. A working card taken frees the friend's lane, and the friend's oldest ready card is taken into working at once. friend sync marks a card taken back as taken in the friend's queue file.\n"

// friendStarted is the friend's cards she has started, each with its why: every work card
// ready or working on her row that her last beat names running (by its id, its job or its
// primary), or whose branch origin holds (a push on it), or whose push cannot be read (the
// card stays with her rather than be taken from under her).
func (a *app) friendStarted(ctx context.Context, st *store.Store, friend string) (map[string]string, error) {
	cards, err := st.ReadCells(ctx, sprint.Fleet, sprint.FriendRow(friend), sprint.Working, sprint.Ready)
	if err != nil || len(cards) == 0 {
		return nil, err
	}
	b, err := st.FriendBeatOf(ctx, friend)
	if err != nil {
		return nil, err
	}
	spec, err := st.FriendSpecOf(ctx, friend)
	if err != nil {
		return nil, err
	}
	pro, _ := toolsfriend.ParsePronouns(spec.Pronouns)
	var running []string
	if b.Friend != nil {
		running = b.Friend.Running
	}
	packets, err := st.Packets(ctx, cards)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, p := range packets {
		if slices.Contains(running, p.Card) || slices.Contains(running, friendJobOf(p)) || slices.Contains(running, p.Primary) {
			out[p.Card] = pro.Say("{their} beat names it running")
			continue
		}
		repo := swarm.ReadCardBase([]byte(p.Brief)).Repo
		if repo == "" {
			out[p.Card] = "the card names no REPO: line, so a push on " + p.Branch + " cannot be read"
			continue
		}
		switch tip, err := a.tip(ctx, repo, p.Branch); {
		case err != nil:
			out[p.Card] = "a push on " + p.Branch + " cannot be read (" + oneline.Escape(err.Error()) + ")"
		case tip != "":
			out[p.Card] = "a push on its branch " + p.Branch + " at " + tip
		}
	}
	return out, nil
}

func (a *app) cmdFriendTake(args []string, stdout, stderr io.Writer) int {
	const name = "friend take"
	fs, c := a.verbSetup(name)
	reason := fs.String("reason", "", "why the cards are taken back, kept on each card (\"taken back by the coordinator: <reason>\")")
	all := fs.Bool("all-unstarted", false, "take only the cards dealt to the friend and not taken (ready, no take, progress, report, carried head or push), naming no card; never a working card")
	working := fs.Bool("working", false, "with cards named: withdraw a named card the friend is working (in flight) mid-flight, with one WARNING line; never with --all-unstarted")
	dry := fs.Bool("dry-run", false, "list each card that would be taken back, kept or refused, and write nothing")
	allOrNothing := fs.Bool("all-or-nothing", false, "take none of the cards named when any one is refused")
	pos, err := parse(fs, args)
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	switch {
	case len(pos) == 0:
		return refuse(stderr, name, "wants a friend, then the cards to take back (or --all-unstarted)")
	case !sprint.ValidID(pos[0]):
		return refuse(stderr, name, "a friend name wants letters, digits, _ and -: "+pos[0])
	case *all == (len(pos) > 1):
		return refuse(stderr, name, "wants the cards to take back, or --all-unstarted, and not both")
	case *working && *all:
		return refuse(stderr, name, "--working withdraws named working cards only; --all-unstarted never takes a working card, so not both")
	}
	friend, ids := pos[0], pos[1:]
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	ctx := context.Background()
	names, err := st.FriendNames(ctx)
	if err != nil {
		return a.readFailed(name, err, stderr)
	}
	if !slices.Contains(names, friend) {
		// the store's refusal of a name the roster lacks, as friend down's: exit 1
		fmt.Fprintf(stderr, "%s %s: no friend %s on the friends table (friends: %s); run: nova-sprint friend sync\n", prog, name, friend, orDashStr(strings.Join(names, ","), "none"))
		return 1
	}
	started, err := a.friendStarted(ctx, st, friend)
	if err != nil {
		return a.readFailed(name, err, stderr)
	}
	c.dry = *dry // the plan's WOULD, NOTE and REFUSED lines, nothing written (planDry)
	step := store.FriendTakeStep(sprint.FriendTakeReq{Friend: friend, IDs: ids, All: *all, Working: *working, AllOrNothing: *allOrNothing, Reason: *reason, Started: started, Who: c.actor, Spends: true})
	step.Named = false // the takeable are taken and the rest refused; sprint.FriendTake keeps --all-or-nothing itself
	return a.runStep(name, *c, st, step, stdout, stderr)
}

// friendGiveWords is what friend give says on -h.
const friendGiveWords = "friend give hands or unmarks: on a ready card never taken back it hands the card to the friend named, dealt to the friend's fleet row at once (the same work card friend sync delivers), where the deal would otherwise draw the queue head; on a card taken back from the friend (friend take) it clears the mark of the friend it was taken from, so the next deal may deal it to the friend again, and a card whose WHO line pins the friend, which waits for no one else, is dealt to the friend. A card not ready, one whose tier the friend's row does not serve, and one whose needs have not landed are refused, one REFUSED line each, as is a card never taken back and already dealt; the others named are handed, and the exit is 1 when any is refused. Each card says \"<id>: handed to <friend>\", and one unmarked says MOVED <card> may be dealt to <friend> again (<reason>).\n"

func (a *app) cmdFriendGive(args []string, stdout, stderr io.Writer) int {
	const name = "friend give"
	fs, c := a.verbSetup(name)
	reason := fs.String("reason", "", "why the cards are given back, kept on the note (default: given back by the coordinator)")
	pos, err := parse(fs, args)
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	switch {
	case len(pos) < 2:
		return refuse(stderr, name, "wants a friend, then the cards to give back")
	case !sprint.ValidID(pos[0]):
		return refuse(stderr, name, "a friend name wants letters, digits, _ and -: "+pos[0])
	}
	friend := pos[0]
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	names, err := st.FriendNames(context.Background())
	if err != nil {
		return a.readFailed(name, err, stderr)
	}
	if !slices.Contains(names, friend) {
		fmt.Fprintf(stderr, "%s %s: no friend %s on the friends table (friends: %s); run: nova-sprint friend sync\n", prog, name, friend, orDashStr(strings.Join(names, ","), "none"))
		return 1
	}
	step := store.FriendGiveStep(sprint.FriendGiveReq{Friend: friend, IDs: pos[1:], Reason: *reason, Who: c.actor})
	step.Named = false // the givable are given and the rest refused
	return a.runStep(name, *c, st, step, stdout, stderr)
}

// friendLevelWords is what friend level says on -h.
const friendLevelWords = "friend level evens the friends' ready queues as fleet level evens the members': among the friends up of one class (the tiers the friend's nova-config row says the friend can do), while one has two more cards over the friend's width than another below the friend's room (twice the friend's width, in batch and one-shot mode alike), the newest card of the first moves to the second at its next generation, into working when the friend has a lane free. Only a card for any friend (WHO: friend) that is ready on the friend's row and that the friend has not started (a push on its branch, the friend's beat naming it running) moves; a card naming the friend and a working card stay. The MOVED line says moved=N to <friend>(n) from <friend>(n). friend sync delivers a moved card as a new job and marks it taken in the queue file of the friend it left.\n"

func (a *app) cmdFriendLevel(args []string, stdout, stderr io.Writer) int {
	const name = "friend level"
	fs, c := a.verbSetup(name)
	dry := fs.Bool("dry-run", false, "say which friends are up to be levelled and write nothing")
	pos, err := parse(fs, args)
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	if len(pos) > 0 {
		return refuse(stderr, name, "takes no words, found "+oneline.Escape(pos[0]))
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	ctx := context.Background()
	rows, err := st.FriendRows(ctx, a.now())
	if err != nil {
		return a.readFailed(name, err, stderr)
	}
	r := sprint.FriendLevelReq{Started: map[string]string{}, Who: c.actor}
	for _, f := range rows {
		r.Seats = append(r.Seats, sprint.FriendSeat{Name: f.Name, Pronouns: f.Pronouns, Width: f.Width, Status: f.Status, Class: f.Class, Mode: f.Mode})
		if f.Status != sprint.Up {
			continue
		}
		started, err := a.friendStarted(ctx, st, f.Name)
		if err != nil {
			return a.readFailed(name, err, stderr)
		}
		maps.Copy(r.Started, started)
	}
	if *dry {
		var up []string
		for _, f := range r.Seats {
			if f.Status == sprint.Up {
				up = append(up, f.Name)
			}
		}
		fmt.Fprintf(stdout, "FRIEND-LEVEL DRY-RUN up=%s; nothing was changed\n", orDashStr(strings.Join(up, ","), "-"))
		return 0
	}
	return a.runStep(name, *c, st, store.FriendLevelStep(r), stdout, stderr)
}
