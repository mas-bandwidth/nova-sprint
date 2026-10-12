package friend

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/atomicfile"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus"
	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// The daemon stages every job it holds (the finding of 2026-10-05: the first lanes of the
// rocketnet audit held with "no worktree, no remote" and the schema cards with "JOB.md
// missing: card not staged", and the coordinator staged clones and JOB.md files by hand all
// night). Writing a brief without staging its job is half a delivery: for each held work
// card whose brief is in her inbox, the daemon adds a git worktree of REPO at BASE on the
// card's branch at jobs/<job>/repo, of the one full bare mirror of the repository it keeps
// under mirrors/ (so a stage is a fetch and a worktree add, seconds and megabytes: on
// 2026-10-05 a whole clone per job had her disk at 99% with 42 staged clones and 765 finished
// job dirs), and writes jobs/<job>/JOB.md in the card-contract shape
// (docs/SPEC-CARD-CONTRACT.md). After each inbox cleanup the finished jobs' worktrees past
// FinishedJobsKept are pruned (Stager.Prune, pruneStep). No lane is handed the card until JOB.md is there. A
// repository her account cannot reach is one judgment to the coordinator with the remedy,
// never a lane that discovers it (docs/SPEC-FRIEND.md, staging). The machine is modelled in
// tla/FriendStage.tla (MCFriendStage*: a lane handed only a staged card, one judgment while it
// stands, a failed job staged again), and the worktrees and their pruning in
// pkg/friend/tla/JobWorktrees.tla (MCJobWorktrees*: a live job never pruned, a branch's
// work never lost, the finished worktrees within the cap after a prune).

// The directories under her working directory that staging writes.
const (
	JobsDir    = "jobs"
	MirrorsDir = "mirrors"
	JobFile    = "JOB.md"
)

// StageRetryEvery is how long a job whose stage failed waits before it is staged again; a
// judgment stands, said once, until a stage of its repository or card succeeds.
// MirrorFreshFor is how long a fetched mirror serves stages without fetching again, so a
// burst of cards on one repository is one fetch. MirrorCloneBudget bounds a mirror's fetch:
// the first, of the whole repository, is the one slow step (every later one fetches only what
// is new).
const (
	StageRetryEvery   = time.Minute
	MirrorFreshFor    = 10 * time.Second
	MirrorCloneBudget = 30 * time.Minute
)

// Packet is what a held work card says about its checkout: the repository (owner/name), the
// base it starts at (a branch, a tag or a full sha) and its pin (BaseSha, the commit a
// `BASE: <ref>@<sha40>` names; "" when unpinned), the branch its work is pushed to, and its
// attempt.
type Packet struct {
	Card, Job, Repo, Base, BaseSha, Branch string
	Attempt                                int
	// Brief is the card's brief, read for the SIBLINGS header that points the harness at
	// the sibling checkouts a stage clones (docs/SPEC-CARD-CONTRACT.md, the SIBLINGS
	// header; docs/SPEC-STAGING.md, the siblings). Empty stages no sibling.
	Brief string
}

var (
	statusBranchRE  = regexp.MustCompile(`push your work to the branch ([^\s;]+);`)
	statusAttemptRE = regexp.MustCompile(`, attempt (\d+);`)
	repoRE          = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*/[A-Za-z0-9_][A-Za-z0-9_.-]*$`)
	refRE           = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_./~-]*$`)
	shaRE           = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// PacketOf is the packet of a held card, the server's fields first and the brief's lines
// (its STATUS line, REPO: and BASE:) for any it did not send. It answers false for a card that
// stages nothing here: a read, or a work card whose brief names no REPO.
func PacketOf(h HeldCard) (Packet, bool) {
	if (h.Kind != "" && h.Kind != "work") || !strings.HasPrefix(h.Brief, "STATUS: nova-sprint card ") {
		return Packet{}, false
	}
	p := Packet{Card: h.Card, Job: h.Job, Repo: h.Repo, Base: h.Base, Branch: h.Branch, Attempt: h.Attempt, Brief: h.Brief}
	status, _, _ := strings.Cut(h.Brief, "\n")
	if m := statusBranchRE.FindStringSubmatch(status); p.Branch == "" && m != nil {
		p.Branch = m[1]
	}
	if m := statusAttemptRE.FindStringSubmatch(status); p.Attempt == 0 && m != nil {
		p.Attempt, _ = strconv.Atoi(m[1]) // ignored: the pattern is digits
	}
	if v, ok := cardhdr.Value(h.Brief, "REPO"); ok && p.Repo == "" {
		p.Repo = v
	}
	if v, ok := cardhdr.Value(h.Brief, "BASE"); ok && p.Base == "" {
		p.Base = v
	}
	// a pinned base (<ref>@<sha40>, as card trees write it) is its ref and its pin; one that
	// does not read stays as written, for check to refuse
	if ref, sha, ok := cardhdr.ParseBase(p.Base); ok && p.BaseSha == "" {
		p.Base, p.BaseSha = ref, sha
	}
	return p, p.Repo != ""
}

