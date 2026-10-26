package main

import (
	"cmp"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-sprint/pkg/friend"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	toolfriend "github.com/mas-bandwidth/nova-tools/pkg/friend"
	"github.com/mas-bandwidth/nova-tools/pkg/member"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
	"github.com/mas-bandwidth/nova-tools/pkg/typedrec"
)

// gitFn runs git with argv in a working directory and returns its combined output.
type gitFn func(ctx context.Context, dir string, args ...string) (string, error)

// mkdirFn makes a directory tree for friend stage. The production function runs
// out of process, so a filesystem call that stops answering can be cancelled with
// the served request instead of holding the server's control line indefinitely.
type mkdirFn func(ctx context.Context, dir string) error

// runStageGit is the real git, the app's stageGit unless a test gives its own: friend
// stage clones and configures with it. It is a field of the app and never a
// package variable, so two parallel tests that each record friend stage's argv
// never record each other's.
func runStageGit(ctx context.Context, dir string, args ...string) (string, error) {
	out, exit, err := toolfriend.RealExec(ctx, dir, "git", args, "")
	if err != nil {
		return out, err
	}
	if exit != 0 {
		return out, fmt.Errorf("git exited with status %d", exit)
	}
	return out, nil
}

// runStageMkdir uses the same process-group and WaitDelay runner as stage's Git
// calls. A filesystem mkdir can itself stop in the kernel before Git starts; it
// therefore belongs inside the same request bound, not in the server process.
func runStageMkdir(ctx context.Context, dir string) error {
	name, args, stdin := stageMkdirCommand(runtime.GOOS, dir)
	out, exit, err := toolfriend.RealExec(ctx, "", name, args, stdin)
	if err != nil {
		return fmt.Errorf("mkdir -p %s: %w", oneline.Field(dir), err)
	}
	if exit != 0 {
		if why := strings.TrimSpace(out); why != "" {
			return fmt.Errorf("mkdir -p %s exited with status %d: %s", oneline.Field(dir), exit, oneline.Escape(why))
		}
		return fmt.Errorf("mkdir -p %s exited with status %d", oneline.Field(dir), exit)
	}
	return nil
}

