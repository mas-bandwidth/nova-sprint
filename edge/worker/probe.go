package worker

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// ProbeRow proves, before the row takes a card, that its harness starts
// inside the wall and answers with each model the row would run: of the
// policy's candidates for each tier (next --json's tier_models), the first
// the harness serves. A take runs only a model the probe proved. Each run's
// stdout.log, stderr.log and command.txt are kept under
// <root>/probes/<row>/<model>. A failure opens the row's probe judgement and
// is the worker's refusal: the cause, the harness's known limits, and where
// its output is.
func (r *Runner) ProbeRow(ctx context.Context, row, harnessName string) error {
	h, err := r.harnessFor_internal(harnessName)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.probed[row] = false
	r.mu.Unlock()
	models, tiers, err := r.probeModels_internal(ctx, row, h)
	for _, m := range models {
		if err != nil {
			break
		}
		err = r.probeModel_internal(ctx, row, h, m)
	}
	if err != nil {
		return r.probeRefusal_internal(ctx, row, err)
	}
	r.mu.Lock()
	r.probed[row] = true
	r.tiers[row] = store.JoinTiers(tiers)
	for _, m := range models {
		r.served[m] = true
	}
	r.mu.Unlock()
	return nil
}

// probeRefusal_internal opens the row's probe judgement and is the refusal;
// a judgement that cannot be opened is said in it, never dropped.
func (r *Runner) probeRefusal_internal(ctx context.Context, row string, cause error) error {
	line := cause.Error()
	judged := r.Sprint.OpenJudgement(ctx, store.Judgement{
		ID: "j-probe-" + row, Kind: "probe", About: row, Group: "probe:" + row,
		Question: fmt.Sprintf("harness probe failed on %s: %s", row, edge.RedactN(line, edge.FailureMax)),
		Command:  fmt.Sprintf("nova-sprint judge j-probe-%s retry --reason fixed", row),
	})
	if judged != nil {
		line += " (and its judgement could not be opened: " + edge.RedactN(judged.Error(), edge.FailureMax) + ")"
	}
	return fmt.Errorf("worker refused: probe failed for row %s: %s", row, line)
}

// probeModels_internal are the models to probe and the tiers they serve:
// every route's key present first, then for a harness given a model, of
// each tier's candidates the first it serves. A tier with no candidate is a
// refusal; a tier whose candidates it serves none of is a warning and not
// among the tiers (the worker advertises the rest, defect 74), and none
// served at all a refusal naming each tier's reasons. A harness given no
// model serves every tier.
func (r *Runner) probeModels_internal(ctx context.Context, row string, h Harness) ([]string, []string, error) {
	if missing := r.Proxy.MissingKeys(); len(missing) > 0 {
		return nil, nil, errors.New(strings.Join(missing, "; "))
	}
	if !h.Needs().Model {
		return []string{""}, store.Tiers, nil
	}
	next, err := r.Sprint.Next(ctx, row)
	if err != nil {
		return nil, nil, fmt.Errorf("the row's models could not be read (nova-sprint next --as %s --json): %w", row, err)
	}
	if len(next.TierModels) == 0 {
		return nil, nil, fmt.Errorf("no model for any tier: next --json names no tier for %s (roster: %s): check that nova-config gives this machine the bench role (nova-sprint check)", row, cmp.Or(next.RosterErr, "read"))
	}
	listed, err := r.listModels_internal(ctx, row, h)
	if err != nil {
		return nil, nil, err
	}
	var chosen, tiers, unserved []string
	for _, tier := range slices.Sorted(maps.Keys(next.TierModels)) {
		cands := next.TierModels[tier]
		if len(cands) == 0 {
			return nil, nil, fmt.Errorf("no model for tier %s: the policy's catalog names none: add one (nova-sprint policy set <file> --reason <why>)", tier)
		}
		m, why := r.servedModel_internal(h.Needs(), cands, listed)
		if m == "" {
			unserved = append(unserved, "tier "+tier+": "+why)
			continue
		}
		tiers = append(tiers, tier)
		if !slices.Contains(chosen, m) {
			chosen = append(chosen, m)
		}
	}
	if len(chosen) == 0 {
		return nil, nil, fmt.Errorf("this bench's %s serves none of the policy's models: %s", h.Name(), strings.Join(unserved, "; "))
	}
	for _, u := range unserved {
		r.report("tier not served on "+row, errors.New(u+": this row does not advertise it, so no card of that tier is dealt here"))
	}
	return chosen, tiers, nil
}

