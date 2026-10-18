package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/config"
	"github.com/mas-bandwidth/nova-tools/pkg/friend"
	"github.com/mas-bandwidth/nova-tools/pkg/member"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
	"github.com/mas-bandwidth/nova-tools/pkg/swarm"
)

// friendStageGit is the git runner friend stage clones and configures with: a
// function (workdir, argv) over the real git. A test replaces it with a fake
// that records every argv and every config write, so the test can assert the
// clone's local user.name and user.email are the friend's and that no git
// invocation ran without them set.
var friendStageGit = func(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// friendStageWords is what friend stage says on -h.
const friendStageWords = "friend stage <card>@<gen> --as friend.<name> [--dir <jobs dir>] stages a friend's dealt card in one step: it clones the card's REPO into <jobs dir>/<card>.w<N> at the card's NEXT head when one is carried, else at BASE, creates the delivered branch (the branch the bus message FRIEND-CARD DELIVERED carries), sets the friend's git identity in the clone's local config (the friend's nova-config row git_identity= field when it names one, else the sprint row's git_identity; the OK line says which, identity=friend or identity=sprint), and writes JOB.md naming the exact finish command. It refuses a card not dealt to the named friend, and refuses when neither identity parses as Name <email> (it never falls back to the machine's global git config). A second run on an already staged directory refuses unless it is the same card@gen, branch and head, and then prints the JOB.md path and exits 0.\n"

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
	dir := fs.String("dir", "", "the jobs directory the staged job is cloned under (else HOME/<friend>-working/jobs)")
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
	ctx := context.Background()

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
	rows, err := a.friends(ctx, *pg)
	if err != nil {
		return refuse(stderr, name, "the config cannot be read: "+oneline.Err(err)+"; run: nova-config friend list")
	}
	git := ""
	for _, r := range rows {
		if r.Name == friendName {
			git = r.Fields["git_identity"]
			break
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
		home := a.getenv("HOME")
		if home == "" {
			return refuse(stderr, name, "wants --dir <jobs dir>, the jobs directory (HOME is not set)")
		}
		root = filepath.Join(home, friendName+"-working", friend.JobsDir)
	}
	jobDir := filepath.Join(root, job)

	// (5) an already staged directory
	if _, err := os.Lstat(filepath.Join(jobDir, friend.JobFile)); err == nil {
		old, err := os.ReadFile(filepath.Join(jobDir, friend.JobFile))
		if err != nil {
			return refuse(stderr, name, "the staged JOB.md cannot be read: "+oneline.Err(err))
		}
		s := string(old)
		if strings.Contains(s, "CARD: "+card+"\n") && strings.Contains(s, "GEN: "+strconv.Itoa(gen)+"\n") &&
			strings.Contains(s, "BRANCH: "+p.Branch+"\n") && strings.Contains(s, "HEAD: "+startRef+"\n") {
			fmt.Fprintln(stdout, filepath.Join(jobDir, friend.JobFile))
			a.outcome, a.changed = "accepted", "no"
			return 0
		}
		return refuseState(stderr, name, fmt.Sprintf("the job %s is already staged as another card, branch or head; nothing was written", oneline.Field(job)))
	} else if !os.IsNotExist(err) {
		return refuse(stderr, name, err.Error())
	}

	// the clone, then the identity in the clone's local config before any other
	// git invocation can see a global identity
	if err := os.MkdirAll(filepath.Dir(jobDir), 0o755); err != nil {
		return refuse(stderr, name, err.Error())
	}
	if _, err := friendStageGit(ctx, filepath.Dir(jobDir), "clone", "-q", repoURL, jobDir); err != nil {
		return refuse(stderr, name, "clone of "+repoURL+" into "+oneline.Field(job)+": "+oneline.Err(err))
	}
	if _, err := friendStageGit(ctx, jobDir, "config", "user.name", id.Name); err != nil {
		return refuse(stderr, name, "setting the clone's local user.name: "+oneline.Err(err))
	}
	if _, err := friendStageGit(ctx, jobDir, "config", "user.email", id.Email); err != nil {
		return refuse(stderr, name, "setting the clone's local user.email: "+oneline.Err(err))
	}
	if next != "" {
		if _, err := friendStageGit(ctx, jobDir, "fetch", "-q", "origin", next); err != nil {
			return refuse(stderr, name, "fetch of the NEXT head "+oneline.Escape(next)+": "+oneline.Err(err))
		}
	}
	if _, err := friendStageGit(ctx, jobDir, "checkout", "-q", "-B", p.Branch, startRef); err != nil {
		return refuse(stderr, name, "checkout of "+oneline.Field(p.Branch)+" at "+oneline.Escape(startRef)+": "+oneline.Err(err))
	}

	// (4) JOB.md: the card id and gen, the branch, the base, the head it
	// started from, and the exact finish command (and its --failed form)
	finish := fmt.Sprintf("nova-sprint finish --as friend.%s %s@%d --epoch %d --head %s --branch %s --base %s", friendName, card, gen, p.Epoch, startRef, p.Branch, baseField)
	jobmd := "CARD: " + card + "\n" +
		"GEN: " + strconv.Itoa(gen) + "\n" +
		"BRANCH: " + p.Branch + "\n" +
		"BASE: " + baseField + "\n" +
		"HEAD: " + startRef + "\n" +
		"FINISH: " + finish + "\n" +
		"FINISH-FAILED: " + finish + " --failed\n"
	if err := os.WriteFile(filepath.Join(jobDir, friend.JobFile), []byte(jobmd), 0o644); err != nil {
		return refuse(stderr, name, "writing JOB.md: "+oneline.Err(err))
	}
	fmt.Fprintf(stdout, "FRIEND-STAGE OK friend=%s card=%s gen=%d branch=%s job=%s jobmd=%s identity=%s\n", friendName, card, gen, p.Branch, oneline.Field(job), filepath.Join(jobDir, friend.JobFile), identityFrom)
	a.outcome, a.changed = "accepted", "yes"
	return 0
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
