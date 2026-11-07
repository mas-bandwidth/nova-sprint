package policy

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// Report is what check found: every answer that would differ from the
// active version's on the current snapshot, by question and subject, then
// every refusal. Code is 0 when nothing is refused, 1 otherwise.
type Report struct {
	Label    string
	Changes  []string
	Refusals []string
}

// Text is the report as check prints it.
func (r Report) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "policy check %s: %d answer(s) change, %d refused\n", r.Label, len(r.Changes), len(r.Refusals))
	for _, c := range r.Changes {
		b.WriteString("  " + c + "\n")
	}
	for _, f := range r.Refusals {
		b.WriteString(f + "\n")
	}
	return b.String()
}

// Code is the exit status: 0 when nothing is refused, 1 otherwise.
func (r Report) Code() int { return min(len(r.Refusals), 1) }

// Check runs a file, a stored version (7 or v7) or default on the current
// snapshot against the active version. Read-only. A file's refusals name
// the file (it has no version yet); a stored version's name the version.
// ro is the roster the snapshot is read with (AnswerContext).
func Check(ctx context.Context, st store.Store, ro roster.Roster, e *Engine, target string) (Report, error) {
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return Report{}, err
	}
	if n, ok := versionArg(target); ok {
		pv := store.PolicyVersion{Body: defaultBody}
		if n > 0 {
			if pv, err = script(ctx, st, "check", n); err != nil {
				return Report{}, err
			}
		}
		return check(ctx, st, ro, e, snap, pv)
	}
	name, body, r := readFile("check", target)
	if r != nil {
		return Report{}, r
	}
	a, fs := e.checkRun(ctx, source{label: name, body: body}, snap, ro)
	return e.report(ctx, st, snap, ro, name, a, fs)
}

// versionArg reads 7, v7 or default (0).
func versionArg(s string) (int, bool) {
	if s == "default" {
		return 0, true
	}
	n, err := strconv.Atoi(strings.TrimPrefix(s, "v"))
	return n, err == nil && n > 0
}

func check(ctx context.Context, st store.Store, ro roster.Roster, e *Engine, snap store.Snapshot, pv store.PolicyVersion) (Report, error) {
	a, fs := e.checkRun(ctx, storedSource(pv.Version, pv.Body), snap, ro)
	return e.report(ctx, st, snap, ro, Label(pv.Version), a, fs)
}

// runFile runs an unstored file (never cached) on snap, labelled by its name.
func (e *Engine) runFile(name, body string, snap store.Snapshot, ro roster.Roster) (Answers, []Failure) {
	return e.checkRun(context.Background(), source{label: name, body: body}, snap, ro)
}

// checkRun runs src on every question snap poses, under a tick's deadline,
// collecting every failure and the answers' text; nothing is cached.
func (e *Engine) checkRun(ctx context.Context, src source, snap store.Snapshot, ro roster.Roster) (Answers, []Failure) {
	if e.proc != nil {
		return e.remote(ctx, e.clock.Now(), request{Kind: "check", Src: src.wire(), Snap: snap, Roster: ro}, []source{src})
	}
	ctx, cancel := e.deadline(ctx, e.clock.Now())
	defer cancel()
	asks := newIndex(snap, ro).asks(ctx, bothRungs(FullQuestions(snap, nil)))
	o := e.start(ctx, src, asks, true).await(nil, func() {})
	return o.a, o.fs
}

// bothRungs is q with landing asked on both rungs for every stream, flowing
// then backed up: check predicts a tick on either (cold read 3's probe).
func bothRungs(q Questions) Questions {
	var out []LandingQuestion
	for _, lq := range q.Landing {
		out = append(out, LandingQuestion{lq.Stream, RungFlowing}, LandingQuestion{lq.Stream, RungBackedUp})
	}
	q.Landing = out
	return q
}

// report compares a run's answers with the active version's on snap.
func (e *Engine) report(ctx context.Context, st store.Store, snap store.Snapshot, ro roster.Roster, label string, a Answers, fs []Failure) (Report, error) {
	r := Report{Label: label}
	for _, f := range fs {
		r.Refusals = append(r.Refusals, f.Error())
	}
	active := store.PolicyVersion{Version: 0, Kind: "script", Body: defaultBody}
	if snap.Policy > 0 {
		var err error
		if active, err = st.Policy(ctx, snap.Policy); err != nil {
			return r, err
		}
	}
	was, _ := e.checkRun(ctx, storedSource(active.Version, active.Body), snap, ro) // ignored: the active's own refusals are not this report's
	for _, k := range slices.Sorted(maps.Keys(a.shown)) {
		if old, ok := was.shown[k]; !ok || old != a.shown[k] {
			r.Changes = append(r.Changes, fmt.Sprintf("%s: %s -> %s", k, cmp.Or(old, "(refused)"), a.shown[k]))
		}
	}
	return r, nil
}

// Show prints version v (0 is the active one): its actor, time, reason and body.
func Show(ctx context.Context, st store.Store, v int) (string, error) {
	if v == 0 {
		snap, err := st.Snapshot(ctx)
		if err != nil || snap.Policy == 0 {
			return "", upFirst("show", err)
		}
		v = snap.Policy
	}
	pv, err := version(ctx, st, "show", v)
	if err != nil {
		return "", err
	}
	reasons, err := reasons(ctx, st)
	if err != nil {
		return "", err
	}
	return line(pv, reasons[v]) + "\n\n" + pv.Body, nil
}

// List prints every version, newest first, the active one marked.
func List(ctx context.Context, st store.Store) (string, error) {
	all, err := versions(ctx, st)
	if err != nil {
		return "", err
	}
	reasons, err := reasons(ctx, st)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, pv := range slices.Backward(all) {
		b.WriteString(line(pv, reasons[pv.Version]) + "\n")
	}
	return b.String(), nil
}

func line(pv store.PolicyVersion, reason string) string {
	mark := " "
	if pv.Active {
		mark = "*"
	}
	return fmt.Sprintf("%s v%d %s %s by %s at %s: %s", mark, pv.Version, pv.Kind, pv.Name, pv.Actor, pv.At.Format("2006-01-02T15:04:05Z"), reason)
}

// reasons are each version's reason, from the note of the event that stored it.
func reasons(ctx context.Context, st store.Store) (map[int]string, error) {
	out := map[int]string{}
	return out, walk(ctx, st, 0, func(e store.Event) {
		if !e.Accepted || e.After == nil {
			return
		}
		for v := range e.After.Policies {
			if e.Before == nil || e.Before.Policies[v].Version == 0 {
				out[v] = e.Receipt.Note
			}
		}
	})
}
