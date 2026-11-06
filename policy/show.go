package policy

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

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
// snapshot against the active version. Read-only.
func Check(ctx context.Context, st store.Store, e *Engine, target string) (Report, error) {
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return Report{}, err
	}
	if n, ok := versionArg(target); ok {
		pv := store.PolicyVersion{Version: 0, Kind: "script", Body: defaultBody}
		if n > 0 {
			if pv, err = version(ctx, st, "check", n); err != nil {
				return Report{}, err
			}
		}
		return check(ctx, st, e, snap, pv)
	}
	name, body, r := readFile("check", target)
	if r != nil {
		return Report{}, r
	}
	a, fs := e.runFile(name, body, snap)
	return e.report(ctx, st, snap, name, a, fs)
}

// versionArg reads 7, v7 or default (0).
func versionArg(s string) (int, bool) {
	if s == "default" {
		return 0, true
	}
	n, err := strconv.Atoi(strings.TrimPrefix(s, "v"))
	return n, err == nil && n > 0
}

func check(ctx context.Context, st store.Store, e *Engine, snap store.Snapshot, pv store.PolicyVersion) (Report, error) {
	p, f := e.program(pv)
	if f != nil {
		return Report{Label: Label(pv.Version), Refusals: []string{f.Error()}}, nil
	}
	a, fs := e.run(p, newIndex(snap).asks(FullQuestions(snap, nil)), true)
	return e.report(ctx, st, snap, Label(pv.Version), a, fs)
}

// runFile compiles an unstored file (never cached) and runs it on snap.
func (e *Engine) runFile(name, body string, snap store.Snapshot) (Answers, []Failure) {
	p, f := compile(0, name, body)
	if f != nil {
		return Answers{}, []Failure{*f}
	}
	return e.run(p, newIndex(snap).asks(FullQuestions(snap, nil)), true)
}

// report compares a run's answers with the active version's on snap.
func (e *Engine) report(ctx context.Context, st store.Store, snap store.Snapshot, label string, a Answers, fs []Failure) (Report, error) {
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
	old := map[string]string{}
	if p, f := e.program(active); f == nil {
		was, _ := e.run(p, newIndex(snap).asks(FullQuestions(snap, nil)), true) // ignored: the active's own refusals are not this report's
		old = was.shown
	}
	for _, k := range sortedKeys(a.shown) {
		if was, ok := old[k]; !ok || was != a.shown[k] {
			r.Changes = append(r.Changes, fmt.Sprintf("%s: %s -> %s", k, cmp.Or(was, "(refused)"), a.shown[k]))
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
	return out, walk(ctx, st, func(e store.Event) {
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

// Diff is the text diff of two bodies: a and b are 7, v7 or default; b ""
// is the active version.
func Diff(ctx context.Context, st store.Store, a, b string) (string, error) {
	if b == "" {
		snap, err := st.Snapshot(ctx)
		if err != nil || snap.Policy == 0 {
			return "", upFirst("diff", err)
		}
		b = strconv.Itoa(snap.Policy)
	}
	var bodies [2]string
	for i, s := range []string{a, b} {
		n, ok := versionArg(s)
		switch {
		case !ok:
			return "", &Refusal{"diff", "unknown version " + s, "nova-sprint policy show --list"}
		case n == 0:
			bodies[i] = defaultBody
		default:
			pv, err := version(ctx, st, "diff", n)
			if err != nil {
				return "", err
			}
			bodies[i] = pv.Body
		}
	}
	return "--- " + a + "\n+++ " + b + "\n" + textDiff(bodies[0], bodies[1]), nil
}

// textDiff is a line diff by longest common subsequence: " " kept, "-" only
// in a, "+" only in b.
func textDiff(a, b string) string {
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")
	lcs := make([][]int, len(x)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(y)+1)
	}
	for i := len(x) - 1; i >= 0; i-- {
		for j := len(y) - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out strings.Builder
	i, j := 0, 0
	for i < len(x) || j < len(y) {
		switch {
		case i < len(x) && j < len(y) && x[i] == y[j]:
			out.WriteString("  " + x[i] + "\n")
			i, j = i+1, j+1
		case i < len(x) && (j == len(y) || lcs[i+1][j] >= lcs[i][j+1]):
			out.WriteString("- " + x[i] + "\n")
			i++
		default:
			out.WriteString("+ " + y[j] + "\n")
			j++
		}
	}
	return out.String()
}
