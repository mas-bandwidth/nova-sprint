package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
	"github.com/mas-bandwidth/nova-tools/pkg/typedrec"
)

// continue is the coordinator's verb (docs/SPEC-SPRINT.md, the continue verb; the
// class in this file, as rebase's and needs' are).
func init() { verbClasses["continue"] = classCoordinator }

// cmdContinue is the continue verb (docs/SPEC-SPRINT.md, the continue verb): the
// coordinator continues a primary from the carried head — its brief edited in
// place with a CARRY: line at the newest full head an attempt pushed (or the
// --carry branch or sha), the continue note appended, its bound reset, its
// bound and brief-defect judgments answered, and with --tier its tier
// re-pinned. A branch --carry is resolved to its head through the lander's clone
// of the card's repository (one git ls-remote; --repo-dir for a card naming no
// REPO: line, refused without one); a sha is taken as given and must be 40 hex.
func (a *app) cmdContinue(args []string, stdout, stderr io.Writer) int {
	fs, c := a.verbSetup("continue")
	note := fs.String("note", "", "the continue note appended to the brief, with the UTC time")
	tier := fs.String("tier", "", "the tier to re-pin ("+cardhdr.RouteList+")")
	carry := fs.String("carry", "", "the branch or sha to carry: a branch is resolved to its head; a sha must be 40 hex")
	repoDir := fs.String("repo-dir", "", "a clone whose origin is asked for the carried branch, for a card whose brief names no REPO: line")
	ans := fs.String("answers", "", answersWords)
	pos, err := parse(fs, args)
	if err != nil {
		return refuse(stderr, "continue", err.Error())
	}
	if len(pos) != 1 || *note == "" {
		return refuse(stderr, "continue", "wants one primary and --note <text>")
	}
	if *tier != "" && !cardhdr.IsRoute(*tier) {
		return refuse(stderr, "continue", "--tier wants "+cardhdr.RouteList+", found "+oneline.Escape(*tier))
	}
	id := pos[0]
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, "continue", err.Error())
	}
	if err := unaliasFlag(context.Background(), st, fs, "answers"); err != nil {
		return refuse(stderr, "continue", "--answers: "+err.Error())
	}
	head := *carry
	if head != "" && !typedrec.IsFullSha(head) {
		// a branch: resolve it to its head through the lander's clone of the card's repository
		ctx := context.Background()
		load, err := st.Load(ctx, []string{sprint.Work}, nil)
		if err != nil {
			return a.readFailed("continue", err, stderr)
		}
		pr := load.Work.Placed(id)
		if pr == nil {
			return refuse(stderr, "continue", "no primary "+id+" on the work table; nothing was changed")
		}
		repo := swarm.ReadCardBase([]byte(pr.F("brief"))).Repo
		l := &lander{a: a, repoDir: *repoDir}
		var dir, why string
		if repo != "" {
			dir, why = l.clone(ctx, repo)
		} else if *repoDir != "" {
			dir = *repoDir
		} else {
			return refuse(stderr, "continue", id+" names no REPO: line and no --repo-dir was given; nothing was changed; run: nova-sprint continue "+id+" --carry "+oneline.Escape(head)+" --repo-dir <clone>")
		}
		if why != "" {
			return refuse(stderr, "continue", "the lander's clone of "+oneline.Escape(repo)+" could not be had: "+oneline.Escape(why)+"; nothing was changed")
		}
		out, err := l.git(ctx, dir, "ls-remote", "--heads", "origin", "refs/heads/"+head)
		if err != nil {
			return refuse(stderr, "continue", "origin "+oneline.Escape(repo)+" could not be asked for "+oneline.Escape(head)+": "+oneline.Escape(err.Error())+"; nothing was changed")
		}
		var sh string
		for _, line := range strings.Split(out, "\n") {
			if got, ref, ok := strings.Cut(line, "\t"); ok && strings.TrimSpace(ref) == "refs/heads/"+head {
				sh = strings.TrimSpace(got)
				break
			}
		}
		if sh == "" || !typedrec.IsFullSha(sh) {
			return refuse(stderr, "continue", "no branch "+oneline.Escape(head)+" on origin "+oneline.Escape(repo)+"; nothing was changed")
		}
		head = sh
	}
	req := sprint.ContinueReq{ID: id, Note: *note, Tier: *tier, Carry: head, Answers: answers(*ans), Who: c.actor}
	if c.dry {
		return a.continueDryRun(*c, st, req, stdout, stderr)
	}
	return a.runStep("continue", *c, st, continueStep(req), stdout, stderr)
}

// continueStep is continue's one store step (sprint.Continue): the brief read
// from the work table, edited in place with a CARRY: line at the carried head,
// the continue note appended, the bound reset, the bound and brief-defect
// judgments answered, and with --tier the tier re-pinned. It reads what a brief
// edit reads: the fleet's work cards for the pushed heads, the readers for a
// card in review, and the merge table for an orphan merge card.
func continueStep(r sprint.ContinueReq) store.Step {
	return store.Step{Answers: r.Answers, Named: true, Args: store.ArgsOf(r), Verb: "continue",
		Load: []string{sprint.Work, sprint.Readers, sprint.Fleet, sprint.Merge}, Mirrors: true,
		Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.Continue(s, r) }}
}

// continueDryRun is continue --dry-run: the step planned on one read, printing
// the new brief's header and the judgments it would answer, and writing nothing.
func (a *app) continueDryRun(c common, st *store.Store, req sprint.ContinueReq, stdout, stderr io.Writer) int {
	snap, err := st.Load(context.Background(), []string{sprint.Work, sprint.Readers, sprint.Fleet, sprint.Merge}, nil)
	if err != nil {
		return a.readFailed("continue", err, stderr)
	}
	p := sprint.Continue(snap, req)
	var lines []string
	for _, u := range p.Units {
		for _, ch := range u.Changes {
			if ch.Table == sprint.Work && ch.Entry.ID == req.ID {
				if b := ch.Entry.Set["brief"]; b != "" {
					lines = append(lines, "BRIEF HEADER:")
					for _, l := range strings.Split(b, "\n") {
						if l == "" {
							break
						}
						lines = append(lines, "  "+l)
					}
				}
			}
		}
		for _, o := range u.Closes {
			lines = append(lines, "WOULD ANSWER "+o.Note.ID+" ("+o.Note.Type+")")
		}
		if u.Moved != "" {
			lines = append(lines, "WOULD "+u.Moved)
		}
	}
	for _, r := range p.Refused {
		lines = append(lines, "REFUSED "+r.Key+": "+r.Why)
	}
	for i := range lines {
		lines[i] = oneline.Escape(lines[i])
	}
	code, status := 0, "ok"
	if len(p.Refused) > 0 {
		code, status = 1, "refused"
	}
	head := fmt.Sprintf("%s %s DRY-RUN changes=%d refused=%d; nothing was written", token("continue"), strings.ToUpper(status), len(p.Units), len(p.Refused))
	sayOK(stdout, c.json, "continue", strings.Join(append([]string{head}, lines...), "\n"),
		map[string]any{"status": status, "exit": code, "dry_run": true, "changes": len(p.Units), "refused": p.Refused, "lines": lines})
	return code
}
