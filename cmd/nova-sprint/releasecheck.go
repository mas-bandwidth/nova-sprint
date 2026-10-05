package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/gitrun"
	"github.com/mas-bandwidth/nova-sprint/internal/oneline"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// release check is a read (docs/SPEC-SPRINT.md, the release check; docs/SPEC-RELEASE.md,
// "release check"): it runs the registry of release checks over the store's log and writes
// nothing. With --audit, the coordinator asks cold reads of 20 landed cards and records
// the audit in the store.
func init() {
	verbClasses["release check"] = classRead
	verbEffect["release check"] = "inspection: reads the store's log and the sprint's settings, writes nothing; with --audit: asks cold reads of 20 landed cards and records the audit in the store"
	verbExit["release check"] = "exit codes: 0 every check passed (RELEASE OK) or audit asked, 1 a check failed (RELEASE NOT READY; each RELEASE CHECK line names what to look at), 2 usage or a store that did not answer"
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
	now      time.Time
	lines    []sprint.Line
	dealtMax time.Duration
	snap     *sprint.Snapshot
}

func (s storeRelease) Now() time.Time             { return s.now }
func (s storeRelease) Log() []sprint.Line         { return s.lines }
func (s storeRelease) DealtMax() time.Duration    { return s.dealtMax }
func (s storeRelease) Snapshot() *sprint.Snapshot { return s.snap }

func (a *app) resolveSince(ctx context.Context, since, repoDir string) (string, time.Time, error) {
	if since != "" {
		if d, err := time.ParseDuration(since); err == nil {
			return since, a.now().Add(-d), nil
		}
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			return since, t, nil
		}
		if repoDir != "" {
			out, err := gitrun.Output(ctx, gitrun.Options{C: repoDir, Env: a.gitEnv, OwnRepo: true}, "log", "-1", "--format=%cI", since)
			if err == nil {
				if t, err := time.Parse(time.RFC3339, strings.TrimSpace(out)); err == nil {
					return since, t, nil
				}
			}
		}
		return since, time.Time{}, nil
	}
	if repoDir != "" {
		tag, err := gitrun.Output(ctx, gitrun.Options{C: repoDir, Env: a.gitEnv, OwnRepo: true}, "describe", "--tags", "--abbrev=0")
		if err == nil && strings.TrimSpace(tag) != "" {
			tag = strings.TrimSpace(tag)
			out, err := gitrun.Output(ctx, gitrun.Options{C: repoDir, Env: a.gitEnv, OwnRepo: true}, "log", "-1", "--format=%cI", tag)
			if err == nil {
				if t, err := time.Parse(time.RFC3339, strings.TrimSpace(out)); err == nil {
					return tag, t, nil
				}
			}
			return tag, time.Time{}, nil
		}
	}
	return "", time.Time{}, nil
}