// NotStageable is a stage that waits on a person: her account cannot reach the repository
// (Card empty: every card on it waits), or the card's packet names what is not there. It is
// one judgment to the coordinator, its Remedy what clears it.
type NotStageable struct {
	Repo, Card, Why, Remedy string
	Job                     string // the job it was staging, when there was one
}

func (n *NotStageable) Error() string {
	if n.Card == "" {
		if n.Job != "" {
			return fmt.Sprintf("her account cannot reach %s (staging %s/%s): %s", n.Repo, JobsDir, n.Job, n.Why)
		}
		return fmt.Sprintf("her account cannot reach %s: %s", n.Repo, n.Why)
	}
	return fmt.Sprintf("card %s cannot be staged from %s: %s", n.Card, n.Repo, n.Why)
}

// key is what a judgment stands on: the repository, or the card.
func (n *NotStageable) key() string {
	if n.Card == "" {
		return "repo " + n.Repo
	}
	return "card " + n.Card
}

// StageJudgmentText is what the coordinator is told of a stage that waits on a person: one
// judgment, with its remedy.
func StageJudgmentText(friend string, n *NotStageable) (subject, body string) {
	if n.Card == "" {
		subject = fmt.Sprintf("judgment: %s cannot reach %s", friend, n.Repo)
	} else {
		subject = fmt.Sprintf("judgment: card %s cannot be staged on %s", n.Card, friend)
	}
	body = fmt.Sprintf("%s. The daemon staged no checkout and wrote no %s/<job>/%s, so no lane is handed the card(s); it tries again once a %s, and says this once until a stage succeeds. The remedy: %s\n",
		n.Error(), JobsDir, JobFile, StageRetryEvery, n.Remedy)
	return subject, body
}

// Stager stages jobs under Dir, her working directory, with the git credentials of the
// process that runs it (the daemon's: her account's). URL names a repository's remote (nil:
// GitHubURL); Env is git's whole environment (nil: the daemon's own, with
// GIT_TERMINAL_PROMPT=0, so git never waits on a prompt no one answers).
type Stager struct {
	Dir string
	URL func(repo string) string
	Env []string
	// SiblingMirror is the directory that holds the bench mirrors a stage clones a SIBLINGS
	// repository from, one bare mirror per repository named <name>.git (default
	// ~/nova-bench/mirror). A sibling with no mirror there is cloned from the forge.
	SiblingMirror string
}

// JobDir is where a job is staged, under her working directory.
func JobDir(dir, job string) string { return filepath.Join(dir, JobsDir, job) }

// Staged says a job's JOB.md is there.
func Staged(dir, job string) bool {
	_, err := os.Lstat(filepath.Join(JobDir(dir, job), JobFile))
	return err == nil
}

// stageAndWrite stages the sibling repositories p's brief names and writes JOB.md naming
// each, so the harness is pointed at $JOB/siblings/<name> (docs/SPEC-STAGING.md, the
// siblings). A sibling that cannot be staged refuses the stage with its name, so the card
// returns to ready (a bench fault, not the worker's).
func (s *Stager) stageAndWrite(p Packet, sha string) error {
	siblings, err := s.stageSiblings(p)
	if err != nil {
		return &NotStageable{Repo: p.Repo, Card: p.Card, Job: p.Job, Why: err.Error(), Remedy: "mirror or stage the sibling repository on this bench, then the card is staged again"}
	}
	return s.writeJob(p, sha, siblings)
}

