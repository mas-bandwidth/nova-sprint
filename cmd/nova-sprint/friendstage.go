package main

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-sprint/pkg/friend"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	"github.com/mas-bandwidth/nova-tools/pkg/member"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
	"github.com/mas-bandwidth/nova-tools/pkg/typedrec"
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
const friendStageWords = "friend stage <card>@<gen> --as friend.<name> [--dir <jobs dir>] stages a friend's dealt card in one step: it clones the card's REPO into <jobs dir>/<card>.w<N> at the card's NEXT head when one is carried, else at BASE, creates the delivered branch (the branch the bus message FRIEND-CARD DELIVERED carries), sets the friend's git identity in the clone's local config (the friend's nova-config row git_identity= field when it names one, else the sprint row's git_identity; the OK line says which, identity=friend or identity=sprint), and writes JOB.md naming the exact finish command. A read card (<primary>.r<attempt>.<reader>) is staged at the exact head under read (the primary attempt's head the read judges, never its NEXT or CARRY head nor BASE), checked out detached and checked with rev-parse, with pushing turned off in the clone; it sets no git identity and reads no config store (only the sprint store), and its JOB.md names the head, BRANCH, BASE, PATHS and the read verbs, READ-OK and READ-BROKEN (nova-sprint read --as friend.<name> --ok|--broken <read>@<gen> --epoch <n> --finding ...); a read with no full head under read is refused. It refuses a card not dealt to the named friend, and refuses when neither identity parses as Name <email> (it never falls back to the machine's global git config). A second run on an already staged directory refuses unless it is the same card@gen, branch and head, and then prints the JOB.md path and exits 0.\n"

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
	pg := fs.String("pg", "", "the config store a work card's friend git identity is read from, Postgres postgres://user@host:port/db with no password (else NOVA_PG_DSN), as nova-config takes it; a read card reads none")
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
		if cc.ID == card && max(1, cc.Int("gen")) == gen { // a record written before generations is gen 1
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
	if p.Kind == "read" {
		// a read is staged at the head under read, read-only, and commits nothing: it
		// needs no git identity, so no config store (friendStageRead)
		return a.friendStageRead(ctx, friendName, card, gen, p, work, *dir, stdout, stderr)
	}

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
	root, ok := a.friendJobsRoot(friendName, *dir)
	if !ok {
		return refuse(stderr, name, "wants --dir <jobs dir>, the jobs directory (HOME is not set)")
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
	if next != "" {
		// the work an earlier attempt pushed, which this one starts from: a finished
		// attempt's head, or one redealt unfinished with the head its progress named
		// (sprint.PushedHead), or the brief's CARRY line
		jobmd += friendStageCarried(p.BaseAttempt, next)
	}
	if err := os.WriteFile(filepath.Join(jobDir, friend.JobFile), []byte(jobmd), 0o644); err != nil {
		return refuse(stderr, name, "writing JOB.md: "+oneline.Err(err))
	}
	fmt.Fprintf(stdout, "FRIEND-STAGE OK friend=%s card=%s gen=%d branch=%s job=%s jobmd=%s identity=%s\n", friendName, card, gen, p.Branch, oneline.Field(job), filepath.Join(jobDir, friend.JobFile), identityFrom)
	a.outcome, a.changed = "accepted", "yes"
	return 0
}

// friendStageCarried is JOB.md's CARRIED line: the attempt whose pushed head the job starts
// from, and that head; the worker continues that work, never starts again from BASE.
func friendStageCarried(attempt int, head string) string {
	if attempt > 0 {
		return fmt.Sprintf("CARRIED: attempt %d head %s (its pushed work: continue it, never start again from BASE)\n", attempt, head)
	}
	return "CARRIED: head " + head + " (its pushed work: continue it, never start again from BASE)\n"
}

// friendJobsRoot is the jobs directory a friend's job is staged under: --dir when
// given, else HOME/<friend>-working/jobs; false when neither can be had.
func (a *app) friendJobsRoot(friendName, dir string) (string, bool) {
	if dir != "" {
		return dir, true
	}
	home := a.getenv("HOME")
	if home == "" {
		return "", false
	}
	return filepath.Join(home, friendName+"-working", friend.JobsDir), true
}

// friendStageRead stages a read card dealt to a friend: it clones the primary's REPO
// into <jobs dir>/<job>, fetches the exact head under read (the primary attempt's head
// the read judges, the read card's head; never the primary's NEXT or CARRY head, which
// is where a later attempt would start, and never BASE), checks it out detached, checks
// that HEAD is that commit, turns pushing off in the clone (a read changes, commits and
// pushes nothing), and writes JOB.md naming the head, the BASE, the PATHS and the read
// verbs that return the verdict. It reads only the sprint store the friend already
// reaches: a read commits nothing, so it sets no git identity and reads no config store.
func (a *app) friendStageRead(ctx context.Context, friendName, card string, gen int, p sprint.Packet, rc *sprint.Card, dir string, stdout, stderr io.Writer) int {
	const name = "friend stage"
	base := swarm.ReadCardBase([]byte(p.Brief))
	repoURL := swarm.CardRepoURL(base.Repo)
	if repoURL == "" {
		return refuse(stderr, name, fmt.Sprintf("read %s's card %s names no REPO: line to clone", oneline.Field(card), oneline.Field(p.Primary)))
	}
	head := cmp.Or(p.Head, rc.F("head"))
	if !typedrec.IsFullSha(head) {
		return refuseState(stderr, name, fmt.Sprintf("read %s names no full head under read (head %s); nothing was staged", oneline.Field(card), oneline.Escape(cmp.Or(head, "-"))))
	}
	workBranch := cmp.Or(p.WorkBranch, rc.F("branch"), "-")
	baseField := cmp.Or(p.WorkBase, base.Ref, base.Sha, "-")
	paths := strings.Join(swarm.CardPaths([]byte(p.Brief)), ", ")
	if paths == "" {
		paths = "none"
	}
	job := friendJobOf(p)
	root, ok := a.friendJobsRoot(friendName, dir)
	if !ok {
		return refuse(stderr, name, "wants --dir <jobs dir>, the jobs directory (HOME is not set)")
	}
	jobDir := filepath.Join(root, job)
	jobPath := filepath.Join(jobDir, friend.JobFile)

	// an already staged directory: the same read at the same gen and head is idempotent
	if old, err := os.ReadFile(jobPath); err == nil {
		s := string(old)
		if strings.Contains(s, "CARD: "+card+"\n") && strings.Contains(s, "GEN: "+strconv.Itoa(gen)+"\n") && strings.Contains(s, "HEAD: "+head+"\n") {
			fmt.Fprintln(stdout, jobPath)
			a.outcome, a.changed = "accepted", "no"
			return 0
		}
		return refuseState(stderr, name, fmt.Sprintf("the job %s is already staged as another card or head; nothing was written", oneline.Field(job)))
	} else if !os.IsNotExist(err) {
		return refuse(stderr, name, "the staged JOB.md cannot be read: "+oneline.Err(err))
	}

	if err := os.MkdirAll(root, 0o755); err != nil {
		return refuse(stderr, name, err.Error())
	}
	if _, err := friendStageGit(ctx, root, "clone", "-q", repoURL, jobDir); err != nil {
		return refuse(stderr, name, "clone of "+repoURL+" into "+oneline.Field(job)+": "+oneline.Err(err))
	}
	if _, err := friendStageGit(ctx, jobDir, "config", "remote.origin.pushurl", friendReadNoPush); err != nil {
		return refuse(stderr, name, "turning pushing off in the read's clone: "+oneline.Err(err))
	}
	if _, err := friendStageGit(ctx, jobDir, "fetch", "-q", "origin", head); err != nil {
		return refuse(stderr, name, "fetch of the head under read "+head+": "+oneline.Err(err))
	}
	if _, err := friendStageGit(ctx, jobDir, "checkout", "-q", "--detach", head); err != nil {
		return refuse(stderr, name, "checkout of the head under read "+head+": "+oneline.Err(err))
	}
	got, err := friendStageGit(ctx, jobDir, "rev-parse", "HEAD")
	if err != nil {
		return refuse(stderr, name, "rev-parse of the staged head: "+oneline.Err(err))
	}
	if strings.TrimSpace(got) != head {
		return refuse(stderr, name, fmt.Sprintf("the staged checkout is at %s, not the head under read %s", oneline.Escape(strings.TrimSpace(got)), head))
	}

	// JOB.md: the read, the head under read, its base and paths, and the read verbs
	read := fmt.Sprintf("nova-sprint read --as %s %%s %s@%d --epoch %d --finding %%s", sprint.FriendRow(friendName), card, gen, p.Epoch)
	jobmd := "CARD: " + card + "\n" +
		"GEN: " + strconv.Itoa(gen) + "\n" +
		"KIND: read\n" +
		"PRIMARY: " + p.Primary + "\n" +
		"ATTEMPT: " + strconv.Itoa(p.Attempt) + "\n" +
		"BRANCH: " + workBranch + "\n" +
		"BASE: " + baseField + "\n" +
		"HEAD: " + head + "\n" +
		"PATHS: " + paths + "\n" +
		"READ-ONLY: the checkout is detached at HEAD and pushing is off; change, commit and push nothing\n" +
		"READ-OK: " + fmt.Sprintf(read, "--ok", "'<what you checked, and the tests you ran>'") + "\n" +
		"READ-BROKEN: " + fmt.Sprintf(read, "--broken", "'<file:line, and what to change>'") + "\n"
	if err := os.WriteFile(jobPath, []byte(jobmd), 0o644); err != nil {
		return refuse(stderr, name, "writing JOB.md: "+oneline.Err(err))
	}
	fmt.Fprintf(stdout, "FRIEND-STAGE OK friend=%s card=%s gen=%d kind=read head=%s job=%s jobmd=%s\n", friendName, card, gen, head, oneline.Field(job), jobPath)
	a.outcome, a.changed = "accepted", "yes"
	return 0
}

// friendReadNoPush is the push URL a read's clone is given: no remote, so a push from
// the read fails instead of reaching origin.
const friendReadNoPush = "no-push://a-read-pushes-nothing"

// refuseState is refuse for a refusal of the world's state, not of the words
// typed (a card not on her row, a friend with no identity on record): exit 1,
// "failed or incomplete (including refused)", nothing written. Exit 2 stays
// usage, so the verb's help example, which names a card no test sprint holds,
// is not a usage refusal.
func refuseState(stderr io.Writer, verb, what string) int {
	refuse(stderr, verb, what)
	return 1
}