// stageMkdirCommand is one direct program with the path in its own argv element,
// never shell text. Windows' mkdir is a cmd.exe builtin, not an executable; its
// standard PowerShell creates the directory through .NET and reads base64 of
// the UTF-8 path from stdin (Windows PowerShell joins words after -Command into
// code, and its console input encoding is the active code page, not UTF-8).
// Unix uses the ordinary mkdir program.
func stageMkdirCommand(goos, dir string) (string, []string, string) {
	if goos == "windows" {
		return "powershell.exe", []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "[IO.Directory]::CreateDirectory([Text.Encoding]::UTF8.GetString([Convert]::FromBase64String([Console]::In.ReadToEnd()))) > $null"}, base64.StdEncoding.EncodeToString([]byte(dir))
	}
	return "mkdir", []string{"-p", "--", dir}, ""
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
	if _, err := a.store(*c); err != nil {
		return refuse(stderr, name, err.Error())
	}
	ctx := a.runCtx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, swarm.DefaultStageTimeout)
	defer cancel()

	// (1) take only a short assignment snapshot under the global control line: the card
	// must be on friend.<name>'s row at that generation, ready or working
	snap, code, ok := a.takeFriendStageSnap(ctx, *c, friendName, card, gen, true, stderr)
	if !ok {
		return code
	}
	if snap.kind == "read" {
		// a read is staged at the head under read, read-only, and commits nothing: it
		// needs no git identity, so no config store (friendStageRead)
		return a.friendStageRead(ctx, *c, snap, *dir, stdout, stderr)
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
		if snap.sprintIDErr != nil {
			return a.readFailed(name, snap.sprintIDErr, stderr)
		}
		if id, ok = config.ParseGitIdentity(snap.sprintID); !ok {
			return refuseState(stderr, name, fmt.Sprintf("the nova-config row of friend %s names no git_identity and the sprint row's git_identity %s is not 'Name <email>', so staging would fall back to the machine's global git config; %s", friendName, strconv.Quote(snap.sprintID), identityRemedy))
		}
		identityFrom = "sprint"
	}

	// (2) the delivered branch is the one the bus message FRIEND-CARD DELIVERED
	// carries (friendcards.go): reuse p.Branch, never compute a second one
	if snap.repoURL == "" {
		return refuse(stderr, name, fmt.Sprintf("card %s names no REPO: line to clone", oneline.Field(card)))
	}
	root, ok := a.friendJobsRoot(friendName, *dir)
	if !ok {
		return refuse(stderr, name, "wants --dir <jobs dir>, the jobs directory (HOME is not set)")
	}
	jobDir := filepath.Join(root, snap.job)
	jobPath := filepath.Join(jobDir, friend.JobFile)

	// (5) an already staged directory
	if _, err := os.Lstat(jobPath); err == nil {
		old, err := os.ReadFile(jobPath)
		if err != nil {
			return refuse(stderr, name, "the staged JOB.md cannot be read: "+oneline.Err(err))
		}
		s := string(old)
		if strings.Contains(s, "CARD: "+card+"\n") && strings.Contains(s, "GEN: "+strconv.Itoa(gen)+"\n") &&
			strings.Contains(s, "BRANCH: "+snap.branch+"\n") && strings.Contains(s, "HEAD: "+snap.startHead+"\n") {
			if code, ok := a.revalidateFriendStageSnap(ctx, *c, snap, stderr); !ok {
				return code
			}
			fmt.Fprintln(stdout, jobPath)
			a.outcome, a.changed = "accepted", "no"
			return 0
		}
		return refuseState(stderr, name, fmt.Sprintf("the job %s is already staged as another card, branch or head; nothing was written", oneline.Field(snap.job)))
	} else if !os.IsNotExist(err) {
		return refuse(stderr, name, err.Error())
	}

	_, statErr := os.Lstat(jobDir)
	createdJobDir := os.IsNotExist(statErr)
	cleanupUnstagedDir := func() {
		if createdJobDir {
			_ = os.RemoveAll(jobDir)
		}
	}

	// the clone, then the identity in the clone's local config before any other
	// git invocation can see a global identity
	if err := a.stageMkdir(ctx, filepath.Dir(jobDir)); err != nil {
		return refuse(stderr, name, err.Error())
	}
	if _, err := a.stageGit(ctx, filepath.Dir(jobDir), "clone", "-q", snap.repoURL, jobDir); err != nil {
		cleanupUnstagedDir()
		return refuse(stderr, name, "clone of "+snap.repoURL+" into "+oneline.Field(snap.job)+": "+oneline.Err(err))
	}
	if _, err := a.stageGit(ctx, jobDir, "config", "user.name", id.Name); err != nil {
		cleanupUnstagedDir()
		return refuse(stderr, name, "setting the clone's local user.name: "+oneline.Err(err))
	}
	if _, err := a.stageGit(ctx, jobDir, "config", "user.email", id.Email); err != nil {
		cleanupUnstagedDir()
		return refuse(stderr, name, "setting the clone's local user.email: "+oneline.Err(err))
	}
	if snap.next != "" {
		if _, err := a.stageGit(ctx, jobDir, "fetch", "-q", "origin", snap.next); err != nil {
			cleanupUnstagedDir()
			return refuse(stderr, name, "fetch of the NEXT head "+oneline.Escape(snap.next)+": "+oneline.Err(err))
		}
	}
	if _, err := a.stageGit(ctx, jobDir, "checkout", "-q", "-B", snap.branch, snap.startHead); err != nil {
		cleanupUnstagedDir()
		return refuse(stderr, name, "checkout of "+oneline.Field(snap.branch)+" at "+oneline.Escape(snap.startHead)+": "+oneline.Err(err))
	}

	// (4) JOB.md: the card id and gen, the branch, the base, the head it
	// started from, and the exact finish command (and its --failed form)
	finish := fmt.Sprintf("nova-sprint finish --as friend.%s %s@%d --epoch %d --head %s --branch %s --base %s", friendName, card, gen, snap.epoch, snap.startHead, snap.branch, snap.baseField)
	jobmd := "CARD: " + card + "\n" +
		"GEN: " + strconv.Itoa(gen) + "\n" +
		"BRANCH: " + snap.branch + "\n" +
		"BASE: " + snap.baseField + "\n" +
		"HEAD: " + snap.startHead + "\n" +
		"FINISH: " + finish + "\n" +
		"FINISH-FAILED: " + finish + " --failed\n"
	if snap.next != "" {
		// the work an earlier attempt pushed, which this one starts from: a finished
		// attempt's head, or one redealt unfinished with the head its progress named
		// (sprint.PushedHead), or the brief's CARRY line
		jobmd += friendStageCarried(snap.baseAttempt, snap.next)
	}
	okLine := fmt.Sprintf("FRIEND-STAGE OK friend=%s card=%s gen=%d branch=%s job=%s jobmd=%s identity=%s\n", friendName, card, gen, snap.branch, oneline.Field(snap.job), jobPath, identityFrom)
	return a.publishFriendStage(ctx, *c, snap, jobDir, jobPath, createdJobDir, jobmd, okLine, stdout, stderr)
}