// stageSiblings clones each repository a brief names in its SIBLINGS header at its ref into
// $JOB/siblings/<name> and answers the JOB.md lines naming each
// (SIBLING_<NAME>=$JOB/siblings/<name>), from the bench mirror where one exists else from
// the forge (docs/SPEC-CARD-CONTRACT.md, the SIBLINGS header; docs/SPEC-STAGING.md, the
// siblings). A brief with no SIBLINGS header stages none. A sibling that cannot be staged
// is refused with its name.
func (s *Stager) stageSiblings(p Packet) ([]string, error) {
	siblings := sprint.ParseSiblings(p.Brief)
	if len(siblings) == 0 {
		return nil, nil
	}
	mirror := s.SiblingMirror
	if mirror == "" {
		if home, err := os.UserHomeDir(); err == nil {
			mirror = filepath.Join(home, "nova-bench", "mirror")
		}
	}
	return sprint.StageSiblings(JobDir(s.Dir, p.Job), mirror, siblings)
}

// FinishedJobsKept is how many finished jobs' worktrees the daemon's cleanup keeps, the newest
// staged; the rest are pruned (Stager.Prune), at most PrunePerPass a cleanup, so the loop that
// runs it is held a few seconds at most.
const (
	FinishedJobsKept = 8
	PrunePerPass     = 4
)

// jobText is JobText with the sibling lines a stage cloned, so JOB.md points the harness at
// each $JOB/siblings/<name> (docs/SPEC-CARD-CONTRACT.md, the SIBLINGS header;
// docs/SPEC-STAGING.md, the siblings). No sibling lines writes JobText's own JOB.md.
func jobText(dir string, p Packet, sha string, siblings []string) string {
	attempt := max(p.Attempt, 1)
	checkout := filepath.Join(JobDir(dir, p.Job), "repo")
	outbox := filepath.Join(dir, "outbox", p.Job)
	sibs := ""
	if len(siblings) > 0 {
		sibs = "\nThe sibling repositories this card reads (read each as $SIBLING_<NAME>):\n" +
			strings.Join(siblings, "\n") + "\n"
	}
	return fmt.Sprintf("# JOB: work %s, attempt %d\n\n"+
		"The staged checkout: %s (a git worktree of %s at %s, %s, on branch %s).\n"+
		"%s"+
		"Work there; commit as the brief says; push the branch (git push -u origin %s).\n"+
		"Finish: write %s with 'Verdict: LAND|HOLD|FAIL' and 'Head: <sha>' on the first two lines, then the report; RESULT.md beside it with the same head. The coordinator syncs the outbox.\n"+
		"If the push is refused, leave the report with Verdict: HOLD naming 'no push' and the coordinator pushes from this checkout.\n",
		p.Card, attempt, checkout, p.Repo, p.Base, sha, sibs, p.Branch, p.Branch, filepath.Join(outbox, "REPORT.md"))
}

func (s *Stager) writeJob(p Packet, sha string, siblings []string) error {
	err := atomicfile.WriteFile(filepath.Join(JobDir(s.Dir, p.Job), JobFile), []byte(jobText(s.Dir, p, sha, siblings)), 0o644, atomicfile.NoReplace())
	if errors.Is(err, fs.ErrExist) {
		return nil // staged by another hand between the look and the write
	}
	return err
}

// stageResult is one stage's end, handed from its goroutine to the loop.
type stageResult struct {
	p       Packet
	sha     string
	err     error
	stopped bool // the daemon stopped while it ran: nothing is said, and the next loop stages it again
}

