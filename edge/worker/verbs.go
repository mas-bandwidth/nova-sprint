package worker

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/store"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrStoreDown = errors.New("worker: store unreachable")
	ErrRefused   = errors.New("worker: sprint refused")
)

type (
	ConsumerSummary struct {
		ID, Producer, Kind, Stream, State, Priority, Head string
		Gen, Attempt, Score                               int
	}
	NextResponse struct {
		Epoch, Width          int
		Seq                   int64
		Row, State, Model     string
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
		RequirePR                                                                               bool `json:"require_pr"`
	}
	ReportRequest struct {
		Epoch, Gen                                             int
		Row, Consumer, ResultID, Result, Head, Reason, Finding string
		Cost                                                   int64
		Priced                                                 bool
	}
	SprintClient interface {
		Next(ctx context.Context, row string) (NextResponse, error)
		Take(ctx context.Context, row, consumer string, gen int) (TakeInfo, error)
		Report(ctx context.Context, req ReportRequest) (store.Receipt, error)
		OpenJudgement(ctx context.Context, j store.Judgement) error
	}
	HookClient interface {
		Hook(ctx context.Context, row string) error
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
	id := FormatTakeOpID(cmp.Or(c.Epoch, 1), consumer, gen, row)
	out, code, err := c.run_internal(ctx, c.wrap_internal("take", fmt.Sprintf("%s@%d", consumer, gen), "--id", id, "--as", row, "--json"))
	var info TakeInfo
	if cerr := c.classifyCLIError_internal(out, code, err); cerr != nil {
		return info, cerr
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return info, fmt.Errorf("parse take --json: %w", err)
	}
	return info, nil
}
func (c *CLISprintClient) Report(ctx context.Context, req ReportRequest) (store.Receipt, error) {
	argv := c.wrap_internal("report", fmt.Sprintf("%s@%d", req.Consumer, req.Gen),
		"--result", req.Result, "--id", req.ResultID, "--as", req.Row, "--json")
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
		return rec, fmt.Errorf("parse report --json: %w", err)
	}
	rec.Op = cmp.Or(rec.Op, req.ResultID)
	return rec, nil
}
func (c *CLISprintClient) OpenJudgement(ctx context.Context, j store.Judgement) error {
	out, code, err := c.run_internal(ctx, c.wrap_internal("judge-open", "--id", j.ID, "--kind", j.Kind, "--about", j.About, "--question", j.Question))
	return c.classifyCLIError_internal(out, code, err)
}
