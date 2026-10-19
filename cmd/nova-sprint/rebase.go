package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// rebase is the coordinator's: it rewrites the base of every unlanded card on
// a branch (coordinator.go, who may run a verb).
func init() { verbClasses["rebase"] = classCoordinator }

// rebaseStep is the store step the verb runs: it plans sprint.Rebase over the
// work table (sprint.Rebase, docs/SPEC-SPRINT.md, the rebase verb).
func rebaseStep(r sprint.RebaseReq) store.Step {
	return store.Step{Args: store.ArgsOf(r), Verb: "rebase", Load: []string{sprint.Work}, Mirrors: true,
		Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.Rebase(s, r) }}
}

// cmdRebase moves every unlanded card whose BASE is --from to --to
// (docs/SPEC-SPRINT.md, the rebase verb): an undealt card's brief changes, a
// dealt or merging card keeps its head, and the new base must contain the old
// one (--repo-dir, git merge-base --is-ancestor). It runs on a RUNNING
// machine as on a STOPPED one. The preview only reads; runStep records the
// actor.
func (a *app) cmdRebase(args []string, stdout, stderr io.Writer) int {
	fs, c := a.verbSetup("rebase")
	recordForwardMerge := fs.Bool("record-forward-merge", false, "record a verified merge into --to and recheck every waiting, ready and review card on that repository/base")
	from := fs.String("from", "", "the source branch merged into the destination, or the old base for a regular rebase")
	to := fs.String("to", "", "the destination base branch for --record-forward-merge, or the replacement branch for a regular rebase")
	repo := fs.String("repo", "", "the owner/name repository whose base took the merge (required with --record-forward-merge)")
	sha := fs.String("sha", "", "the full merge commit now at origin/--to (required with --record-forward-merge)")
	reason := fs.String("reason", "", "why the forward-merge was made (required with --record-forward-merge)")
	repoDir := fs.String("repo-dir", "", "a clone of the cards' repository, its branches as fetched")
	dry := fs.Bool("dry-run", false, "show the planned rebases and refusals without writing")
	pos, err := parse(fs, args)
	if err != nil {
		return refuse(stderr, "rebase", err.Error())
	}
	if len(pos) > 0 {
		return refuse(stderr, "rebase", "takes no card ids: "+strings.Join(pos, " ")+"; it moves every card whose BASE is --from")
	}
	if *recordForwardMerge {
		return a.cmdRecordForwardMerge(*c, *repo, *repoDir, *from, *to, *sha, *reason, *dry, stdout, stderr)
	}
	if strings.TrimSpace(*from) == "" || strings.TrimSpace(*to) == "" {
		return refuse(stderr, "rebase", "wants --from <branch> and --to <branch>; run: nova-sprint help rebase")
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, "rebase", err.Error())
	}
	req := sprint.RebaseReq{From: *from, To: *to, Who: c.actor}
	if *repoDir != "" {
		dir := *repoDir
		req.Contains = func(from, to string) error {
			_, err := sprint.RunGit(context.Background(), dir, "merge-base", "--is-ancestor", "--end-of-options", from, to)
			return err
		}
	}
	if *dry {
		_, snap, err := st.Check(context.Background(), 1)
		if err != nil {
			return a.readFailed("rebase", err, stderr)
		}
		plan := sprint.Rebase(snap, req)
		lines := append([]string(nil), plan.Said...)
		for _, u := range plan.Units {
			lines = append(lines, u.Moved)
		}
		for _, r := range plan.Refused {
			lines = append(lines, "REFUSED "+r.Key+": "+r.Why)
		}
		code, status := 0, "ok"
		if len(plan.Refused) > 0 {
			code, status = 1, "refused"
		}
		sayOK(stdout, c.json, "rebase", fmt.Sprintf("REBASE %s DRY-RUN from=%s to=%s changes=%d refused=%d\n%s", strings.ToUpper(status), *from, *to, len(plan.Units), len(plan.Refused), strings.Join(lines, "\n")), map[string]any{"status": status, "exit": code, "dry_run": true, "from": *from, "to": *to, "changes": len(plan.Units), "lines": lines, "refused": plan.Refused})
		return code
	}
	return a.runStep("rebase", *c, st, rebaseStep(req), stdout, stderr)
}