// friendStageSnap is the short assignment snapshot friend stage takes under the
// global control line before staging and compares against when reacquiring the
// line before JOB.md is published.
type friendStageSnap struct {
	friendName  string
	card        string
	gen         int
	kind        string
	branch      string
	startHead   string
	baseField   string
	next        string
	repoURL     string
	paths       string
	job         string
	epoch       uint64
	baseAttempt int
	primary     string
	attempt     int
	sprintID    string
	sprintIDErr error
}

func (a *app) lockStageLine(ctx context.Context) error {
	if a.stageLine == nil {
		return ctx.Err()
	}
	return a.stageLine.LockCtx(ctx)
}

func (a *app) unlockStageLine() {
	if a.stageLine != nil {
		a.stageLine.Unlock()
	}
}

func (a *app) takeFriendStageSnap(ctx context.Context, c common, friendName, card string, gen int, wantSprintID bool, stderr io.Writer) (friendStageSnap, int, bool) {
	const name = "friend stage"
	if err := a.lockStageLine(ctx); err != nil {
		return friendStageSnap{}, refuse(stderr, name, err.Error()), false
	}
	defer a.unlockStageLine()
	return a.readFriendStageSnapLocked(ctx, c, friendName, card, gen, wantSprintID, stderr)
}

func (a *app) readFriendStageSnapLocked(ctx context.Context, c common, friendName, card string, gen int, wantSprintID bool, stderr io.Writer) (friendStageSnap, int, bool) {
	const name = "friend stage"
	st, err := a.store(c)
	if err != nil {
		return friendStageSnap{}, refuse(stderr, name, err.Error()), false
	}
	cards, err := st.ReadCells(ctx, sprint.Fleet, sprint.FriendRow(friendName), sprint.Ready, sprint.Working)
	if err != nil {
		return friendStageSnap{}, a.readFailed(name, err, stderr), false
	}
	var work *sprint.Card
	for _, cc := range cards {
		if cc.ID == card && max(1, cc.Int("gen")) == gen { // a record written before generations is gen 1
			work = cc
			break
		}
	}
	if work == nil {
		return friendStageSnap{}, refuseState(stderr, name, fmt.Sprintf("card %s at gen %d is not dealt to friend %s (ready or working)", oneline.Field(card), gen, friendName)), false
	}
	pk, err := st.Packets(ctx, []*sprint.Card{work})
	if err != nil {
		return friendStageSnap{}, a.readFailed(name, err, stderr), false
	}
	p := pk[0]
	base := swarm.ReadCardBase([]byte(p.Brief))
	snap := friendStageSnap{
		friendName: friendName,
		card:       card,
		gen:        gen,
		kind:       p.Kind,
		repoURL:    swarm.CardRepoURL(base.Repo),
		job:        friendJobOf(p),
		epoch:      p.Epoch,
	}
	if p.Kind == "read" {
		snap.primary = p.Primary
		snap.attempt = p.Attempt
		snap.startHead = cmp.Or(p.Head, work.F("head"))
		snap.branch = cmp.Or(p.WorkBranch, work.F("branch"), "-")
		snap.baseField = cmp.Or(p.WorkBase, base.Ref, base.Sha, "-")
		paths := strings.Join(swarm.CardPaths([]byte(p.Brief)), ", ")
		if paths == "" {
			paths = "none"
		}
		snap.paths = paths
		return snap, 0, true
	}
	snap.branch = p.Branch
	snap.baseAttempt = p.BaseAttempt
	baseField := base.Sha
	if baseField == "" {
		baseField = base.Ref
	}
	snap.baseField = baseField
	next := p.BaseHead
	if next == "" {
		if carry, ok := member.CarryOf(p.Brief); ok {
			next = carry.Head
		}
	}
	snap.next = next
	switch {
	case next != "":
		snap.startHead = next
	case base.Sha != "":
		snap.startHead = base.Sha
	default:
		snap.startHead = base.Ref
	}
	if wantSprintID {
		snap.sprintID, snap.sprintIDErr = st.GitIdentity(ctx)
	}
	return snap, 0, true
}

