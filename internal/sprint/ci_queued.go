package sprint

import (
	"fmt"
	"time"
)

// A CI job queued with no runner is a judgment (docs/SPEC-SPRINT.md, "Drift
// alarms", beside the base's CI after a landing). The owner, 2026-10-13: "we need
// to catch these CI failures much much much earlier". Every rocketnet CI job
// needing a self-hosted runner sat QUEUED for about three hours while the only
// machine carrying the labels was down, and nothing alarmed; it was found by
// hand. The design follows TickBaseCI (base_ci.go): the binding reads the facts
// outside the tick, the tick raises one judgment per (Repo, Job) queued past the
// bound while it names no runner, and closes it when the job is no longer in the
// facts. The episode machine is the pass's, tla/CoordinatorPass.tla, as the drift
// alarms' and the base CI after a landing's are: no module of its own.
const (
	// NCIQueuedNoRunner is the judgment that a CI job has sat queued past the
	// bound with no runner online to take it.
	NCIQueuedNoRunner = "CI job queued with no runner"
	// PropCIQueuedBound is the work table's property: the CI-queued bound, a
	// duration; none set is CIQueuedBound, the default.
	PropCIQueuedBound = "ci_queued_bound"
)

// CIQueuedBound is how long, in running time, a CI job may sit queued before the
// judgment is raised, and the default. Ten minutes is long enough for a busy
// runner to pick a job up and short enough to be "much much much earlier" than
// the hours the owner lost.
const CIQueuedBound = 10 * time.Minute

// CIQueuedFacts is one CI job queued at a forge, read by the binding: the
// repository and run it is of, the job's name, the runs-on labels it wants, when
// it was created queued, and whether a runner carrying its labels is online
// ("yes", "no", or "unknown" when the runners read was refused). Repo or Job
// empty is no fact this tick, and the judgment it would judge is neither raised
// nor closed.
type CIQueuedFacts struct {
	Repo         string    // the repository, owner/name
	RunID        string    // the workflow run's id
	Run          string    // the workflow run's name or url, as the fact names it
	Job          string    // the job's name
	Labels       []string  // the runs-on labels the job wants
	QueuedSince  time.Time // when the job was created queued
	RunnerOnline string    // "yes", "no" or "unknown"
}

// CIQueuedRepos is every repository the live streams' cards name: each stream's
// control card repository (FieldRepo), first seen once, in stream order. The
// binding's read asks the forge for these repositories' queued jobs.
func CIQueuedRepos(s *Snapshot) []string {
	seen := map[string]bool{}
	var out []string
	for _, st := range s.Streams() {
		ctl := s.StreamCtl(st)
		if ctl == nil {
			continue
		}
		if repo := s.cardRepo(ctl); repo != "" && !seen[repo] {
			seen[repo] = true
			out = append(out, repo)
		}
	}
	return out
}

// ciQueuedBound is the CI-queued bound: the sprint's setting (PropCIQueuedBound),
// else CIQueuedBound.
func (s *Snapshot) ciQueuedBound() time.Duration {
	if s.Work != nil {
		if v, ok := s.Work.Prop(PropCIQueuedBound); ok {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				return d
			}
		}
	}
	return CIQueuedBound
}

// TickCIQueued is the tick's queued-CI judgment, planned in the deadlines part on
// the facts the binding read (TickReq.CIQueued; docs/SPEC-SPRINT.md, "Drift
// alarms"), as TickBaseCI is on TickReq.BaseCI: one judgment per (Repo, Job)
// queued longer than the bound, closed when a fact no longer names that (Repo,
// Job). A tick whose facts name the job nowhere (including a read that returned
// none) closes its judgment: the job is no longer queued. No raise again: the
// coordinator's pass reminds of open judgments as it does for any.
func TickCIQueued(s *Snapshot, r TickReq) (Plan, int) {
	var p Plan
	var conds []cond
	bound := s.ciQueuedBound()
	for _, f := range r.CIQueued {
		if f.Repo == "" || f.Job == "" {
			continue
		}
		sub := f.Repo + "@" + f.Job
		if f.QueuedSince.IsZero() || s.Now.Sub(f.QueuedSince) <= bound {
			continue // not past the bound: no condition, and notify closes any open judgment
		}
		conds = append(conds, cond{typ: NCIQueuedNoRunner, primaries: []string{sub}, decisions: TickDecisions[NCIQueuedNoRunner],
			what: fmt.Sprintf("%s run %s job %s has been queued for %s (since %s), runs-on %s, runner %s: no runner is taking it; check the runners on those labels",
				f.Repo, f.Run, f.Job, s.Now.Sub(f.QueuedSince).Round(time.Second), f.QueuedSince.UTC().Format(time.RFC3339), Preview(f.Labels, ","), f.RunnerOnline)})
	}
	due := notify(&p, s, conds, []string{NCIQueuedNoRunner}, r)
	return p, due
}