// cmdReleaseCheck runs the release checks and prints one RELEASE CHECK line per check, then
// RELEASE OK checks=<n> or RELEASE NOT READY failed=<n>; --json prints the one report
// object. With --audit, the coordinator asks cold reads and records the audit.
// Exit 0 every check passed or audit asked, 1 one failed, 2 usage or a store that did not answer.
func (a *app) cmdReleaseCheck(args []string, stdout, stderr io.Writer) int {
	fs, c := a.verbSetup("release check")
	streams := fs.String("streams", "", "only the log of the streams this glob names (path.Match over the stream's name; default every stream)")
	var names stringList
	fs.Var(&names, "check", "run only this check, by name (repeat for more; default every check): "+strings.Join(sprint.ReleaseCheckNames(), ", "))
	audit := fs.Bool("audit", false, "ask cold reads of 20 cards landed since the last release and record the audit (coordinator only)")
	since := fs.String("since", "", "git tag, commit or time (RFC 3339 or duration) to sample cards landed since; default the base's last release tag")
	seed := fs.Int64("seed", 0, "seed for uniform random sampling (default random)")
	repoDir := fs.String("repo-dir", ".", "the git repository clone to inspect tags from")

	pos, err := parse(fs, args)
	if err != nil || len(pos) > 0 {
		return refuse(stderr, "release check", argErr("takes no words ", err, pos...))
	}
	for _, n := range names {
		if !slicesHas(sprint.ReleaseCheckNames(), n) {
			return refuse(stderr, "release check", fmt.Sprintf("no release check named %s; the checks are %s", oneline.Escape(n), strings.Join(sprint.ReleaseCheckNames(), ", ")))
		}
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, "release check", err.Error())
	}
	ctx := context.Background()

	if *audit {
		rc := *c
		rc.verb = "release check --audit"
		if rc.actor == "" {
			return refuse(stderr, "release check", "--audit asks the cold audit reads: --actor <name> is required (or NOVA_SPRINT_ACTOR); nothing was changed")
		}
		if why, err := coordinatorsAlone(ctx, st, rc); err != nil || why != "" {
			if err != nil {
				return a.readFailed("release check", err, stderr)
			}
			return refuse(stderr, "release check", why)
		}

		sinceTag, sinceTime, err := a.resolveSince(ctx, *since, *repoDir)
		if err != nil {
			return refuse(stderr, "release check", err.Error())
		}

		s := *seed
		if s == 0 {
			s = rand.Int63()
			if s == 0 {
				s = 1
			}
		}

		req := sprint.ColdAuditReq{
			Seed:      s,
			SinceTag:  sinceTag,
			SinceTime: sinceTime,
			Who:       c.actor,
		}

		var plannedRec sprint.ColdAuditRecord
		var planErr error
		step := store.Step{
			Verb:  "release check --audit",
			Load:  []string{sprint.Work, sprint.Readers, sprint.Fleet},
			Actor: c.actor,
			Plan: func(snap *sprint.Snapshot) sprint.Plan {
				p, rec, perr := sprint.PlanColdAudit(snap, req)
				if perr != nil {
					planErr = perr
					return sprint.Plan{Refused: []sprint.Refusal{{Why: perr.Error()}}}
				}
				plannedRec = rec
				return p
			},
		}

		res, err := st.Run(ctx, step)
		if err != nil {
			return a.readFailed("release check", err, stderr)
		}
		if planErr != nil {
			return refuse(stderr, "release check", planErr.Error())
		}
		if len(res.Refused) > 0 {
			return refuse(stderr, "release check", res.Refused[0].Why)
		}

		timeStr := plannedRec.Asked.UTC().Format(time.RFC3339)
		line := fmt.Sprintf("COLD AUDIT seed=%d cards=%d asked=%s", plannedRec.Seed, len(plannedRec.IDs), timeStr)
		sayOK(stdout, c.json, "release check", line, map[string]any{
			"seed":  plannedRec.Seed,
			"cards": plannedRec.IDs,
			"asked": timeStr,
			"count": len(plannedRec.IDs),
		})
		return 0
	}

	es, err := st.EpochNow(ctx)
	if err != nil {
		return a.readFailed("release check", err, stderr)
	}
	lines, err := st.Log(ctx)
	if err != nil {
		return a.readFailed("release check", err, stderr)
	}
	now := a.now()
	from := now.Add(-sprint.StuckWindow)
	if lines, err = sprint.ReleaseStreamLines(lines, *streams); err != nil {
		return refuse(stderr, "release check", err.Error())
	}
	s, err := st.Load(ctx, []string{sprint.Work, sprint.Readers, sprint.Fleet}, nil)
	if err != nil {
		return a.readFailed("release check", err, stderr)
	}
	rep, err := sprint.RunReleaseChecks(storeRelease{now: now, lines: lines, dealtMax: s.DealtMax(), snap: s}, names)
	if err != nil {
		return refuse(stderr, "release check", err.Error())
	}
	if es.N > 0 && !es.Cleared.IsZero() && es.Cleared.After(from) {
		for i, r := range rep.Results {
			if r.Name == sprint.CheckNoStuckFriend && r.OK {
				rep.Results[i] = sprint.ReleaseResult{
					Name:     sprint.CheckNoStuckFriend,
					OK:       false,
					Evidence: fmt.Sprintf("coverage incomplete: sprint cleared at %s (%s ago), less than %s window", oneline.Escape(es.Cleared.UTC().Format(time.RFC3339)), now.Sub(es.Cleared).Truncate(time.Second), sprint.StuckWindow),
				}
				rep.Failed++
				rep.Ready = false
				rep.Summary = fmt.Sprintf("RELEASE NOT READY failed=%d", rep.Failed)
			}
		}
	}
	if c.json {
		b, _ := json.Marshal(rep)
		fmt.Fprintln(stdout, string(b))
		return rep.ExitCode()
	}
	for _, r := range rep.Results {
		fmt.Fprintln(stdout, oneline.Escape(r.Line()))
	}
	fmt.Fprintln(stdout, rep.Summary)
	return rep.ExitCode()
}

func slicesHas(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