func (a *app) revalidateFriendStageSnap(ctx context.Context, c common, snap friendStageSnap, stderr io.Writer) (int, bool) {
	const name = "friend stage"
	if err := a.lockStageLine(ctx); err != nil {
		return refuse(stderr, name, err.Error()), false
	}
	defer a.unlockStageLine()
	return a.revalidateFriendStageSnapLocked(ctx, c, snap, stderr)
}

func (a *app) revalidateFriendStageSnapLocked(ctx context.Context, c common, snap friendStageSnap, stderr io.Writer) (int, bool) {
	const name = "friend stage"
	cur, code, ok := a.readFriendStageSnapLocked(ctx, c, snap.friendName, snap.card, snap.gen, false, stderr)
	if !ok {
		return code, false
	}
	if cur.kind != snap.kind || cur.branch != snap.branch || cur.startHead != snap.startHead {
		return refuseState(stderr, name, fmt.Sprintf("card %s at gen %d on friend %s changed while staging (branch %s -> %s, head %s -> %s); nothing was staged",
			oneline.Field(snap.card), snap.gen, snap.friendName, oneline.Escape(snap.branch), oneline.Escape(cur.branch), oneline.Escape(snap.startHead), oneline.Escape(cur.startHead))), false
	}
	return 0, true
}

