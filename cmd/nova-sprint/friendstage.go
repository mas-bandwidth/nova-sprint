package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-sprint/pkg/friend"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	"github.com/mas-bandwidth/nova-tools/pkg/member"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
)

// friendOp is one step friend stage runs on a host: a git command in a working
// directory (Git), a directory to create (Mkdir), the JOB.md write (Job), or
// reading the staged JOB.md back (Read). Host "" is the local machine.
type friendOp struct {
	Host  string
	Dir   string
	Git   []string
	Mkdir string
	Job   string
	Read  bool
}

// friendStageRun is the runner friend stage runs every git step, the directory
// it clones into, the JOB.md write and the idempotent re-run read through: a
// function (ctx, op). A test replaces it with a fake that records the host and
// each git argv, so no test opens a socket.
var friendStageRun = func(ctx context.Context, op friendOp) (string, error) {
	if op.Host == "" {
		return friendRunLocal(ctx, op)
	}
	return friendRunSSH(ctx, op)
}

// friendRunLocal runs one step on the machine nova-sprint itself runs on.
func friendRunLocal(ctx context.Context, op friendOp) (string, error) {
	switch {
	case len(op.Git) > 0:
		cmd := exec.CommandContext(ctx, "git", op.Git...)
		cmd.Dir = op.Dir
		out, err := cmd.CombinedOutput()
		return string(out), err
	case op.Read:
		b, err := os.ReadFile(filepath.Join(op.Dir, friend.JobFile))
		return string(b), err
	case op.Job != "":
		if err := os.MkdirAll(op.Dir, 0o755); err != nil {
			return "", err
		}
		return "", os.WriteFile(filepath.Join(op.Dir, friend.JobFile), []byte(op.Job), 0o644)
	default:
		return "", os.MkdirAll(op.Mkdir, 0o755)
	}
}

// friendSSHArgs is the ssh argv for one step: never a prompt (BatchMode=yes) and
// never an unanswered connect (ConnectTimeout=10), both before the host.
func friendSSHArgs(host, script string) []string {
	return []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", host, script}
}

// friendRunSSH runs one step on a bench over ssh: the step's shell command on
// the host, the JOB.md write's content on stdin.
func friendRunSSH(ctx context.Context, op friendOp) (string, error) {
	var cmd *exec.Cmd
	switch {
	case len(op.Git) > 0:
		d := friendPath(op.Dir)
		cmd = exec.CommandContext(ctx, "ssh", friendSSHArgs(op.Host, fmt.Sprintf("cd %s && git %s", d, strings.Join(quoteArgs(op.Git), " ")))...)
	case op.Read:
		p := friendPath(filepath.Join(op.Dir, friend.JobFile))
		cmd = exec.CommandContext(ctx, "ssh", friendSSHArgs(op.Host, fmt.Sprintf("test ! -e %s && exit 44 || cat %s", p, p))...)
	case op.Job != "":
		d := friendPath(op.Dir)
		cmd = exec.CommandContext(ctx, "ssh", friendSSHArgs(op.Host, fmt.Sprintf("mkdir -p %s && cat > %s/%s", d, d, friend.JobFile))...)
		cmd.Stdin = strings.NewReader(op.Job)
	default:
		cmd = exec.CommandContext(ctx, "ssh", friendSSHArgs(op.Host, "mkdir -p "+friendPath(op.Mkdir))...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil && op.Read {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 44 {
			return string(out), &os.PathError{Path: filepath.Join(op.Dir, friend.JobFile), Op: "open", Err: os.ErrNotExist}
		}
	}
	return string(out), err
}

// shQuote quotes a word for the remote shell. quoteArgs quotes each Git argv.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// friendPath quotes a path for the remote shell, letting a leading $HOME stand
// for the bench's own home: a default jobs directory is ~/<friend>-jobs on the
// host, which this machine cannot name.
func friendPath(p string) string {
	if p == "$HOME" {
		return `"$HOME"`
	}
	if rest, ok := strings.CutPrefix(p, "$HOME/"); ok {
		return `"$HOME/"` + shQuote(rest)
	}
	return shQuote(p)
}

func quoteArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = friendPath(a)
	}
	return out
}

// friendStageBound is how long one friend stage may run: one clone and a few
// small steps, then the stage gives up rather than hang on a bench.
const friendStageBound = 10 * time.Minute