// servedModel_internal is the first of cands the harness serves (a route
// for its provider through the proxy, its own provider without, and in its
// model list when it has one), or "" and why each is not.
func (r *Runner) servedModel_internal(needs Needs, cands, listed []string) (string, string) {
	var why []string
	for _, m := range cands {
		provider, _, _ := strings.Cut(m, "/")
		switch {
		case needs.Proxy && !r.Proxy.HasRoute(provider):
			why = append(why, m+": no key-proxy route for "+provider+" (--providers "+provider+"=<url>@<KEY>)")
		case !needs.Proxy && needs.Provider != "" && provider != needs.Provider:
			why = append(why, m+": the harness serves "+needs.Provider+" only")
		case listed != nil && !slices.Contains(listed, m):
			why = append(why, m+": not in the harness's model list")
		default:
			return m, ""
		}
	}
	if listed != nil {
		var same []string
		for _, l := range listed {
			if p, _, _ := strings.Cut(l, "/"); slices.ContainsFunc(cands, func(c string) bool { return strings.HasPrefix(c, p+"/") }) {
				same = append(same, l)
			}
		}
		list := cmp.Or(strings.Join(same, " "), "none of those providers' models")
		if len(list) > 400 {
			list = list[:400] + "..."
		}
		why = append(why, "the harness lists "+list)
	}
	return "", strings.Join(why, ", ")
}

// listModels_internal is the harness's own model list (its Needs'
// ListModels run outside the wall, under the probe's home), nil when it has
// no such command.
func (r *Runner) listModels_internal(ctx context.Context, row string, h Harness) ([]string, error) {
	argv := h.Needs().ListModels
	if argv == nil {
		return nil, nil
	}
	dir := filepath.Join(r.RootDir, "probes", row)
	for _, sub := range []string{"home", "tmp", "data"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("the probe directory: %w", err)
		}
	}
	_, env := h.Command(Job{JobDir: dir, Row: row, Consumer: row + ".models"})
	cmd := edge.Cmd{Argv: argv, Env: env, CleanEnv: true, Dir: dir, Timeout: r.Timeouts.LaunchGrace}
	res, err := cmp.Or[edge.Exec](r.Exec, edge.NewOSExec()).Run(ctx, cmd)
	if err != nil || res.ExitCode != 0 {
		return nil, fmt.Errorf("the harness could not list its models: %w: fix the harness's install on this bench", edge.Failure(argv, nil, res.ExitCode, res.Stdout, res.Stderr, err))
	}
	var out []string
	for _, l := range strings.Split(edge.StripANSI(res.Stdout), "\n") {
		if f := strings.Fields(l); len(f) > 0 && strings.Contains(f[0], "/") {
			out = append(out, f[0])
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the harness listed no models (%s printed no provider/model line): fix the harness's install on this bench", strings.Join(argv, " "))
	}
	return out, nil
}

