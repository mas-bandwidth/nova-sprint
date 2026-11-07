package worker

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/store"
	"os"
	"path/filepath"
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
		Deadline                                                                                int // seconds, from policy's timeouts().consumer[tier]
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
		OpenJudgement(ctx context.Context, j store.Judgement) error
	}
	CLISprintClient struct {
		Exec                  edge.Exec
		Timeout               time.Duration
		Run                   func(ctx context.Context, argv []string) ([]byte, int, error)
		SecretsBin, SprintBin string
		Epoch                 int
	}
)

func ReadProviderKey(ex edge.Exec, secretsBin, name string) string {
	// LOCAL DOGFOOD STOPGAP: the worker runs under nova-secrets exec, so the
	// key is in its own environment; nova-tools v2 has no `get` verb.
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	res, err := cmp.Or[edge.Exec](ex, edge.NewOSExec()).Run(context.Background(), edge.Cmd{
		Argv: []string{cmp.Or(secretsBin, "nova-secrets"), "get", name}, CleanEnv: true, Timeout: 5 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		return ""
	}
	return strings.TrimSpace(res.Stdout)
}
func (c *CLISprintClient) wrap_internal(args ...string) []string {
	sb := cmp.Or(c.SprintBin, "nova-sprint")
	if os.Getenv("NOVA_WORKER_NO_WRAP") == "1" { // LOCAL DOGFOOD STOPGAP: the worker already runs under nova-secrets exec; v2 exec needs --store/--key/--only (defect 35)
		return append([]string{sb}, args...)
	}
	if c.SecretsBin != "" && (c.Run != nil || c.SecretsBin != "nova-secrets" || fileExists_internal(filepath.Join(filepath.Dir(resolveToolBin_internal(sb)), "nova-secrets"))) {
		return append([]string{c.SecretsBin, "exec", "--", sb}, args...)
	}
	return append([]string{sb}, args...)
}
func (c *CLISprintClient) run_internal(ctx context.Context, argv []string) ([]byte, int, error) {
	if c.Run != nil {
		return c.Run(ctx, argv)
	}
	res, err := cmp.Or[edge.Exec](c.Exec, edge.NewOSExec()).Run(ctx, edge.Cmd{Argv: argv, Timeout: c.Timeout})
	return []byte(res.Stdout + res.Stderr), res.ExitCode, err
}
func (c *CLISprintClient) classifyCLIError_internal(out []byte, code int, err error) error {
	if code == 0 && err == nil {
		return nil
	}
	if code == 1 {
		return fmt.Errorf("%w: %s", ErrRefused, TailBytes(out, 512))
	}
	return fmt.Errorf("%w: exit %d (%v): %s", ErrStoreDown, code, err, TailBytes(out, 512))
}
func (c *CLISprintClient) Hook(ctx context.Context, row string) error {
	out, code, err := c.run_internal(ctx, c.wrap_internal("hook", "--as", row, "--role", "worker"))
	return c.classifyCLIError_internal(out, code, err)
}
func (c *CLISprintClient) Unhook(ctx context.Context, row string) error {
	out, code, err := c.run_internal(ctx, c.wrap_internal("unhook", "--as", row))
	return c.classifyCLIError_internal(out, code, err)
}
func (c *CLISprintClient) Next(ctx context.Context, row string) (NextResponse, error) {
	out, code, err := c.run_internal(ctx, c.wrap_internal("next", "--as", row, "--json"))
	var resp NextResponse
	if cerr := c.classifyCLIError_internal(out, code, err); cerr != nil {
		return resp, cerr
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return resp, fmt.Errorf("parse next --json: %w", err)
	}
	c.Epoch = cmp.Or(resp.Epoch, c.Epoch)
	return resp, nil
}
func (c *CLISprintClient) Take(ctx context.Context, row, consumer string, gen int) (TakeInfo, error) {
	ep := cmp.Or(c.Epoch, 1)
	id := FormatTakeOpID(ep, consumer, gen, row)
	out, code, err := c.run_internal(ctx, c.wrap_internal("take", fmt.Sprintf("%s@%d", consumer, gen), "--id", id, "--epoch", strconv.Itoa(ep), "--as", row, "--json"))
	var info TakeInfo
	if cerr := c.classifyCLIError_internal(out, code, err); cerr != nil {
		return info, cerr
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return info, fmt.Errorf("parse take --json: %w", err)
	}
	// The forge may be "" (hand), so presence is read from the keys.
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(out, &keys)
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
// stages anything: a model and a deadline (a forge of "" means none).
func checkReceipt_internal(info TakeInfo) error {
	var missing []string
	if info.Model == "" {
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
	argv := c.wrap_internal("report", fmt.Sprintf("%s@%d", req.Consumer, req.Gen),
		"--result", req.Result, "--id", id, "--epoch", strconv.Itoa(ep),
		"--cost-microusd", strconv.FormatInt(max(0, req.Cost), 10), "--priced", strconv.FormatBool(req.Priced),
		"--as", req.Row, "--json")
	for _, kv := range [][2]string{{"--head", req.Head}, {"--reason", req.Reason}, {"--finding", req.Finding}, {"--usage", req.Usage}} {
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
		return rec, fmt.Errorf("parse report --json: %w", err)
	}
	rec.Op = cmp.Or(rec.Op, id)
	return rec, nil
}
func (c *CLISprintClient) OpenJudgement(ctx context.Context, j store.Judgement) error {
	out, code, err := c.run_internal(ctx, c.wrap_internal("judge-open", "--id", j.ID, "--kind", j.Kind, "--about", j.About, "--question", j.Question))
	return c.classifyCLIError_internal(out, code, err)
}