// friendStageWords is what friend stage says on -h.
var friendStageWords = "friend stage <card>@<gen> --as friend.<name> [--bench <host>] [--dir <jobs dir>] stages a friend's dealt card in one step: it clones the card's REPO into <jobs dir>/<card>.w<N> at the card's NEXT head when one is carried, else at BASE, creates the delivered branch (the branch the bus message FRIEND-CARD DELIVERED carries), sets the friend's git identity in the clone's local config (the friend's nova-config row git_identity= field when it names one, else the sprint row's git_identity; when nova-config cannot be read, the sprint row's is used instead of refusing; the OK line says which, identity=friend or identity=sprint), and writes JOB.md naming the exact finish command. With --bench <host> every git step and the JOB.md write run on that host over ssh (the jobs directory default on a bench is ~/<friend>-jobs; --dir overrides; the host must be a fleet member, and JOB.md names it BENCH). Without --bench it runs on this machine and JOB.md says BENCH: local. It gives up after " + friendStageBound.String() + " on a bench that does not answer, refusing an expiry naming the bound and the host. It refuses a card not dealt to the named friend, and refuses when neither identity parses as Name <email> (it never falls back to the machine's global git config). A second run on an already staged directory refuses unless it is the same card@gen, branch, head and bench, and then prints the JOB.md path and exits 0.\n"

// splitFriendCardGen parses --as friend.<name> and a <card>@<gen> operand.
// splitFriendCardGen returns the friend name, the card id, the generation, and
// an error describing the first malformed word.
func splitFriendCardGen(as, arg string) (friendName, card string, gen int, err error) {
	name := strings.TrimSpace(as)
	if !strings.HasPrefix(name, "friend.") {
		return "", "", 0, fmt.Errorf("--as wants friend.<name>, found %s", oneline.Escape(as))
	}
	name = strings.TrimPrefix(name, "friend.")
	if !sprint.ValidID(name) {
		return "", "", 0, fmt.Errorf("a friend name wants letters, digits, _ and -: %s", oneline.Escape(name))
	}
	card, g, ok := strings.Cut(strings.TrimSpace(arg), "@")
	if !ok || card == "" {
		return "", "", 0, fmt.Errorf("wants a card as <card>@<gen>, found %s", oneline.Escape(arg))
	}
	gen, err = strconv.Atoi(g)
	if err != nil || gen < 1 {
		return "", "", 0, fmt.Errorf("the generation of %s wants a whole number of at least 1", oneline.Escape(arg))
	}
	return name, card, gen, nil
}

