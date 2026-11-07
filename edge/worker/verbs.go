package worker

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/store"
	"strconv"
	"strings"
	"time"
)

var (
	ErrStoreDown = errors.New("worker: store unreachable")
	ErrRefused   = errors.New("worker: sprint refused")
	// ErrReceipt marks a take receipt that lacks what the worker takes from
	// it (model, deadline, forge). It is fatal: the sprint is older than the
	// worker, and every further card would be refused the same way.
	ErrReceipt = errors.New("worker: take receipt refused")
)

type (
	ConsumerSummary struct {
		ID, Producer, Kind, Stream, State, Priority, Head string
		Gen, Attempt, Score                               int
	}
	NextResponse struct {
		Epoch, Width          int
		Seq                   int64
		Row, State            string
		Harness               string
		Held                  bool
		Ready, Working, Final []ConsumerSummary
		// TierModels are the models the row may run by tier it serves, in
		// provider/model form (a bench's: the policy's candidates).
		TierModels map[string][]string `json:"tier_models"`
		RosterErr  string              `json:"roster_error"`
	}
	TakeInfo struct {
		Consumer, Producer, Kind, Stream, Repo, Base, Branch, Head, Tier, Brief, Prose, Finding string
		StreamRef                                                                               string   `json:"stream_ref"`
		BaseSHA                                                                                 string   `json:"base_sha"`
		PriorHead                                                                               string   `json:"prior_head"`
		Paths                                                                                   []string `json:"paths"`
		DoneWhen                                                                                []string `json:"done_when"`
		Epoch, Gen, Attempt, Review                                                             int
		Model, Forge                                                                            string
		Models                                                                                  []string // a bench's candidates for the tier, when Model is ""
		Deadline                                                                                int      // seconds, from policy's timeouts().consumer[tier]
	}
	ReportRequest struct {
		Epoch, Gen                                             int
		Row, Consumer, ResultID, Result, Head, Reason, Finding string
		Cost                                                   int64
		Priced                                                 bool
	}
	SprintClient interface {
		Hook(ctx context.Context, row string) error
		Unhook(ctx context.Context, row string) error
		Next(ctx context.Context, row string) (NextResponse, error)
		Take(ctx context.Context, row, consumer string, gen int) (TakeInfo, error)
		Report(ctx context.Context, req ReportRequest) (store.Receipt, error)
		// Release gives the row's own card back for a redeal with no result:
		// neither ok nor fail, no fault and no bounce (`nova-sprint release`);
		// head, when not empty, is the partial head the worker pushed.
		Release(ctx context.Context, row, consumer string, gen int, head, reason string) error
		OpenJudgement(ctx context.Context, j store.Judgement) error
	}
	CLISprintClient struct {
		Exec      edge.Exec
		Timeout   time.Duration
		Run       func(ctx context.Context, argv []string) ([]byte, int, error)
		SprintBin string
		Epoch     int
	}
)

// argv_internal is a sprint verb's command line. The worker runs under the
// bench seat's nova-secrets exec, which put the sprint's credentials in its
// environment; each verb is run directly and inherits them (defect 35: a
// bare nova-secrets exec is refused by nova-tools v2, which wants --only,
// --store and --key).
func (c *CLISprintClient) argv_internal(args ...string) []string {
	return append([]string{cmp.Or(c.SprintBin, "nova-sprint")}, args...)
}

// run_internal runs argv for its output (stdout then stderr, as the parsers
// read it); a failure is an edge.CommandError naming the command, its exit
// code and its output's first lines.
func (c *CLISprintClient) run_internal(ctx context.Context, argv []string) ([]byte, int, error) {
	if c.Run != nil {
		out, code, err := c.Run(ctx, argv)
		if err != nil || code != 0 {
			return out, code, edge.Failure(argv, nil, code, string(out), "", err)
		}
		return out, code, nil
	}
	res, err := cmp.Or[edge.Exec](c.Exec, edge.NewOSExec()).Run(ctx, edge.Cmd{Argv: argv, Timeout: c.Timeout})
	if err != nil || res.ExitCode != 0 {
		return []byte(res.Stdout + res.Stderr), res.ExitCode, edge.Failure(argv, nil, res.ExitCode, res.Stdout, res.Stderr, err)
	}
	return []byte(res.Stdout + res.Stderr), res.ExitCode, nil
}

