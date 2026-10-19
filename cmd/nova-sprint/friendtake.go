package main

import (
	"context"
	"fmt"
	"io"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"

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
// every one as it holds her. What she has started is read here, outside the step: her beat's
// running cards and a push on each card's branch (one git ls-remote for every sprint branch
// of each distinct repository, the tip friend sync reads); the step is a function of the
// tables and that set, and a card started between the read and the step is in the same
// window the coordinator's command outside the server has always had.

// friendTakeWords is what friend take says on -h.
const friendTakeWords = "friend take takes back and marks cards dealt to the friend: each goes back to ready, withdrawn from the friend's row (no failure, no bound spent: the card records \"taken back by the coordinator: <reason>\"), and the next tick deals the same card at its next generation to another friend up with room, never back to the friend until friend give (a friend's card is never a machine's); each card taken is said as \"<id>: back to ready, not dealt to <friend> again until friend give\" and the summary counts them (marked=<n>); a card whose WHO line pins the friend waits until it is given back to the friend (friend give), briefed for another friend or dropped. The selectors: --all-unstarted takes ONLY the cards dealt to the friend and not taken: ready in the friend's queue with no take recorded (taken, first_taken), no progress, no report and no carried head, and not started (no push on its branch, not named running by the friend's beat); it never takes a working card, and says each card the friend keeps (\"friend <f> keeps <id>: <why>\"). <id>... takes the cards named that are dealt to the friend and not taken; a named card in flight (working, or with a take, progress, report or carried head recorded) is refused unless --working is given, and with --working it is withdrawn mid-flight and one WARNING line counts the working cards withdrawn. --working is for named cards only, never with --all-unstarted. A card is refused, one REFUSED line each, when it is not dealt to that friend, when the friend has started it (a push on its branch, the friend's beat naming it running), or finished (in review or later), even with --working; the others named are taken, and the exit is 1 when any is refused. --all-or-nothing takes none when one is refused. --dry-run lists each card that would move (WOULD), each the friend keeps and each refused, and writes nothing. A working card taken frees the friend's lane, and the friend's oldest ready card is taken into working at once. friend sync marks a card taken back as taken in the friend's queue file.\n"

// friendStarted is the friend's cards she has started, each with its why: every work card
// ready or working on her row that her last beat names running (by its id, its job or its
// primary), whose branch origin holds, or whose push cannot be read (the card stays with her).
func (a *app) friendStarted(ctx context.Context, st *store.Store, friend string) (map[string]string, error) {
	return a.friendStartedFor(ctx, st, []string{friend})
}

// friendTipsHooks gives one app a repository-tips reader a test can block, count or fail.
// It is a map rather than an app field because the app's fields are in main.go, which this
// card may not change; production registers nothing and reads each repository once.
var friendTipsHooks sync.Map // map[*app]tipsFn

// setFriendTips installs f as the app's tips reader (nil restores the default).
func (a *app) setFriendTips(f tipsFn) {
	if f == nil {
		friendTipsHooks.Delete(a)
		return
	}
	friendTipsHooks.Store(a, f)
}

// copyFriendTips carries the tips hook between apps the lanes make (lanesFor, laneApp).
func copyFriendTips(dst, src *app) {
	if f, ok := friendTipsHooks.Load(src); ok {
		friendTipsHooks.Store(dst, f)
		return
	}
	friendTipsHooks.Delete(dst)
}

// friendTips is the app's repository-tips reader: a test's hook when it gave one, the
// per-card tip when a test injected only that older seam, and otherwise one bounded read
// of every sprint branch of a repository.
func (a *app) friendTips() tipsFn {
	if f, ok := friendTipsHooks.Load(a); ok {
		return f.(tipsFn)
	}
	if a.tip != nil && reflect.ValueOf(a.tip).Pointer() != reflect.ValueOf(a.branchTip).Pointer() {
		return nil
	}
	return a.branchTips
}

// friendStartedFor reads all selected friends' cards and checks each distinct repository
// once. The server's friend level gives it every up friend at once, so repository reads do
// not multiply by the number of cards.
func (a *app) friendStartedFor(ctx context.Context, st *store.Store, friends []string) (map[string]string, error) {
	out := map[string]string{}
	owner := map[string]string{}
	running := map[string][]string{}
	pronouns := map[string]toolsfriend.Pronouns{}
	var cards []*sprint.Card
	for _, friend := range friends {
		rows, err := st.ReadCells(ctx, sprint.Fleet, sprint.FriendRow(friend), sprint.Working, sprint.Ready)
		if err != nil {
			return nil, err
		}
		beat, err := st.FriendBeatOf(ctx, friend)
		if err != nil {
			return nil, err
		}
		spec, err := st.FriendSpecOf(ctx, friend)
		if err != nil {
			return nil, err
		}
		pronouns[friend], _ = toolsfriend.ParsePronouns(spec.Pronouns)
		if beat.Friend != nil {
			running[friend] = beat.Friend.Running
		}
		for _, card := range rows {
			owner[card.ID] = friend
			cards = append(cards, card)
		}
	}
	if len(cards) == 0 {
		return out, nil
	}
	packets, err := st.Packets(ctx, cards)
	if err != nil {
		return nil, err
	}
	byRepo := map[string][]sprint.Packet{}
	for _, p := range packets {
		friend := owner[p.Card]
		if slices.Contains(running[friend], p.Card) || slices.Contains(running[friend], friendJobOf(p)) || slices.Contains(running[friend], p.Primary) {
			out[p.Card] = pronouns[friend].Say("{their} beat names it running")
			continue
		}
		repo := swarm.ReadCardBase([]byte(p.Brief)).Repo
		if repo == "" {
			out[p.Card] = "the card names no REPO: line, so a push on " + p.Branch + " cannot be read"
			continue
		}
		byRepo[repo] = append(byRepo[repo], p)
	}
	if len(byRepo) == 0 {
		return out, nil
	}
	tips := a.friendTips()
	if tips == nil {
		// A test that injects only the per-card branch tip retains that seam.
		for repo, repoPackets := range byRepo {
			for _, p := range repoPackets {
				tip, err := a.tip(ctx, repo, p.Branch)
				switch {
				case err != nil:
					out[p.Card] = "a push on " + p.Branch + " cannot be read (" + oneline.Escape(err.Error()) + ")"
				case tip != "":
					out[p.Card] = "a push on its branch " + p.Branch + " at " + tip
				}
			}
		}
		return out, nil
	}
	type result struct {
		repo string
		tips map[string]string
		err  error
	}
	repos := slices.Sorted(maps.Keys(byRepo))
	results := make(chan result, len(repos))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, repo := range repos {
		repo := repo
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results <- result{repo: repo, err: ctx.Err()}
				return
			}
			byBranch, err := tips(ctx, repo)
			results <- result{repo: repo, tips: byBranch, err: err}
		}()
	}
	wg.Wait()
	close(results)
	byRepoTips := make(map[string]result, len(repos))
	for r := range results {
		byRepoTips[r.repo] = r
	}
	for repo, repoPackets := range byRepo {
		r := byRepoTips[repo]
		for _, p := range repoPackets {
			switch tip := r.tips[p.Branch]; {
			case r.err != nil:
				out[p.Card] = "a push on " + p.Branch + " cannot be read (" + oneline.Escape(r.err.Error()) + ")"
			case tip != "":
				out[p.Card] = "a push on its branch " + p.Branch + " at " + tip
			}
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
const friendGiveWords = "friend give hands or unmarks: on a ready card never taken back it hands the card to the friend named, dealt to the friend's fleet row at once (the same work card friend sync delivers), where the deal would otherwise draw the queue head; on a card taken back from the friend (friend take) it clears the mark of the friend it was taken from, so the next deal may deal it to the friend again, and a card whose WHO line pins the friend, which waits for no one else, is dealt to the friend. A ready card whose current work card is Done (a brief-replaced bounce) or absent is handed to the friend at its next attempt, from no friend, as the deal deals it. A card not ready, one whose tier or restriction the friend's row does not serve, one whose needs have not landed, one hard pinned to another friend (nova-sprint unpin releases it), and one whose work card is live on a row (friend take frees it) are refused, one REFUSED line each, as is a card taken back from another friend; a friend not up is no refusal, the card waits on the friend's row; the others named are handed, and the exit is 1 when any is refused. Each card says \"<id>: handed to <friend>\", and one unmarked says MOVED <card> may be dealt to <friend> again (<reason>).\n"

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

type friendLevelPlan struct {
	c   common
	st  *store.Store
	req sprint.FriendLevelReq
	dry bool
}

// prepareFriendLevel reads the friend rows and started set without applying a step. The
// server can do this on its read lane, then apply only FriendLevelStep under the serial line.
func (a *app) prepareFriendLevel(ctx context.Context, args []string, stdout, stderr io.Writer) (*friendLevelPlan, int) {
	const name = "friend level"
	if len(args) >= 2 && args[0] == "friend" && args[1] == "level" {
		args = args[2:] // the server hands the verb's words whole (serve.go); a direct run has stripped them
	}
	fs, c := a.verbSetup(name)
	dry := fs.Bool("dry-run", false, "say which friends are up to be levelled and write nothing")
	pos, err := parse(fs, args)
	if err != nil {
		return nil, refuse(stderr, name, err.Error())
	}
	if len(pos) > 0 {
		return nil, refuse(stderr, name, "takes no words, found "+oneline.Escape(pos[0]))
	}
	st, err := a.store(*c)
	if err != nil {
		return nil, refuse(stderr, name, err.Error())
	}
	rows, err := st.FriendRows(ctx, a.now())
	if err != nil {
		return nil, a.readFailed(name, err, stderr)
	}
	plan := &friendLevelPlan{c: *c, st: st, dry: *dry, req: sprint.FriendLevelReq{Started: map[string]string{}, Who: c.actor}}
	var up []string
	for _, f := range rows {
		plan.req.Seats = append(plan.req.Seats, sprint.FriendSeat{Name: f.Name, Pronouns: f.Pronouns, Width: f.Width, Status: f.Status, Class: f.Class, Mode: f.Mode, WorkerLive: f.WorkerLive})
		if f.Status == sprint.Up {
			up = append(up, f.Name)
		}
	}
	started, err := a.friendStartedFor(ctx, st, up)
	if err != nil {
		return nil, a.readFailed(name, err, stderr)
	}
	maps.Copy(plan.req.Started, started)
	return plan, 0
}

func (a *app) finishFriendLevel(plan *friendLevelPlan, stdout, stderr io.Writer) int {
	const name = "friend level"
	if plan.dry {
		var up []string
		for _, f := range plan.req.Seats {
			if f.Status == sprint.Up {
				up = append(up, f.Name)
			}
		}
		fmt.Fprintf(stdout, "FRIEND-LEVEL DRY-RUN up=%s; nothing was changed\n", orDashStr(strings.Join(up, ","), "-"))
		return 0
	}
	return a.runStep(name, plan.c, plan.st, store.FriendLevelStep(plan.req), stdout, stderr)
}

func (a *app) cmdFriendLevel(args []string, stdout, stderr io.Writer) int {
	plan, code := a.prepareFriendLevel(context.Background(), args, stdout, stderr)
	if plan == nil {
		return code
	}
	return a.finishFriendLevel(plan, stdout, stderr)
}
