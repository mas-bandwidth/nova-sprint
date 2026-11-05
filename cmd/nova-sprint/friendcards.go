package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/friend"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
	"github.com/mas-bandwidth/nova-tools/pkg/gitrun"
	"github.com/mas-bandwidth/nova-tools/pkg/member"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
	"github.com/mas-bandwidth/nova-tools/pkg/redisconn"
	"github.com/mas-bandwidth/nova-tools/pkg/typedrec"
)

// A friend's sprint cards (the owner, 2026-10-03: "Could we try expressing the work left
// for nova-tools-1.1.0 into cards, and doing it via the sprint, but doing parts on friends
// where we would normally do friend work."; docs/SPEC-SPRINT.md section 1, a friend's
// card). The tick deals a card whose brief says WHO: friend to a friend's fleet row
// (sprint.TickDeal). She runs her own loop: she takes it (nova-sprint take --as
// friend.<name>), works it (friend stage clones it for her), and finishes it herself
// (nova-sprint finish --as friend.<name>); friend cards shows her the cards on her row and
// the brief of each. Nothing delivers or collects her cards (Glenn, 2026-11-05: "One shot was
// a workaround for friends who can't reliably run their own loops and take ready cards").

// friendJobOf is the job a friend's sprint card is staged as (friend stage, jobs/<job>): its
// stored id, and from its second generation (a card taken back and dealt again,
// sprint.FriendTake) .g<gen> after it, so a card dealt again to the same friend is a new job
// whose brief names its own branch. A read card is a card, under the same job.
//
// A friend's read asked before read cards (the packet's ReadJob) keeps its
// legacy job name in generation one; a resumed generation gets its own .gN
// directory so a pre-STOP outbox report cannot be read as a new result.
func friendJobOf(p sprint.Packet) string {
	job := sprint.StoredID(p.Card, p.Epoch)
	if p.Kind == "read" && p.ReadJob != "" {
		job = p.ReadJob
	}
	if p.Gen > 1 {
		job += ".g" + strconv.Itoa(p.Gen)
	}
	return job
}

// friendBrief is the BRIEF.md of a friend's sprint card: its STATUS line (the card, its
// epoch and attempt, the branch to push and the report to write), the working-directory
// line of docs/FRIENDS.md, a later attempt's start and why it exists (as a child's JOB.md
// says them), then the brief as a child is handed it (the rules it names injected). A
// reworked card's is the daemon's form (friend.ReworkedBrief): its fix the first line after
// STATUS and its STOP, whichever of friend sync and the daemon writes it first.
func friendBrief(name string, p sprint.Packet) string {
	return friend.ReworkedBrief(friendServerBrief(name, p))
}

// friendServerBrief is friendBrief before it is reworked: the fix on a 'The coordinator
// asks:' line under the start, the form friend.ReworkedBrief reads.
func friendServerBrief(name string, p sprint.Packet) string {
	job := friendJobOf(p)
	var b strings.Builder
	fmt.Fprintf(&b, "STATUS: nova-sprint card %s, epoch %d, attempt %d; push your work to the branch %s; when done, finish it yourself: nova-sprint finish --as friend.%s %s@%d --epoch %d (with --failed when it failed); nova-sprint friend stage %s@%d --as friend.%s stages it and writes the exact finish command in JOB.md\n", p.Card, p.Epoch, p.Attempt, p.Branch, name, p.Card, max(p.Gen, 1), p.Epoch, p.Card, max(p.Gen, 1), name)
	fmt.Fprintf(&b, "Work in ~/%[1]s-working/jobs/%[2]s/: every clone, worktree and build output goes inside it, "+swarm.FriendGoCacheLine(name)+".\n", name, job)
	if c, ok := member.CarryOf(p.Brief); ok && p.BaseHead == "" {
		// a card brief --widen widened starts from the held attempt's head (member.Carried)
		p.BaseHead, p.BaseAttempt = c.Head, c.Attempt
	}
	if p.BaseHead != "" {
		b.WriteString(member.CarryLine(member.Carry{Card: p.Primary, Attempt: p.BaseAttempt, Head: p.BaseHead}) + "\n")
	}
	if p.Attempt > 1 || p.BaseHead != "" {
		b.WriteString(friendStart(p))
	}
	for _, l := range [][2]string{{"This attempt exists because: ", p.Why}, {"A reader found: ", p.Finding}, {"The coordinator asks: ", p.Fix}} {
		if l[1] != "" {
			b.WriteString(l[0] + l[1] + "\n")
		}
	}
	brief := p.Brief
	// A delivered carry is derived from all earlier successful attempts above. An older
	// CARRY in the stored brief must not override that newer pushed head.
	briefLines := strings.Split(brief, "\n")
	briefLines = slices.DeleteFunc(briefLines, func(line string) bool {
		return strings.HasPrefix(strings.TrimSpace(line), member.CarryKey)
	})
	brief = strings.Join(briefLines, "\n")
	if p.Rules != "" {
		if rules, err := swarm.HeldRules(p.Rules); err == nil {
			brief = swarm.StagedBrief(brief, rules)
		}
	}
	b.WriteString("\n" + strings.TrimRight(brief, "\n") + "\n")
	return b.String()
}