// classifyCLIError_internal is a sprint verb's failure: exit 1 is the
// sprint's refusal, in its own words (its one line, redacted), after the
// command; anything else the store or the run being down, with the
// command's failure.
func (c *CLISprintClient) classifyCLIError_internal(out []byte, code int, err error) error {
	if code == 0 && err == nil {
		return nil
	}
	var ce *edge.CommandError
	if code == 1 && errors.As(err, &ce) {
		return fmt.Errorf("%w: %s: exit 1: %s", ErrRefused, ce.Command, edge.RedactN(TailBytes(out, 512), 512))
	}
	if code == 1 {
		return fmt.Errorf("%w: %s", ErrRefused, edge.RedactN(TailBytes(out, 512), 512))
	}
	return fmt.Errorf("%w: %w", ErrStoreDown, err)
}
func (c *CLISprintClient) Hook(ctx context.Context, row string) error {
	out, code, err := c.run_internal(ctx, c.argv_internal("hook", "--as", row, "--role", "worker"))
	return c.classifyCLIError_internal(out, code, err)
}
func (c *CLISprintClient) Unhook(ctx context.Context, row string) error {
	out, code, err := c.run_internal(ctx, c.argv_internal("unhook", "--as", row))
	return c.classifyCLIError_internal(out, code, err)
}
func (c *CLISprintClient) Next(ctx context.Context, row string) (NextResponse, error) {
	out, code, err := c.run_internal(ctx, c.argv_internal("next", "--as", row, "--json"))
	var resp NextResponse
	if cerr := c.classifyCLIError_internal(out, code, err); cerr != nil {
		return resp, cerr
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return resp, fmt.Errorf("nova-sprint next --json: %s", edge.ParseFailure(err, out))
	}
	c.Epoch = cmp.Or(resp.Epoch, c.Epoch)
	return resp, nil
}
func (c *CLISprintClient) Take(ctx context.Context, row, consumer string, gen int) (TakeInfo, error) {
	ep := cmp.Or(c.Epoch, 1)
	id := FormatTakeOpID(ep, consumer, gen, row)
	out, code, err := c.run_internal(ctx, c.argv_internal("take", fmt.Sprintf("%s@%d", consumer, gen), "--id", id, "--epoch", strconv.Itoa(ep), "--as", row, "--json"))
	var info TakeInfo
	if cerr := c.classifyCLIError_internal(out, code, err); cerr != nil {
		return info, cerr
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return info, fmt.Errorf("nova-sprint take --json: %s", edge.ParseFailure(err, out))
	}
	// The forge may be "" (hand), so presence is read from the keys.
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(out, &keys); err != nil {
		return TakeInfo{}, fmt.Errorf("nova-sprint take --json: %s", edge.ParseFailure(err, out))
	}
	have := map[string]bool{}
	for k := range keys {
		have[strings.ToLower(k)] = true
	}
	var missing []string
	for _, k := range []string{"model", "deadline", "forge"} {
		if !have[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return TakeInfo{}, receiptError_internal(consumer, missing)
	}
	return info, nil
}

// checkReceipt_internal is what the runner needs from a receipt before it
// stages anything: a model (or a bench's candidates) and a deadline (a
// forge of "" means none).
func checkReceipt_internal(info TakeInfo) error {
	var missing []string
	if info.Model == "" && len(info.Models) == 0 {
		missing = append(missing, "model")
	}
	if info.Deadline <= 0 {
		missing = append(missing, "deadline")
	}
	if len(missing) > 0 {
		return receiptError_internal(info.Consumer, missing)
	}
	return nil
}
func receiptError_internal(consumer string, missing []string) error {
	return fmt.Errorf("%w: the receipt for %s has no %s: update nova-sprint, then check that `nova-sprint take <card>@<gen> --as <row> --json` prints model, deadline and forge", ErrReceipt, consumer, strings.Join(missing, ", "))
}
func (c *CLISprintClient) Report(ctx context.Context, req ReportRequest) (store.Receipt, error) {
	ep := cmp.Or(req.Epoch, c.Epoch, 1)
	id := cmp.Or(req.ResultID, FormatResultID(ep, req.Consumer, 0, req.Gen))
	argv := c.argv_internal("report", fmt.Sprintf("%s@%d", req.Consumer, req.Gen),
		"--result", req.Result, "--id", id, "--epoch", strconv.Itoa(ep),
		"--cost-microusd", strconv.FormatInt(max(0, req.Cost), 10), "--priced", strconv.FormatBool(req.Priced),
		"--as", req.Row, "--json")
	for _, kv := range [][2]string{{"--head", req.Head}, {"--reason", req.Reason}, {"--finding", req.Finding}} {
		if kv[1] != "" {
			argv = append(argv, kv[0], kv[1])
		}
	}
	out, code, err := c.run_internal(ctx, argv)
	if cerr := c.classifyCLIError_internal(out, code, err); cerr != nil {
		return store.Receipt{Accepted: false, Reason: TailBytes(out, 512)}, cerr
	}
	var rec store.Receipt
	if err := json.Unmarshal(out, &rec); err != nil {
		return rec, fmt.Errorf("nova-sprint report --json: %s", edge.ParseFailure(err, out))
	}
	rec.Op = cmp.Or(rec.Op, id)
	return rec, nil
}
func (c *CLISprintClient) Release(ctx context.Context, row, consumer string, gen int, head, reason string) error {
	args := []string{"release", fmt.Sprintf("%s@%d", consumer, gen), "--reason", reason, "--epoch", strconv.Itoa(cmp.Or(c.Epoch, 1)), "--as", row}
	if head != "" {
		args = append(args, "--head", head)
	}
	out, code, err := c.run_internal(ctx, c.argv_internal(args...))
	return c.classifyCLIError_internal(out, code, err)
}
func (c *CLISprintClient) OpenJudgement(ctx context.Context, j store.Judgement) error {
	out, code, err := c.run_internal(ctx, c.argv_internal("judge-open", "--id", j.ID, "--kind", j.Kind, "--about", j.About, "--question", j.Question))
	return c.classifyCLIError_internal(out, code, err)
}
func (c *CLISprintClient) Beat(ctx context.Context, row string, hb Heartbeat) error {
	args := []string{"beat", "--as", row, "--load1", strconv.FormatFloat(hb.Load1, 'f', 2, 64),
		"--cores", strconv.Itoa(hb.Cores), "--slots-used", strconv.Itoa(hb.SlotsUsed), "--slots-total", strconv.Itoa(hb.SlotsTotal)}
	if hb.Tiers != "" {
		args = append(args, "--tiers", hb.Tiers)
	}
	out, code, err := c.run_internal(ctx, c.argv_internal(args...))
	return c.classifyCLIError_internal(out, code, err)
}
