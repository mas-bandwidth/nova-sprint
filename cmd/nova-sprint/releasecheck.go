package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/gitrun"
	"github.com/mas-bandwidth/nova-sprint/internal/oneline"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// release check (docs/SPEC-SPRINT.md, release-check-cold-audit-r-ns-b.w1): a read
// that runs the release checks (sprint.ReleaseChecks) and writes nothing; with
// --audit, the coordinator's, it asks the cold audit's reads and records it.
func init() {
	verbClasses["release check"] = classRead
}

// releaseCheckID is the sub-verb's word: no card is added with it as its id, so
// `release check` and `release <id>` never name the same thing.
const releaseCheckID = "check"

// reservedCardID is why a card may not be added with one of these ids: "" is may.
func reservedCardID(ids ...string) string {
	for _, id := range ids {
		if id == releaseCheckID {
			return "a card cannot be called check: release check is the release gate's verb, and release <id> would name both; give the card another id"
		}
	}
	return ""
}

// storeRelease is the release check's facts, read from the store once.
type storeRelease struct {
	now  time.Time
	snap *sprint.Snapshot
}

func (s storeRelease) Now() time.Time             { return s.now }
func (s storeRelease) Snapshot() *sprint.Snapshot { return s.snap }

// auditSince is the point the audit samples after: --since (an RFC 3339 time,
// or a git tag or commit, read at its commit time in repoDir), else the last
// release tag reachable from base. Neither is a refusal: the audit never
// samples every card landed for want of a point.
func (a *app) auditSince(ctx context.Context, since, base, repoDir string) (string, time.Time, error) {
	if since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			return since, t, nil
		}
	} else {
		tag, err := gitrun.Output(ctx, gitrun.Options{C: repoDir, Env: a.gitEnv, OwnRepo: true}, "describe", "--tags", "--abbrev=0", base)
		if err != nil || strings.TrimSpace(tag) == "" {
			return "", time.Time{}, fmt.Errorf("no release tag is reachable from %s in %s; name the point the audit samples after: --since <tag|commit|RFC 3339 time>", base, repoDir)
		}
		since = strings.TrimSpace(tag)
	}
	out, err := gitrun.Output(ctx, gitrun.Options{C: repoDir, Env: a.gitEnv, OwnRepo: true}, "log", "-1", "--format=%cI", since)
	t, perr := time.Parse(time.RFC3339, strings.TrimSpace(out))
	if err != nil || perr != nil {
		return "", time.Time{}, fmt.Errorf("--since %s is not an RFC 3339 time, and git in %s names no commit by it; nothing was asked", oneline.Escape(since), repoDir)
	}
	return since, t, nil
}

// cmdReleaseCheck prints one RELEASE CHECK line per check, then RELEASE OK
// checks=<n> or RELEASE NOT READY failed=<n>; --json prints the one report
// object. With --audit it asks the cold audit and prints COLD AUDIT lines.
func (a *app) cmdReleaseCheck(args []string, stdout, stderr io.Writer) int {
	fs, c := a.verbSetup("release check")
	var names stringList
	fs.Var(&names, "check", "run only this check, by name (repeat for more; default every check): "+strings.Join(sprint.ReleaseCheckNames(), ", "))
	audit := fs.Bool("audit", false, fmt.Sprintf("the coordinator's: ask cold reads of %d cards landed since the last release, each of a reader up that never saw it, and record the audit", sprint.ColdAuditCards))
	since := fs.String("since", "", "with --audit: the point the sample is drawn after, an RFC 3339 time or a git tag or commit (default the last release tag reachable from --base)")
	base := fs.String("base", "HEAD", "with --audit: the ref whose last release tag is the default --since")
	seed := fs.Int64("seed", -1, "with --audit: the sample's seed, printed and recorded so the sample can be redrawn (default drawn at random)")
	repoDir := fs.String("repo-dir", ".", "with --audit: the clone git reads the release tag in")
	pos, err := parse(fs, args)
	if err != nil || len(pos) > 0 {
		return refuse(stderr, "release check", argErr("takes no words ", err, pos...))
	}
	for _, n := range names {
		if !slices.Contains(sprint.ReleaseCheckNames(), n) {
			return refuse(stderr, "release check", fmt.Sprintf("no release check named %s; the checks are %s", oneline.Escape(n), strings.Join(sprint.ReleaseCheckNames(), ", ")))
		}
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, "release check", err.Error())
	}
	ctx := context.Background()
	if *audit {
		return a.releaseAudit(ctx, st, *c, *since, *base, *repoDir, *seed, stdout, stderr)
	}
	s, err := st.Load(ctx, []string{sprint.Work, sprint.Readers}, sprint.ColdAuditExtras)
	if err != nil {
		return a.readFailed("release check", err, stderr)
	}
	rep, err := sprint.RunReleaseChecks(storeRelease{now: a.now(), snap: s}, names)
	if err != nil {
		return refuse(stderr, "release check", err.Error())
	}
	code := 0
	if !rep.Ready {
		code = 1
	}
	if c.json {
		b, _ := json.Marshal(rep)
		fmt.Fprintln(stdout, string(b))
		return code
	}
	for _, r := range rep.Results {
		fmt.Fprintln(stdout, oneline.Escape(r.Line()))
	}
	fmt.Fprintln(stdout, rep.Summary)
	return code
}

// releaseAudit is release check --audit: the coordinator's alone, one step that
// asks every read of the sample or none (sprint.PlanColdAudit).
func (a *app) releaseAudit(ctx context.Context, st *store.Store, c common, since, base, repoDir string, seed int64, stdout, stderr io.Writer) int {
	c.verb = "release check --audit"
	if why, err := coordinatorsAlone(ctx, st, c); err != nil || why != "" {
		if err != nil {
			return a.readFailed("release check", err, stderr)
		}
		return refuse(stderr, "release check", why)
	}
	name, at, err := a.auditSince(ctx, since, base, repoDir)
	if err != nil {
		return refuse(stderr, "release check", err.Error())
	}
	if seed < 0 {
		seed = rand.Int63()
	}
	req := sprint.ColdAuditReq{Seed: seed, Since: name, SinceTime: at, Who: c.actor}
	var rec sprint.ColdAuditRecord
	res, err := st.Run(ctx, store.Step{Verb: "release check --audit", Named: true, Actor: c.actor,
		Load: []string{sprint.Work, sprint.Readers, sprint.Merge, sprint.Fleet}, Readers: true, Routes: true,
		Extras: sprint.ColdAuditExtrasForAsk,
		Plan: func(s *sprint.Snapshot) sprint.Plan {
			p, r := sprint.PlanColdAudit(s, req)
			rec = r
			return p
		}})
	if err != nil {
		return a.readFailed("release check", err, stderr)
	}
	if len(res.Refused) > 0 {
		var why []string
		for _, r := range res.Refused {
			why = append(why, r.Key+": "+r.Why)
		}
		return refuse(stderr, "release check", fmt.Sprintf("the audit (seed %d, since %s) asked nothing: %s", seed, name, strings.Join(why, "; ")))
	}
	if c.json {
		b, _ := json.Marshal(rec)
		fmt.Fprintln(stdout, string(b))
		return 0
	}
	for _, r := range rec.Reads {
		fmt.Fprintln(stdout, oneline.Escape("COLD AUDIT READ "+r.Card+" "+r.Read))
	}
	fmt.Fprintln(stdout, oneline.Escape(fmt.Sprintf("COLD AUDIT seed=%d since=%s cards=%d asked=%s", rec.Seed, name, len(rec.Reads), rec.Asked.UTC().Format(time.RFC3339))))
	return 0
}
