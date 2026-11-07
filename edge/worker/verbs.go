package worker

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/store"
	"time"
)

var (
	ErrStoreDown = errors.New("worker: store unreachable")
	ErrRefused   = errors.New("worker: sprint refused")
)

type ConsumerSummary struct {
	ID, Producer, Kind, Stream, State, Priority, Head string
	Gen, Attempt, Score                               int
}
type NextResponse struct {
	Epoch, Width          int
	Seq                   int64
	Row, State, Model     string
	Harness               string
	Held                  bool
	Ready, Working, Final []ConsumerSummary
}
type TakeInfo struct {
	Consumer, Producer, Kind, Stream, Repo, Base, Branch, Head, Tier, Brief, Prose, Finding string
	StreamRef                                                                               string   `json:"stream_ref"`
	BaseSHA                                                                                 string   `json:"base_sha"`
	PriorHead                                                                               string   `json:"prior_head"`
	Paths                                                                                   []string `json:"paths"`
	DoneWhen                                                                                []string `json:"done_when"`
	Epoch, Gen, Attempt, Review                                                             int
	RequirePR                                                                               bool `json:"require_pr"`
}
type ReportRequest struct {
	Epoch, Gen                                    int
	Row, Consumer, ResultID, Result, Head, Reason string
	Finding                                       string
	Cost                                          int64
	Priced                                        bool
}
type SprintClient interface {
	Next(ctx context.Context, row string) (NextResponse, error)
	Take(ctx context.Context, row, consumer string, gen int) (TakeInfo, error)
	Report(ctx context.Context, req ReportRequest) (store.Receipt, error)
	OpenJudgement(ctx context.Context, j store.Judgement) error
}
type CLISprintClient struct {
	Exec                  edge.Exec
	Timeout               time.Duration
	Run                   func(ctx context.Context, argv []string) ([]byte, int, error)
	SecretsBin, SprintBin string
}

func (c *CLISprintClient) wrap_internal(args ...string) []string {
	sb := cmp.Or(c.SprintBin, "nova-sprint")
	if c.SecretsBin != "" {
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
	msg := TailBytes(out, 512)
	switch {
	case code == 1:
		return fmt.Errorf("%w: %s", ErrRefused, msg)
	case code == 2 || code < 0 || (err != nil && code != 0):
		return fmt.Errorf("%w: %s", ErrStoreDown, msg)
	case err != nil:
		return fmt.Errorf("%w: %v", ErrStoreDown, err)
	case code != 0:
		return fmt.Errorf("%w: exit %d: %s", ErrStoreDown, code, msg)
	default:
		return nil
	}
}
func (c *CLISprintClient) Next(ctx context.Context, row string) (NextResponse, error) {
	out, code, err := c.run_internal(ctx, c.wrap_internal("next", "--as", row, "--json"))
	if cerr := c.classifyCLIError_internal(out, code, err); cerr != nil {
		return NextResponse{}, cerr
	}
	var resp NextResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return NextResponse{}, fmt.Errorf("parse next --json: %w", err)
	}
	return resp, nil
}
func (c *CLISprintClient) Take(ctx context.Context, row, consumer string, gen int) (TakeInfo, error) {
	out, code, err := c.run_internal(ctx, c.wrap_internal("take", fmt.Sprintf("%s@%d", consumer, gen), "--as", row, "--json"))
	if cerr := c.classifyCLIError_internal(out, code, err); cerr != nil {
		return TakeInfo{}, cerr
	}
	var info TakeInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return TakeInfo{}, fmt.Errorf("parse take --json: %w", err)
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
		return store.Receipt{Accepted: true, Op: req.ResultID}, nil
	}
	rec.Op = cmp.Or(rec.Op, req.ResultID)
	return rec, nil
}
func (c *CLISprintClient) OpenJudgement(ctx context.Context, j store.Judgement) error {
	out, code, err := c.run_internal(ctx, c.wrap_internal("judge-open", "--id", j.ID, "--kind", j.Kind, "--about", j.About, "--question", j.Question))
	return c.classifyCLIError_internal(out, code, err)
}