// cmdRecordForwardMerge is the coordinator's forward-merge record path. It verifies that
// the named origin/base is the supplied merge commit, that the commit is a merge and
// advances any known base tip, recomputes the branch's behind count, and runs the existing
// brief-at-base lint for the exact affected cards before one fenced store step writes the
// new durable tip and check witnesses.
func (a *app) cmdRecordForwardMerge(c common, repo, repoDir, from, base, sha, reason string, dry bool, stdout, stderr io.Writer) int {
	const verb = "rebase"
	if strings.TrimSpace(repo) == "" || strings.TrimSpace(repoDir) == "" || strings.TrimSpace(from) == "" || strings.TrimSpace(base) == "" || strings.TrimSpace(sha) == "" || strings.TrimSpace(reason) == "" {
		return refuse(stderr, verb, "--record-forward-merge wants --repo <owner/name> --repo-dir <clone> --from <source branch> --to <base branch> --sha <full merge commit> --reason <text>")
	}
	ctx := context.Background()
	st, err := a.store(c)
	if err != nil {
		return refuse(stderr, verb, err.Error())
	}
	git := func(args ...string) (string, error) {
		return sprint.RunGit(ctx, repoDir, args...)
	}
	for _, name := range []string{from, base, "dev"} {
		if _, err := git("check-ref-format", "refs/heads/"+name); err != nil {
			return refuse(stderr, verb, "invalid branch name "+name+"; nothing was written")
		}
	}
	l := &lander{a: a, repoDir: repoDir}
	if why := l.originIs(ctx, repoDir, repo); why != "" {
		return refuse(stderr, verb, "the clone origin does not match "+repo+": "+why+"; nothing was written")
	}
	if _, err := git("fetch", "--no-tags", "origin",
		"+refs/heads/"+base+":refs/remotes/origin/"+base,
		"+refs/heads/dev:refs/remotes/origin/dev"); err != nil {
		return refuse(stderr, verb, "the destination base and dev branches could not be fetched: "+err.Error()+"; nothing was written")
	}
	resolved, err := git("rev-parse", "--verify", "--quiet", "--end-of-options", sha+"^{commit}")
	if err != nil || strings.TrimSpace(resolved) == "" {
		return refuse(stderr, verb, "--sha is not a commit in the named repository; nothing was written")
	}
	merge := strings.TrimSpace(resolved)
	if strings.TrimSpace(sha) != merge {
		return refuse(stderr, verb, "--sha must be the full commit id; nothing was written")
	}
	tip, err := git("rev-parse", "--verify", "--quiet", "--end-of-options", "refs/remotes/origin/"+base+"^{commit}")
	if err != nil || strings.TrimSpace(tip) != merge {
		return refuse(stderr, verb, "origin/"+base+" is not at --sha; nothing was written")
	}
	snap, err := st.Load(ctx, []string{sprint.Work, sprint.Merge}, nil)
	if err != nil {
		return a.readFailed(verb, err, stderr)
	}
	oldTip := forwardMergeTip(snap, repo, base)
	if oldTip != "" && oldTip != merge {
		if _, err := git("merge-base", "--is-ancestor", oldTip, merge); err != nil {
			return refuse(stderr, verb, "the new base tip does not advance the recorded tip "+oldTip+"; nothing was written")
		}
	}
	parents, err := git("rev-list", "--parents", "-n", "1", merge)
	fields := strings.Fields(parents)
	if err != nil || len(fields) < 3 {
		return refuse(stderr, verb, "--sha is not a merge commit; nothing was written")
	}
	if oldTip != merge {
		if _, err := git("fetch", "--no-tags", "origin", "+refs/heads/"+from+":refs/remotes/origin/"+from); err != nil {
			return refuse(stderr, verb, "the source branch "+from+" could not be fetched; nothing was written")
		}
		if _, err := git("merge-base", "--is-ancestor", fields[2], "refs/remotes/origin/"+from); err != nil {
			return refuse(stderr, verb, "the merge's second parent is not held by source branch "+from+"; nothing was written")
		}
	}
	if oldTip == merge {
		if len(pendingForwardMergeBriefs(snap, repo, base, merge)) == 0 {
			fmt.Fprintf(stdout, "FORWARD-MERGE OK repo=%s base=%s sha=%s already recorded and rechecked\n", repo, base, merge)
			return 0
		}
	} else if !dry {
		req := sprint.ForwardMergeReq{Repo: repo, Base: base, From: from, Merge: merge, Who: c.actor, Reason: reason}
		step := store.Step{Args: store.ArgsOf(req), Verb: "rebase record-forward-merge", Load: []string{sprint.Work, sprint.Merge}, Mirrors: true,
			Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.RecordForwardMerge(s, req) }}
		if code := a.runStep("rebase record-forward-merge", c, st, step, stdout, stderr); code != 0 {
			return code
		}
		snap, err = st.Load(ctx, []string{sprint.Work, sprint.Merge}, nil)
		if err != nil {
			return a.readFailed(verb, err, stderr)
		}
	}
	if dry {
		var briefs []briefCheck
		for _, col := range []sprint.State{sprint.Waiting, sprint.Ready, sprint.Review} {
			for _, stream := range snap.Streams() {
				for _, card := range snap.Work.Cell(stream, col) {
					if card.F("kind") == "sentinel" {
						continue
					}
					cardRepo, cardBase := sprint.CardRepoAndBase(snap, card)
					if cardRepo == repo && cardBase == base {
						briefs = append(briefs, briefCheck{id: card.ID, stream: stream, brief: card.F("brief")})
					}
				}
			}
		}
		if len(briefs) == 0 {
			return refuse(stderr, verb, "no waiting, ready or review card names "+repo+" BASE "+base+"; nothing was changed")
		}
		fmt.Fprintf(stdout, "FORWARD-MERGE WOULD-RECORD repo=%s base=%s from=%s sha=%s cards=%d; dry-run skips brief checks and writes\n", repo, base, from, merge, len(briefs))
		for _, b := range briefs {
			fmt.Fprintf(stdout, "WOULD RECHECK card=%s stream=%s\n", b.id, b.stream)
		}
		return 0
	}

	briefs := pendingForwardMergeBriefs(snap, repo, base, merge)
	if len(briefs) == 0 {
		return refuse(stderr, verb, "no cards remain pending recheck for "+repo+" BASE "+base+" at "+merge)
	}
	counts, err := git("rev-list", "--left-right", "--count", "refs/remotes/origin/dev..."+merge)
	if err != nil {
		return refuse(stderr, verb, "the base's behind count could not be recomputed: "+err.Error()+"; cards remain pending recheck")
	}
	count := strings.Fields(counts)
	if len(count) != 2 {
		return refuse(stderr, verb, "the base's behind count was not returned; cards remain pending recheck")
	}
	behind, err := strconv.Atoi(count[0])
	if err != nil || behind < 0 {
		return refuse(stderr, verb, "the base's behind count was invalid; cards remain pending recheck")
	}
	if code := a.holdBriefBase(verb, st, false, stderr, briefs...); code != 0 {
		return code
	}
	// Refresh the remote-tracking ref after the card checks; checking the earlier
	// fetched ref again would not detect the base moving while lint ran.
	if _, err := git("fetch", "--no-tags", "origin", "+refs/heads/"+base+":refs/remotes/origin/"+base); err != nil {
		return refuse(stderr, verb, "origin/"+base+" could not be refreshed after the forward-merge recheck; cards remain pending")
	}
	tip, err = git("rev-parse", "--verify", "--quiet", "--end-of-options", "refs/remotes/origin/"+base+"^{commit}")
	if err != nil || strings.TrimSpace(tip) != merge {
		return refuse(stderr, verb, "origin/"+base+" moved during the forward-merge recheck; cards remain pending; run the verb again")
	}
	snap, err = st.Load(ctx, []string{sprint.Work, sprint.Merge}, nil)
	if err != nil {
		return a.readFailed(verb, err, stderr)
	}
	briefs = pendingForwardMergeBriefs(snap, repo, base, merge)
	checks := make([]sprint.ForwardMergeCardCheck, 0, len(briefs))
	for _, b := range briefs {
		card := snap.Work.Card(b.id)
		if card == nil || card.Row != b.stream || card.F("brief") != b.brief {
			return refuse(stderr, verb, "card "+b.id+" changed during the forward-merge recheck; cards remain pending; run the verb again")
		}
		checks = append(checks, sprint.ForwardMergeCardCheck{ID: b.id, Stream: b.stream, BriefSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(b.brief)))})
	}
	req := sprint.ForwardMergeRecheckReq{Repo: repo, Base: base, Merge: merge, Who: c.actor, Behind: behind, Checks: checks}
	step := store.Step{Args: store.ArgsOf(req), Verb: "rebase recheck-forward-merge", Load: []string{sprint.Work, sprint.Merge}, Mirrors: true,
		Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.RecheckForwardMerge(s, req) }}
	return a.runStep("rebase recheck-forward-merge", c, st, step, stdout, stderr)
}