func (a *app) publishFriendStage(ctx context.Context, c common, snap friendStageSnap, jobDir, jobPath string, createdJobDir bool, jobmd, okLine string, stdout, stderr io.Writer) int {
	const name = "friend stage"
	tmpPath := filepath.Join(jobDir, "."+friend.JobFile+".stage")
	cleanup := func() {
		_ = os.Remove(tmpPath)
		if createdJobDir {
			if _, err := os.Lstat(jobPath); os.IsNotExist(err) {
				_ = os.RemoveAll(jobDir)
			}
		}
	}
	if err := os.WriteFile(tmpPath, []byte(jobmd), 0o644); err != nil {
		cleanup()
		return refuse(stderr, name, "writing JOB.md: "+oneline.Err(err))
	}
	if err := a.lockStageLine(ctx); err != nil {
		cleanup()
		return refuse(stderr, name, err.Error())
	}
	defer a.unlockStageLine()
	if code, ok := a.revalidateFriendStageSnapLocked(ctx, c, snap, stderr); !ok {
		cleanup()
		return code
	}
	if err := os.Rename(tmpPath, jobPath); err != nil {
		cleanup()
		return refuse(stderr, name, "writing JOB.md: "+oneline.Err(err))
	}
	fmt.Fprint(stdout, okLine)
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
func (a *app) friendStageRead(ctx context.Context, c common, snap friendStageSnap, dir string, stdout, stderr io.Writer) int {
	const name = "friend stage"
	if snap.repoURL == "" {
		return refuse(stderr, name, fmt.Sprintf("read %s's card %s names no REPO: line to clone", oneline.Field(snap.card), oneline.Field(snap.primary)))
	}
	head := snap.startHead
	if !typedrec.IsFullSha(head) {
		return refuseState(stderr, name, fmt.Sprintf("read %s names no full head under read (head %s); nothing was staged", oneline.Field(snap.card), oneline.Escape(cmp.Or(head, "-"))))
	}
	root, ok := a.friendJobsRoot(snap.friendName, dir)
	if !ok {
		return refuse(stderr, name, "wants --dir <jobs dir>, the jobs directory (HOME is not set)")
	}
	jobDir := filepath.Join(root, snap.job)
	jobPath := filepath.Join(jobDir, friend.JobFile)

	// an already staged directory: the same read at the same gen and head is idempotent
	if old, err := os.ReadFile(jobPath); err == nil {
		s := string(old)
		if strings.Contains(s, "CARD: "+snap.card+"\n") && strings.Contains(s, "GEN: "+strconv.Itoa(snap.gen)+"\n") && strings.Contains(s, "HEAD: "+head+"\n") {
			if code, ok := a.revalidateFriendStageSnap(ctx, c, snap, stderr); !ok {
				return code
			}
			fmt.Fprintln(stdout, jobPath)
			a.outcome, a.changed = "accepted", "no"
			return 0
		}
		return refuseState(stderr, name, fmt.Sprintf("the job %s is already staged as another card or head; nothing was written", oneline.Field(snap.job)))
	} else if !os.IsNotExist(err) {
		return refuse(stderr, name, "the staged JOB.md cannot be read: "+oneline.Err(err))
	}

	_, statErr := os.Lstat(jobDir)
	createdJobDir := os.IsNotExist(statErr)
	cleanupUnstagedDir := func() {
		if createdJobDir {
			_ = os.RemoveAll(jobDir)
		}
	}

	if err := a.stageMkdir(ctx, root); err != nil {
		return refuse(stderr, name, err.Error())
	}
	if _, err := a.stageGit(ctx, root, "clone", "-q", snap.repoURL, jobDir); err != nil {
		cleanupUnstagedDir()
		return refuse(stderr, name, "clone of "+snap.repoURL+" into "+oneline.Field(snap.job)+": "+oneline.Err(err))
	}
	if _, err := a.stageGit(ctx, jobDir, "config", "remote.origin.pushurl", friendReadNoPush); err != nil {
		cleanupUnstagedDir()
		return refuse(stderr, name, "turning pushing off in the read's clone: "+oneline.Err(err))
	}
	if _, err := a.stageGit(ctx, jobDir, "fetch", "-q", "origin", head); err != nil {
		cleanupUnstagedDir()
		return refuse(stderr, name, "fetch of the head under read "+head+": "+oneline.Err(err))
	}
	if _, err := a.stageGit(ctx, jobDir, "checkout", "-q", "--detach", head); err != nil {
		cleanupUnstagedDir()
		return refuse(stderr, name, "checkout of the head under read "+head+": "+oneline.Err(err))
	}
	got, err := a.stageGit(ctx, jobDir, "rev-parse", "HEAD")
	if err != nil {
		cleanupUnstagedDir()
		return refuse(stderr, name, "rev-parse of the staged head: "+oneline.Err(err))
	}
	if strings.TrimSpace(got) != head {
		cleanupUnstagedDir()
		return refuse(stderr, name, fmt.Sprintf("the staged checkout is at %s, not the head under read %s", oneline.Escape(strings.TrimSpace(got)), head))
	}

	// JOB.md: the read, the head under read, its base and paths, and the read verbs
	read := fmt.Sprintf("nova-sprint read --as %s %%s %s@%d --epoch %d --finding %%s", sprint.FriendRow(snap.friendName), snap.card, snap.gen, snap.epoch)
	jobmd := "CARD: " + snap.card + "\n" +
		"GEN: " + strconv.Itoa(snap.gen) + "\n" +
		"KIND: read\n" +
		"PRIMARY: " + snap.primary + "\n" +
		"ATTEMPT: " + strconv.Itoa(snap.attempt) + "\n" +
		"BRANCH: " + snap.branch + "\n" +
		"BASE: " + snap.baseField + "\n" +
		"HEAD: " + head + "\n" +
		"PATHS: " + snap.paths + "\n" +
		"READ-ONLY: the checkout is detached at HEAD and pushing is off; change, commit and push nothing\n" +
		"READ-OK: " + fmt.Sprintf(read, "--ok", "'<what you checked, and the tests you ran>'") + "\n" +
		"READ-BROKEN: " + fmt.Sprintf(read, "--broken", "'<file:line, and what to change>'") + "\n"
	okLine := fmt.Sprintf("FRIEND-STAGE OK friend=%s card=%s gen=%d kind=read head=%s job=%s jobmd=%s\n", snap.friendName, snap.card, snap.gen, head, oneline.Field(snap.job), jobPath)
	return a.publishFriendStage(ctx, c, snap, jobDir, jobPath, createdJobDir, jobmd, okLine, stdout, stderr)
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