// cmdFriendStage is the verb `nova-sprint friend stage <card>@<gen> --as
// friend.<name> [--dir <jobs dir>]` (docs/FRIENDS.md): it stages a friend's
// dealt card in one step, so a friend's worker never stages by hand and never
// commits under the machine's global identity.
func (a *app) cmdFriendStage(args []string, stdout, stderr io.Writer) int {
	const name = "friend stage"
	fs, c := a.verbSetup(name)
	as := fs.String("as", "", "the friend to stage as, friend.<name>")
	bench := fs.String("bench", "", "stage on this fleet member over ssh (else this machine); the jobs directory default on a bench is ~/<friend>-jobs")
	dir := fs.String("dir", "", "the jobs directory the staged job is cloned under (else HOME/<friend>-working/jobs, or ~/<friend>-jobs on a bench)")
	pg := fs.String("pg", "", "the config store, Postgres postgres://user@host:port/db with no password (else NOVA_PG_DSN), as nova-config takes it")
	pos, err := parse(fs, args)
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	if len(pos) != 1 {
		return refuse(stderr, name, "wants one card: friend stage <card>@<gen> --as friend.<name>")
	}
	friendName, card, gen, err := splitFriendCardGen(*as, pos[0])
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	c.orActor(friendName) // a worker's verb is its worker's: no --actor is needed
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), friendStageBound)
	defer cancel()

	host := strings.TrimSpace(*bench)
	if host != "" {
		snap, err := st.Load(ctx, []string{sprint.Fleet}, nil)
		if err != nil {
			return a.readFailed(name, err, stderr)
		}
		if !slices.Contains(snap.Members(), host) {
			return refuseState(stderr, name, fmt.Sprintf("%s is no fleet member; stage on a member of the sprint, or with no --bench to stage on this machine", oneline.Field(host)))
		}
	}

	// (1) the card must be on friend.<name>'s row at that generation, ready or working
	cards, err := st.ReadCells(ctx, sprint.Fleet, sprint.FriendRow(friendName), sprint.Ready, sprint.Working)
	if err != nil {
		return a.readFailed(name, err, stderr)
	}
	var work *sprint.Card
	for _, cc := range cards {
		if cc.ID == card && cc.Int("gen") == gen {
			work = cc
			break
		}
	}
	if work == nil {
		return refuseState(stderr, name, fmt.Sprintf("card %s at gen %d is not dealt to friend %s (ready or working)", oneline.Field(card), gen, friendName))
	}
	pk, err := st.Packets(ctx, []*sprint.Card{work})
	if err != nil {
		return a.readFailed(name, err, stderr)
	}
	p := pk[0]

	// (3) the friend's own git identity, from her nova-config row when it names one, else the
	// sprint row's git_identity (v1.4.5: the friend kind has no git_identity field yet, so a
	// row could never name one and every friend delivery was refused); never the machine's
	// global git config
	git := ""
	if rows, ferr := a.friends(ctx, *pg); ferr == nil {
		for _, r := range rows {
			if r.Name == friendName {
				git = r.Fields["git_identity"]
				break
			}
		}
	}
	identityFrom := "friend"
	id, ok := config.ParseGitIdentity(git)
	if !ok && strings.TrimSpace(git) != "" {
		return refuseState(stderr, name, fmt.Sprintf("friend %s's nova-config row names git_identity %s, which is not 'Name <email>'; fix the row before staging", friendName, strconv.Quote(git)))
	}
	if !ok {
		sprintID, err := st.GitIdentity(ctx)
		if err != nil {
			return a.readFailed(name, err, stderr)
		}
		if id, ok = config.ParseGitIdentity(sprintID); !ok {
			return refuseState(stderr, name, fmt.Sprintf("the nova-config row of friend %s names no git_identity and the sprint row's git_identity %s is not 'Name <email>', so staging would fall back to the machine's global git config; %s", friendName, strconv.Quote(sprintID), identityRemedy))
		}
		identityFrom = "sprint"
	}

	// (2) the delivered branch is the one the bus message FRIEND-CARD DELIVERED
	// carries (friendcards.go): reuse p.Branch, never compute a second one
	base := swarm.ReadCardBase([]byte(p.Brief))
	repoURL := swarm.CardRepoURL(base.Repo)
	if repoURL == "" {
		return refuse(stderr, name, fmt.Sprintf("card %s names no REPO: line to clone", oneline.Field(card)))
	}
	baseField := base.Sha
	if baseField == "" {
		baseField = base.Ref
	}
	// the NEXT head: the packet's, else the CARRY line a widened card's brief
	// holds (the same rule friendServerBrief reads it by)
	next := p.BaseHead
	if next == "" {
		if c, ok := member.CarryOf(p.Brief); ok {
			next = c.Head
		}
	}
	startRef := ""
	if next != "" {
		startRef = next
	} else if base.Sha != "" {
		startRef = base.Sha
	} else {
		startRef = base.Ref
	}
	job := friendJobOf(p)
	root := *dir
	if root == "" {
		if host != "" {
			// the default on a bench is ~/<friend>-jobs on that host; $HOME is
			// expanded by the remote shell, never by this machine
			root = filepath.Join("$HOME", friendName+"-jobs")
		} else {
			home := a.getenv("HOME")
			if home == "" {
				return refuse(stderr, name, "wants --dir <jobs dir>, the jobs directory (HOME is not set)")
			}
			root = filepath.Join(home, friendName+"-working", friend.JobsDir)
		}
	}
	jobDir := filepath.Join(root, job)

	benchLine := "local"
	if host != "" {
		benchLine = host
	}

	// (5) an already staged directory. A v1.6 JOB.md has no BENCH line: it
	// staged only on the friend's own machine, so it matches as a local stage
	// and a local re-run accepts it; a bench re-run refuses it.
	if s, err := friendStageRun(ctx, friendOp{Host: host, Dir: jobDir, Read: true}); err == nil {
		benchesMatch := strings.Contains(s, "BENCH: "+benchLine+"\n")
		if !strings.Contains(s, "BENCH: ") && benchLine == "local" {
			benchesMatch = true
		}
		if strings.Contains(s, "CARD: "+card+"\n") && strings.Contains(s, "GEN: "+strconv.Itoa(gen)+"\n") &&
			strings.Contains(s, "BRANCH: "+p.Branch+"\n") && strings.Contains(s, "HEAD: "+startRef+"\n") &&
			benchesMatch {
			fmt.Fprintln(stdout, filepath.Join(jobDir, friend.JobFile))
			a.outcome, a.changed = "accepted", "no"
			return 0
		}
		return refuseState(stderr, name, fmt.Sprintf("the job %s is already staged as another card, branch, head or bench; nothing was written", oneline.Field(job)))
	} else if !os.IsNotExist(err) {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return friendStageTimedOut(stderr, name, host)
		}
		return refuse(stderr, name, "the staged JOB.md cannot be read: "+oneline.Err(err))
	}

	// the clone, then the identity in the clone's local config before any other
	// git invocation can see a global identity
	if _, err := friendStageRun(ctx, friendOp{Host: host, Mkdir: filepath.Dir(jobDir)}); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return friendStageTimedOut(stderr, name, host)
		}
		return refuse(stderr, name, err.Error())
	}
	if _, err := friendStageRun(ctx, friendOp{Host: host, Dir: filepath.Dir(jobDir), Git: []string{"clone", "-q", repoURL, jobDir}}); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return friendStageTimedOut(stderr, name, host)
		}
		return refuse(stderr, name, "clone of "+repoURL+" into "+oneline.Field(job)+": "+oneline.Err(err))
	}
	if _, err := friendStageRun(ctx, friendOp{Host: host, Dir: jobDir, Git: []string{"config", "user.name", id.Name}}); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return friendStageTimedOut(stderr, name, host)
		}
		return refuse(stderr, name, "setting the clone's local user.name: "+oneline.Err(err))
	}
	if _, err := friendStageRun(ctx, friendOp{Host: host, Dir: jobDir, Git: []string{"config", "user.email", id.Email}}); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return friendStageTimedOut(stderr, name, host)
		}
		return refuse(stderr, name, "setting the clone's local user.email: "+oneline.Err(err))
	}
	if next != "" {
		if _, err := friendStageRun(ctx, friendOp{Host: host, Dir: jobDir, Git: []string{"fetch", "-q", "origin", next}}); err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return friendStageTimedOut(stderr, name, host)
			}
			return refuse(stderr, name, "fetch of the NEXT head "+oneline.Escape(next)+": "+oneline.Err(err))
		}
	}
	if _, err := friendStageRun(ctx, friendOp{Host: host, Dir: jobDir, Git: []string{"checkout", "-q", "-B", p.Branch, startRef}}); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return friendStageTimedOut(stderr, name, host)
		}
		return refuse(stderr, name, "checkout of "+oneline.Field(p.Branch)+" at "+oneline.Escape(startRef)+": "+oneline.Err(err))
	}

	// (4) JOB.md: the card id and gen, the branch, the base, the head it
	// started from, the bench it staged on, and the exact finish command (and
	// its --failed form)
	finish := fmt.Sprintf("nova-sprint finish --as friend.%s %s@%d --epoch %d --head %s --branch %s --base %s", friendName, card, gen, p.Epoch, startRef, p.Branch, baseField)
	jobmd := "CARD: " + card + "\n" +
		"GEN: " + strconv.Itoa(gen) + "\n" +
		"BRANCH: " + p.Branch + "\n" +
		"BASE: " + baseField + "\n" +
		"HEAD: " + startRef + "\n" +
		"BENCH: " + benchLine + "\n" +
		"FINISH: " + finish + "\n" +
		"FINISH-FAILED: " + finish + " --failed\n"
	if _, err := friendStageRun(ctx, friendOp{Host: host, Dir: jobDir, Job: jobmd}); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return friendStageTimedOut(stderr, name, host)
		}
		return refuse(stderr, name, "writing JOB.md: "+oneline.Err(err))
	}
	fmt.Fprintf(stdout, "FRIEND-STAGE OK friend=%s card=%s gen=%d branch=%s job=%s jobmd=%s bench=%s identity=%s\n", friendName, card, gen, p.Branch, oneline.Field(job), filepath.Join(jobDir, friend.JobFile), benchLine, identityFrom)
	a.outcome, a.changed = "accepted", "yes"
	return 0
}

// friendStageTimedOut refuses a stage that ran past friendStageBound: it names
// the bound and the host it gave up on (local without --bench).
func friendStageTimedOut(stderr io.Writer, verb, host string) int {
	if host == "" {
		host = "local"
	}
	return refuseState(stderr, verb, fmt.Sprintf("friend stage gave up after %s on %s: the stage did not finish", friendStageBound, oneline.Field(host)))
}

// refuseState is refuse for a refusal of the world's state, not of the words
// typed (a card not on her row, a friend with no identity on record): exit 1,
// "failed or incomplete (including refused)", nothing written. Exit 2 stays
// usage, so the verb's help example, which names a card no test sprint holds,
// is not a usage refusal.
func refuseState(stderr io.Writer, verb, what string) int {
	refuse(stderr, verb, what)
	return 1
}