// stageStep is the daemon's staging, once a reconcile: every stage that ended is said (a
// judgment once while it stands, to the coordinator), and every held work card whose brief is
// in her inbox and whose job is not staged is staged, each on a goroutine of its own, so the
// loop beats on while a mirror is cloned. A job whose stage failed waits StageRetryEvery.
func (l *loop) stageStep(cards []HeldCard, now time.Time) {
	d := l.d
	at := now.UTC().Format(time.RFC3339)
	d.stageMu.Lock()
	done := d.stageDone
	d.stageDone = nil
	d.stageMu.Unlock()
	for _, r := range done {
		delete(d.staging, r.p.Job)
		var ns *NotStageable
		switch {
		case r.stopped:
		case r.err == nil:
			delete(d.stageRetry, r.p.Job)
			delete(d.stageSaid, "repo "+r.p.Repo)
			delete(d.stageSaid, "card "+r.p.Card)
			if r.sha != "" {
				d.Record(fmt.Sprintf("%s stage: staged %s/%s/repo (%s at %s, %s, on %s) and its %s", at, JobsDir, r.p.Job, r.p.Repo, r.p.Base, r.sha, r.p.Branch, JobFile))
			}
			if line, ok := d.stageDealt[r.p.Job]; ok && l.mode == ModeBatch {
				l.dealt = append(l.dealt, line)
			}
			delete(d.stageDealt, r.p.Job)
		case errors.As(r.err, &ns):
			d.stageRetry[r.p.Job] = now.Add(StageRetryEvery)
			if !d.stageSaid[ns.key()] {
				d.stageSaid[ns.key()] = true
				d.Record(fmt.Sprintf("%s stage: not staged %s/%s: %s; told the coordinator", at, JobsDir, r.p.Job, oneLine(ns.Error(), 400)))
				subject, body := StageJudgmentText(d.Friend, ns)
				l.tellKind(bus.KindBlocker, subject, body, now)
			}
		default:
			d.stageRetry[r.p.Job] = now.Add(StageRetryEvery)
			if key := "job " + r.p.Job + ": " + r.err.Error(); !d.stageSaid[key] {
				d.stageSaid[key] = true
				d.Record(fmt.Sprintf("%s stage: not staged %s/%s: %s; tried again in %s", at, JobsDir, r.p.Job, oneLine(r.err.Error(), 400), StageRetryEvery))
			}
		}
	}
	for _, h := range cards {
		p, ok := PacketOf(h)
		if !ok || !validJob(p.Job) || d.staging[p.Job] || now.Before(d.stageRetry[p.Job]) {
			continue
		}
		if !exists(filepath.Join(d.Dir, "inbox", p.Job, "BRIEF.md")) || Staged(d.Dir, p.Job) {
			continue
		}
		d.staging[p.Job] = true
		d.stageWG.Add(1)
		go func() {
			defer d.stageWG.Done()
			sha, err := d.Stage(l.ctx, p)
			d.stageMu.Lock()
			d.stageDone = append(d.stageDone, stageResult{p: p, sha: sha, err: err, stopped: err != nil && l.ctx.Err() != nil})
			d.stageMu.Unlock()
		}()
	}
}

// pruneStep is the daemon's cleanup of finished jobs, run in the loop after each inbox
// cleanup, so what is live cannot change under it (a prune on a goroutine of its own, handed a
// snapshot, could remove a job dealt to her again and handed to a lane meanwhile: the reversed
// witness "async" of tla/JobWorktrees.tla). Live is every job held on her row, run by a lane
// (keep) or being staged; Stager.Prune never touches one, nor a job whose brief is in her
// inbox, and removes at most PrunePerPass, never waiting on a mirror a stage holds. Each job
// removed is said, and a failure once while it stands.
func (l *loop) pruneStep(held []HeldCard, keep map[string]bool, now time.Time) {
	d := l.d
	if d.Prune == nil {
		return
	}
	live := map[string]bool{}
	for _, h := range held {
		live[h.Job] = true
	}
	for job := range keep {
		live[job] = true
	}
	for job := range d.staging {
		live[job] = true
	}
	pruned, err := d.Prune(l.ctx, live)
	at := now.UTC().Format(time.RFC3339)
	for _, job := range pruned {
		d.Record(fmt.Sprintf("%s prune: removed %s/%s and its worktree: its card is finished (%d finished kept)", at, JobsDir, job, FinishedJobsKept))
	}
	switch {
	case err == nil:
		d.pruneSaid = ""
	case l.ctx.Err() != nil:
	case err.Error() != d.pruneSaid:
		d.pruneSaid = err.Error()
		d.Record(fmt.Sprintf("%s prune: not pruned: %s; tried again at the next cleanup", at, oneLine(err.Error(), 400)))
	}
}

// stageOwed says a held card is not handed to a lane yet: the daemon stages jobs, the card is
// one it stages, and its JOB.md is not there.
func (d *Daemon) stageOwed(h HeldCard) bool {
	if d.Stage == nil {
		return false
	}
	_, ok := PacketOf(h)
	return ok && !Staged(d.Dir, h.Job)
}

// TipBudget bounds the one ls-remote of Tip.
const TipBudget = 10 * time.Second