// probeModel_internal is one probe run inside the wall with model.
func (r *Runner) probeModel_internal(ctx context.Context, row string, h Harness, model string) error {
	dir := filepath.Join(r.RootDir, "probes", row, cmp.Or(strings.ReplaceAll(model, "/", "_"), "default"))
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("the probe directory %s could not be cleared: %w", dir, err)
	}
	for _, sub := range []string{"repo", "home", "tmp", "data"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return fmt.Errorf("the probe directory: %w", err)
		}
	}
	pj := h.Probe(Job{Consumer: row + ".probe", Row: row, Kind: "work", Model: model, HarnessName: h.Name(), ProxyAddr: r.ProxyAddr})
	pj.JobDir, pj.WorktreeDir, pj.Needs = dir, filepath.Join(dir, "repo"), h.Needs()
	argv, env := h.Command(pj)
	res, runErr := r.Wall.RunHarness(ctx, pj, argv, env, r.Timeouts.LaunchGrace)
	r.report("probe logs for "+row, keepLogs_internal(dir, res))
	pj.ProxyStatus = cmp.Or(pj.ProxyStatus, r.Proxy.ConsumeStatus())
	out := ClassifyJob(h, pj, res.ExitCode, res.Elapsed, r.Timeouts.LaunchGrace, res.Output)
	if runErr == nil && out.Result == "ok" {
		r.logStep_internal(row, pj.Consumer, "probe", "ok "+cmp.Or(model, "default"), res.Elapsed)
		return nil
	}
	return probeFailure_internal(pj, res, runErr, out)
}

// probeFailure_internal is a failed probe run in one line: the harness's
// cause line (escapes stripped), a death inside the wall before its log
// with the command that shows the wall, the proxy's provider status, the
// harness's known limits, and where its output is kept.
func probeFailure_internal(pj Job, res WallRunResult, runErr error, out Outcome) error {
	cause := cmp.Or(edge.CauseLine(string(res.Stderr), edge.FailureMax), edge.CauseLine(string(res.Output), edge.FailureMax), edge.RedactN(out.Reason, edge.FailureMax))
	if runErr != nil {
		cause = edge.RedactN(runErr.Error(), edge.FailureMax) + ": " + cause
	}
	msg := fmt.Sprintf("%s with %s: exit %d: %s", pj.HarnessName, cmp.Or(pj.Model, "its default model"), res.ExitCode, cause)
	if diedEarly_internal(pj, res) {
		msg += "; the harness died inside the wall before writing a log; " + SandboxShow(res.Argv)
	}
	if pj.ProxyStatus != 0 {
		msg += fmt.Sprintf("; the key proxy saw HTTP %d from the provider", pj.ProxyStatus)
	}
	if pj.Needs.Limits != "" {
		msg += "; known limits of " + pj.HarnessName + ": " + pj.Needs.Limits
	}
	return fmt.Errorf("%s; its output is kept in %s (stdout.log, stderr.log, command.txt): fix the harness or its credentials and run the worker again", msg, pj.JobDir)
}

// diedEarly_internal: the harness exited non-zero in its first second and
// wrote no log (its declared log directory empty; with none, no output but
// the wall's own WALL line).
func diedEarly_internal(pj Job, res WallRunResult) bool {
	if res.ExitCode == 0 || res.Elapsed >= time.Second {
		return false
	}
	if pj.Needs.LogDir != "" {
		entries, err := os.ReadDir(filepath.Join(pj.JobDir, pj.Needs.LogDir))
		return err != nil || len(entries) == 0
	}
	for _, l := range strings.Split(edge.StripANSI(string(res.Output)), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "WALL") {
			return false
		}
	}
	return true
}

// keepLogs_internal keeps a run's stdout.log, stderr.log and command.txt
// (the wall's command line) in its job directory; a result with only the
// combined output keeps that as stdout.log.
func keepLogs_internal(dir string, res WallRunResult) error {
	stdout := res.Stdout
	if stdout == nil && res.Stderr == nil {
		stdout = res.Output
	}
	quoted := make([]string, len(res.Argv))
	for i, a := range res.Argv {
		if a == "" || strings.ContainsAny(a, " \t\n\"'\\$") {
			a = strconv.Quote(a)
		}
		quoted[i] = a
	}
	return errors.Join(
		os.WriteFile(filepath.Join(dir, "stdout.log"), stdout, 0o600),
		os.WriteFile(filepath.Join(dir, "stderr.log"), res.Stderr, 0o600),
		os.WriteFile(filepath.Join(dir, "command.txt"), []byte(strings.Join(quoted, " ")+"\n"), 0o600))
}