func pendingForwardMergeBriefs(s *sprint.Snapshot, repo, base, merge string) []briefCheck {
	var out []briefCheck
	for _, col := range []sprint.State{sprint.Waiting, sprint.Ready, sprint.Review} {
		for _, stream := range s.Streams() {
			for _, card := range s.Work.Cell(stream, col) {
				if card.F("kind") == "sentinel" || card.F(sprint.FieldForwardMerge) != merge || card.F(sprint.FieldForwardMergeChecked) == merge {
					continue
				}
				cardRepo, cardBase := sprint.CardRepoAndBase(s, card)
				if cardRepo == repo && cardBase == base {
					out = append(out, briefCheck{id: card.ID, stream: stream, brief: card.F("brief")})
				}
			}
		}
	}
	return out
}

func forwardMergeTip(s *sprint.Snapshot, repo, base string) string {
	if s == nil {
		return ""
	}
	for _, stream := range s.Streams() {
		ctl := s.StreamCtl(stream)
		if ctl != nil && ctl.F(sprint.FieldBaseLandedRepo) == repo && ctl.F(sprint.FieldBaseLandedBase) == base {
			return ctl.F(sprint.FieldBaseLandedTip)
		}
	}
	if tip, ok := s.Work.Prop(sprint.BaseLandedKey(repo, base)); ok {
		return tip
	}
	return ""
}