// friendStart is where a friend's later attempt starts, the rule a member's rework is staged
// by (docs/SPEC-CARD-CONTRACT.md, where a rework starts; nova-tools#5215): the current tip of
// the card's base branch on origin, never an older base, with the work of the last attempt that
// pushed carried onto it by the friend herself, and the Head she finishes with on that tip.
func friendStart(p sprint.Packet) string {
	base := swarm.ReadCardBase([]byte(p.Brief)).Ref
	ref := "origin/" + base
	if base == "" || typedrec.IsFullSha(base) {
		base, ref = "the repository's default branch", "origin/HEAD"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "This attempt starts from the current tip of %s on origin, never from an older base: fetch it and start your branch there.", base)
	if p.BaseHead != "" {
		fmt.Fprintf(&b, " Carry the work of attempt %d onto it yourself: its head, %s, is the last pushed by any attempt before this one (`git diff %s...%s` shows that work); where it does not apply cleanly, redo it.", p.BaseAttempt, p.BaseHead, ref, p.BaseHead)
	} else {
		b.WriteString(" No attempt before this one pushed work to carry.")
	}
	b.WriteString(" The Head you finish with must be origin's tip of your branch; the attempt is expected to start from the tip named above.\n")
	return b.String()
}

// tipFn is origin's tip of a branch of a repository (a card's REPO: line, as
// swarm.ReadCardBase resolves it), "" when origin has no such branch; an error is a tip
// that could not be read.
type tipFn func(ctx context.Context, repo, branch string) (string, error)

// friendTipBudget bounds the one ls-remote of a branch's tip.
const friendTipBudget = 10 * time.Second

// branchTip is the real tipFn: one git ls-remote of the one ref, bounded.
func (a *app) branchTip(ctx context.Context, repo, branch string) (string, error) {
	ref := "refs/heads/" + branch
	out, err := gitrun.Output(ctx, gitrun.Options{Env: a.gitEnv, Timeout: friendTipBudget}, "ls-remote", "--", repo, ref)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[1] == ref {
			return f[0], nil
		}
	}
	return "", nil
}

// busRedisEnv names the friends' bus store (nova-bus's).
const busRedisEnv = "NOVA_BUS_REDIS"

// busUserEnv and busPasswordEnvEnv are the bus's own login, apart from the sprint store's.
const (
	busUserEnv        = "NOVA_BUS_REDIS_USER"
	busPasswordEnvEnv = "NOVA_BUS_REDIS_PASSWORD_ENV"
)

// busSendFn sends one message on the friends' bus; say is handed each line the send has
// for the verb's output (a bus store's alarm raised or cleared).
type busSendFn func(ctx context.Context, m bus.Message, say func(string)) error

// busWatch is the app's Watch of the bus store addr as user, one per store and user
// for the life of the process: its alarm is raised at the first send that fails on the
// login or the connection and cleared at the next that succeeds. Each is queued as its
// one line (Alarm.Text) for the send that saw it to say (sendBus).
func (a *app) busWatch(addr, user string) *bus.Watch {
	a.busWatchesMu.Lock()
	defer a.busWatchesMu.Unlock()
	key := addr + ":" + user
	if w, ok := a.busWatches[key]; ok {
		return w
	}
	queue := func(al bus.Alarm) {
		a.busAlarmsMu.Lock()
		defer a.busAlarmsMu.Unlock()
		a.busAlarms = append(a.busAlarms, al.Text())
	}
	w := &bus.Watch{Store: addr, User: user, Raise: queue, Clear: queue}
	if a.busWatches == nil {
		a.busWatches = map[string]*bus.Watch{}
	}
	a.busWatches[key] = w
	return w
}

// busOptions selects the login for the bus connection used by friend sync
// (SPEC-SPRINT section 1). It holds variable names, never a password value.
func busOptions(getenv func(string) string) redisconn.Options {
	addr := getenv(busRedisEnv)
	user := getenv(busUserEnv)
	o := redisconn.Options{Addr: addr, Env: redisconn.Env{User: busUserEnv}}
	if user != "" || getenv(busPasswordEnvEnv) != "" {
		o.Env.PasswordEnv = busPasswordEnvEnv
	}
	return o
}

// busConnectionOptions is shared by the connection and coordinator sender
// selection, including a recorded seat login for this address.
func (a *app) busConnectionOptions(addr, user string) (redisconn.Options, func(string) string, error) {
	o := busOptions(a.getenv)
	if addr != "" {
		o.Addr = addr
	}
	if user != "" {
		o.User = user
	}
	o, getenv, err := a.seatBusOptions(o)
	if err != nil {
		return redisconn.Options{}, nil, err
	}
	return o, getenv, nil
}

