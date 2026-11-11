package worker

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/mas-bandwidth/nova-sprint/modules/usage"
	"strconv"
	"strings"
	"time"
)

var (
	ErrStoreDown = errors.New("worker: store unreachable")
	ErrRefused   = errors.New("worker: sprint refused")
	// ErrHookFailed is a hook the sprint failed (exit 2) for something
	// other than the store, in the sprint's own words: a stream's origin
	// this host cannot read is not the store being down.
	ErrHookFailed = errors.New("worker: sprint hook failed")
	// ErrReceipt marks a take receipt that lacks what the worker runs a card
	// with (model, deadline, commit identity). It is fatal: every further
	// card would be refused the same way.
	ErrReceipt = errors.New("worker: take receipt refused")
	// ErrTruncated is a sprint verb whose output overran the worker's cap:
	// what it printed is not the answer, so none of it is parsed.
	ErrTruncated = errors.New("worker: sprint output truncated")
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
		PriorFinished                                                                           bool     `json:"prior_finished"` // an attempt reported ok at PriorHead (the card machine's FinishedHeads)
		Paths                                                                                   []string `json:"paths"`
		DoneWhen                                                                                []string `json:"done_when"`
		Epoch, Gen, Attempt, Review                                                             int
		Model, Forge                                                                            string
		Deadline                                                                                int    // seconds, from policy's timeouts().consumer[tier]
		CommitName                                                                              string `json:"commit_name"`  // the sprint's commit identity (store.Identity): every
		CommitEmail                                                                             string `json:"commit_email"` // commit of the job is authored and committed as it
	}
	ReportRequest struct {
		Epoch, Gen                                             int
		Row, Consumer, ResultID, Result, Head, Reason, Finding string
		Cost                                                   int64
		Priced                                                 bool
		Usage                                                  string // the run's tokens, package usage's words (--usage)
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

// outputMaxBytes_internal caps a sprint verb's or a git listing's output, far
// above edge's 64 KiB default: `next --json` lists every finished card the
// row held this epoch (nine of twelve benches passed 64 KiB on 2026-11-09,
// and the capped bytes were parsed: "parse failed at byte 32759", no card
// taken anywhere), and collect reads a whole `ls-tree -r`.
const outputMaxBytes_internal = 16 << 20

// run_internal runs argv for its output (stdout then stderr, as the parsers
// read it); a failure is an edge.CommandError naming the command, its exit
// code and its output's first lines; an output past its cap is ErrTruncated,
// naming the command, and is never handed to a parser.
func (c *CLISprintClient) run_internal(ctx context.Context, argv []string) ([]byte, int, error) {
	if c.Run != nil {
		out, code, err := c.Run(ctx, argv)
		if err != nil || code != 0 {
			return out, code, edge.Failure(argv, nil, code, string(out), "", err)
		}
		return out, code, nil
	}
	res, err := cmp.Or[edge.Exec](c.Exec, edge.NewOSExec()).Run(ctx, edge.Cmd{Argv: argv, Timeout: c.Timeout, MaxBytes: outputMaxBytes_internal})
	out := []byte(res.Stdout + res.Stderr)
	if err != nil || res.ExitCode != 0 {
		return out, res.ExitCode, edge.Failure(argv, nil, res.ExitCode, res.Stdout, res.Stderr, err)
	}
	if res.Truncated {
		return out, res.ExitCode, fmt.Errorf("%w at %d bytes: %s", ErrTruncated, outputMaxBytes_internal, edge.CommandLine(argv, nil))
	}
	return out, res.ExitCode, nil
}

// classifyCLIError_internal is a sprint verb's failure: the store
// unreachable (storeDown_internal) is ErrStoreDown, waited out; any other
// is the sprint's refusal, in its own words (redacted) after the command and
// its exit: exit 1 a refusal, exit 2 a failure that is not the store (a
// misuse, a version skew), anything else a command that did not run as
// asked.
func (c *CLISprintClient) classifyCLIError_internal(out []byte, code int, err error) error {
	if code == 0 && err == nil {
		return nil
	}
	if errors.Is(err, ErrTruncated) {
		return err
	}
	if storeDown_internal(out, code, err) {
		return fmt.Errorf("%w: %w", ErrStoreDown, err)
	}
	words := edge.RedactN(TailBytes(out, 512), 512)
	var ce *edge.CommandError
	if errors.As(err, &ce) {
		return fmt.Errorf("%w: %s: exit %d: %s", ErrRefused, ce.Command, code, cmp.Or(words, edge.RedactN(fmt.Sprint(ce.Err), 512)))
	}
	return fmt.Errorf("%w: exit %d: %s", ErrRefused, code, words)
}

// storeDown_internal: the verb did not get through to the store: it never
// finished (timed out, or stopped by its caller), or the sprint failed
// (exit 2) saying the store or PostgreSQL could not be reached, or saying
// nothing at all.
func storeDown_internal(out []byte, code int, err error) bool {
	var xe *edge.ExecError
	if errors.As(err, &xe) && (xe.Kind == edge.ErrTimeout || xe.Kind == edge.ErrCanceled) {
		return true
	}
	if code != 2 {
		return false
	}
	if strings.TrimSpace(string(out)) == "" {
		return true
	}
	for _, line := range strings.Split(string(out), "\n") {
		if rest, ok := strings.CutPrefix(line, "SPRINT REFUSED "); ok {
			_, words, _ := strings.Cut(rest, ": ")
			return strings.HasPrefix(words, "the store ") || strings.HasPrefix(words, "PostgreSQL cannot be reached")
		}
	}
	return false
}
func (c *CLISprintClient) Hook(ctx context.Context, row string) error {
	out, code, err := c.run_internal(ctx, c.argv_internal("hook", "--as", row, "--role", "worker"))
	if line, ok := strings.CutPrefix(string(out), "SPRINT REFUSED hook: "); code == 2 && err != nil && ok && !strings.HasPrefix(line, "the store ") {
		return fmt.Errorf("%w: %w", ErrHookFailed, err)
	}
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
	out, code, err := c.run_internal(ctx, c.argv_internal("take", fmt.Sprintf("%s@%d", consumer, gen), "--epoch", strconv.Itoa(cmp.Or(c.Epoch, 1)), "--as", row, "--json"))
	var info TakeInfo
	if cerr := c.classifyCLIError_internal(out, code, err); cerr != nil {
		return info, cerr
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return info, fmt.Errorf("nova-sprint take --json: %s", edge.ParseFailure(err, out))
	}
	return info, nil
}

// checkReceipt_internal is what the runner needs from a receipt before it
// stages anything: the model the deal named and a deadline (a forge of ""
// means none).
func checkReceipt_internal(info TakeInfo) error {
	var missing []string
	if info.Model == "" {
		missing = append(missing, "model")
	}
	if info.Deadline <= 0 {
		missing = append(missing, "deadline")
	}
	if info.CommitName == "" || info.CommitEmail == "" {
		missing = append(missing, "commit identity (commit_name, commit_email)")
	}
	if len(missing) > 0 {
		return receiptError_internal(info.Consumer, missing)
	}
	return nil
}
func receiptError_internal(consumer string, missing []string) error {
	return fmt.Errorf("%w: the receipt for %s has no %s: check that `nova-sprint take <card>@<gen> --as <row> --json` prints model, deadline, commit_name and commit_email (the deal's model, the policy's timeouts().consumer[tier], the sprint's commit identity)", ErrReceipt, consumer, strings.Join(missing, ", "))
}
func (c *CLISprintClient) Report(ctx context.Context, req ReportRequest) (store.Receipt, error) {
	ep := cmp.Or(req.Epoch, c.Epoch, 1)
	id := cmp.Or(req.ResultID, resultID_internal(ep, req.Consumer, req.Gen))
	argv := c.argv_internal("report", fmt.Sprintf("%s@%d", req.Consumer, req.Gen),
		"--result", req.Result, "--epoch", strconv.Itoa(ep), "--as", req.Row, "--json")
	cost := "" // unpriced: no --cost
	if req.Priced {
		cost = usage.USD(req.Cost)
	}
	for _, kv := range [][2]string{{"--head", req.Head}, {"--reason", req.Reason}, {"--finding", req.Finding}, {"--usage", req.Usage}, {"--cost", cost}} {
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
func (c *CLISprintClient) Beat(ctx context.Context, row string, hb Heartbeat) error {
	args := []string{"beat", "--as", row, "--load1", strconv.FormatFloat(hb.Load1, 'f', 2, 64), "--cpu-peak", strconv.FormatFloat(hb.CPUPeak, 'f', 1, 64),
		"--cores", strconv.Itoa(hb.Cores), "--slots-used", strconv.Itoa(hb.SlotsUsed), "--slots-total", strconv.Itoa(hb.SlotsTotal)}
	if hb.Tiers != "" {
		args = append(args, "--tiers", hb.Tiers)
	}
	out, code, err := c.run_internal(ctx, c.argv_internal(args...))
	return c.classifyCLIError_internal(out, code, err)
}