func (a *app) openBus(ctx context.Context, addr, user string) (*bus.Bus, func(), error) {
	o, getenv, err := a.busConnectionOptions(addr, user)
	if err != nil {
		return nil, nil, err
	}
	conn, err := redisconn.Open(ctx, o, getenv)
	if err != nil {
		return nil, nil, err
	}
	return &bus.Bus{Store: bus.Redis{C: conn.Client()}}, func() {
		_ = conn.Close() // ignored: the connection is closed at the end of the send; a failed close has no one to tell
	}, nil
}

// sendBus is the real busSendFn: one message sent through friend.Courier on a
// connection opened for it (busOpen) and closed after, its result watched
// (busWatch). The alarm a send raises or clears is said as one FRIEND-CARD
// BUS-ALARM line, and a send that fails while the alarm is raised says so in its
// error, which friend sync writes on the card's story.
func (a *app) sendBus(ctx context.Context, m bus.Message, say func(string)) error {
	addr := a.getenv(busRedisEnv)
	if addr == "" {
		return errors.New(busRedisEnv + " is not set: no bus to send on")
	}
	user := a.getenv(busUserEnv)
	c := &friend.Courier{
		Now:   func() time.Time { return a.now() },
		Open:  func(ctx context.Context) (*bus.Bus, func(), error) { return a.busOpen(ctx, addr, user) },
		Watch: a.busWatch(addr, user),
	}
	_, err := c.Send(ctx, m)
	a.busAlarmsMu.Lock()
	said := a.busAlarms
	a.busAlarms = nil
	a.busAlarmsMu.Unlock()
	for _, text := range said {
		say("FRIEND-CARD BUS-ALARM " + oneline.Escape(text))
	}
	if err != nil && c.Watch.Open() {
		return fmt.Errorf("%w (the bus store's alarm is raised: FRIEND-CARD BUS-ALARM)", err)
	}
	return err
}

// busEnroll puts names on the bus store's roster (bus.Enroll: only the missing
// ones, returned): nothing when NOVA_BUS_REDIS names no store.
func (a *app) busEnroll(ctx context.Context, names ...string) ([]string, error) {
	addr := a.getenv(busRedisEnv)
	if addr == "" {
		return nil, nil
	}
	b, closeBus, err := a.busOpen(ctx, addr, a.getenv(busUserEnv))
	if err != nil {
		return nil, err
	}
	if closeBus != nil {
		defer closeBus()
	}
	return b.Enroll(ctx, names...)
}

// friendReadText is the BRIEF.md of a friend's read, as friend cards shows it: the read's brief, the attempt's branch, start commit and head (the packet's,
// else the card's), and a deadline of thirty minutes on the sprint clock.
func friendReadText(st *store.Store, name string, p sprint.Packet, c *sprint.Card) string {
	if p.ReadJob != "" {
		// a read asked the old way keeps the old brief
		branch, head, start := p.WorkBranch, p.Head, ""
		if c != nil {
			branch, head, start = cmp.Or(branch, c.F("branch")), cmp.Or(head, c.F("head")), c.F("start")
		}
		var deadline time.Time
		if st.Now != nil {
			deadline = st.Now().Add(sprint.FriendReadDeadline)
		}
		return sprint.FriendReadBrief(name, p.Primary, p.Brief, branch, start, head, p.Attempt, deadline)
	}
	start := ""
	var deadline time.Time
	if c != nil {
		p.WorkBranch, p.Head = cmp.Or(p.WorkBranch, c.F("branch")), cmp.Or(p.Head, c.F("head"))
		start = c.F("start")
		if c.Col == sprint.Working {
			// a read dealt to her working runs from its deal (sprint read_cards.go, readStart)
			deadline, _ = time.Parse(time.RFC3339, c.F("asked"))
			if !deadline.IsZero() {
				deadline = deadline.Add(sprint.ReadCardDeadline)
			}
		}
	}
	return sprint.ReadCardBrief(name, friendJobOf(p), p, start, deadline)
}

func (a *app) seatBusOptions(o redisconn.Options) (redisconn.Options, func(string) string, error) {
	getenv := a.getenv
	if o.User == "" && o.PasswordEnv == "" && getenv(busUserEnv) == "" && getenv(busPasswordEnvEnv) == "" {
		l, ok, err := a.recordedLogin()
		if err != nil {
			return redisconn.Options{}, nil, err
		}
		if ok && l.Redis == o.Addr {
			secret, err := a.readLoginSecret("seat environment", l)
			if err != nil {
				return redisconn.Options{}, nil, err
			}
			o.User, o.PasswordEnv = l.User, seatLoginPassword
			password := ""
			if err := secret.Use(func(p string) error { password = p; return nil }); err != nil {
				return redisconn.Options{}, nil, err
			}
			base := getenv
			getenv = func(k string) string {
				if k != seatLoginPassword {
					return base(k)
				}
				return password
			}
		}
	}
	return o, getenv, nil
}
