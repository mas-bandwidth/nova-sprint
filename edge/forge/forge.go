// Package forge is the GitHub edge of layer 4 (docs/LAYER-4-EDGES.md).
package forge

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type CheckState string
type FaultKind string

const (
	MaxChecksBatch                     = 100
	DefaultTimeout                     = 30 * time.Second
	DefaultHourlyLimit                 = 5000
	DefaultBucketBurst                 = 100
	DefaultRateLimitRestSec            = 60
	CheckGreen              CheckState = "green"
	CheckRed                CheckState = "red"
	CheckPending            CheckState = "pending"
	// CheckAbsent is a commit with no check rollup at all: no workflow has
	// even queued a job for it. Queued and running jobs are pending; a
	// rollup that stays absent means no CI listens to the branch (defect 40).
	CheckAbsent    CheckState = "absent"
	CheckFault     CheckState = "fault"
	FaultRateLimit FaultKind  = "rate_limit"
	FaultGate      FaultKind  = "gate_fault"
)

var (
	ErrStaleHead = errors.New("forge: stale head commit")
	ErrBaseMoved = errors.New("forge: base branch moved")
	accountRe    = regexp.MustCompile(`(?i)account\s+([A-Za-z0-9_.-]+)`)
	retryAfterRe = regexp.MustCompile(`(?i)retry-after[:\s]+(\d+)`)
)

type ExecFunc func(ctx context.Context, cmd edge.Cmd) (edge.Result, error)

func (f ExecFunc) Run(ctx context.Context, cmd edge.Cmd) (edge.Result, error) { return f(ctx, cmd) }

type CheckRollup struct {
	SHA, URL, Reason string
	State            CheckState
	Failing          []string
}
type PRInfo struct {
	Number                        int
	URL, HeadRefName, BaseRefName string
	State                         string
	HeadRef                       string `json:"headRefName"`
	HeadSHA                       string `json:"headRefOid"`
	BaseRef                       string `json:"baseRefName"`
}
type FaultError struct {
	Kind             FaultKind
	Identity, Reason string
	RetryAfter       int64
}

func (e *FaultError) Error() string {
	return fmt.Sprintf("forge fault (%s) for %s until %d: %s", e.Kind, e.Identity, e.RetryAfter, e.Reason)
}

type Forge interface {
	PR(ctx context.Context, repo, head, base string) (PRInfo, bool, error)
	OpenPR(ctx context.Context, repo, head, base, title, body string) (PRInfo, error)
	Checks(ctx context.Context, repo string, shas []string) (map[string]CheckRollup, error)
	Merge(ctx context.Context, repo string, pr int, sha, base string) error
}

// Identity is who the forge acts as: always an explicit token, never the
// gh login of whoever runs the process (defect 39: the first landing merged
// as the human's own account through the host's bare gh). Name is for the
// refusals; ExpectedLogin, when set, is checked against gh auth status.
type Identity struct{ Name, ExpectedLogin, Token string }

// TokenEnv is the environment variable the sprint's own GitHub token comes
// in: the control seat's secret of that name, handed to nova-sprint by
// nova-secrets exec --only GH_GATE_TOKEN,... (the sprint-run service's unit
// does this).
const TokenEnv = "GH_GATE_TOKEN"

type Config struct {
	Identity           Identity
	Binary             string
	Exec               edge.Exec
	Timeout            time.Duration
	Now                func() int64
	HourlyLimit, Burst int
}
type Client struct {
	cfg                Config
	mu                 sync.Mutex
	verified           bool
	tokens             float64
	lastTick, restTill int64
}

func New(cfg Config) *Client { return NewClient(cfg.Exec, cfg) }
func NewClient(ex edge.Exec, cfg Config) *Client {
	cfg.Binary = cmp.Or(cfg.Binary, "gh")
	cfg.Exec = cmp.Or[edge.Exec](ex, cmp.Or[edge.Exec](cfg.Exec, edge.NewOSExec()))
	cfg.Timeout = cmp.Or(cfg.Timeout, DefaultTimeout)
	cfg.HourlyLimit = cmp.Or(cfg.HourlyLimit, DefaultHourlyLimit)
	cfg.Burst = cmp.Or(cfg.Burst, DefaultBucketBurst)
	if cfg.Now == nil {
		cfg.Now = func() int64 { return time.Now().Unix() }
	}
	return &Client{cfg: cfg, tokens: float64(cfg.Burst), lastTick: cfg.Now()}
}
func (c *Client) RestedUntil() int64 { c.mu.Lock(); defer c.mu.Unlock(); return c.restTill }

// Ready refuses, with its one fix and without running anything, a client
// with no token: the forge never acts as the host's own gh login.
func (c *Client) Ready() error {
	id := c.cfg.Identity
	name := cmp.Or(id.Name, "unknown")
	switch {
	case strings.TrimSpace(id.Token) == "":
		return &edge.Refusal{Problem: "the forge has no GitHub token for " + name + " (" + TokenEnv + " is not in this process) and never acts as whoever gh is logged in as",
			Fix: "run it under nova-secrets exec --only " + TokenEnv + ",... -- nova-sprint ..., so the control seat's token is the identity"}
	case isForbiddenLogin_internal(id.ExpectedLogin):
		return &edge.Refusal{Problem: "the forge identity " + name + " expects the login " + id.ExpectedLogin + ", the human's own account, which is never used",
			Fix: "set NOVA_SPRINT_FORGE_LOGIN to the sprint's own GitHub login"}
	}
	return nil
}
func (c *Client) checkCredentials_internal(ctx context.Context) error {
	if err := c.Ready(); err != nil {
		return err
	}
	id := c.cfg.Identity
	name := cmp.Or(id.Name, "unknown")
	c.mu.Lock()
	already := c.verified
	c.mu.Unlock()
	if already {
		return nil
	}
	cmd := edge.Cmd{Argv: []string{c.cfg.Binary, "auth", "status"}, Env: c.env_internal(), CleanEnv: true, Timeout: c.cfg.Timeout}
	res, err := c.cfg.Exec.Run(ctx, cmd)
	combined := res.Stdout + "\n" + res.Stderr
	login := parseAuthLogin_internal(combined)
	if (err != nil || !res.OK()) && login == "" {
		// The environment is the whole process's: the failure names none of it.
		return fmt.Errorf("forge refused: gh auth status for %s: %w: check the token in %s", name, edge.Failure(cmd.Argv, nil, res.ExitCode, res.Stdout, res.Stderr, err), TokenEnv)
	}
	want := cmp.Or(id.ExpectedLogin, name)
	if isForbiddenLogin_internal(login) || (id.ExpectedLogin != "" && !strings.EqualFold(login, id.ExpectedLogin)) {
		return &edge.Refusal{Problem: "the token in " + TokenEnv + " is " + cmp.Or(login, "an unknown login") + "'s, expected " + want + " (the human's own credentials are never used)",
			Fix: "put " + want + "'s token in the control seat's " + TokenEnv}
	}
	c.mu.Lock()
	c.verified = true
	c.mu.Unlock()
	return nil
}
func isForbiddenLogin_internal(login string) bool {
	switch strings.ToLower(strings.TrimSpace(login)) {
	case "glenn", "gaffer", "gafferongames", "glennfiedler":
		return true
	}
	return false
}
func parseAuthLogin_internal(s string) string {
	if m := accountRe.FindStringSubmatch(s); len(m) == 2 {
		return m[1]
	}
	return ""
}
func (c *Client) env_internal() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GH_TOKEN=") && !strings.HasPrefix(kv, "GITHUB_TOKEN=") && !strings.HasPrefix(kv, "GH_ENTERPRISE_TOKEN=") && !strings.HasPrefix(kv, "GH_CONFIG_DIR=") {
			env = append(env, kv)
		}
	}
	// gh reads GH_TOKEN before its own login; Ready refused an empty one.
	return append(env, "GH_TOKEN="+c.cfg.Identity.Token, "GH_PROMPT_DISABLED=1", "NO_COLOR=1")
}
func (c *Client) takeToken_internal() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.cfg.Now()
	if now < c.restTill {
		return &FaultError{Kind: FaultRateLimit, Identity: c.cfg.Identity.Name, RetryAfter: c.restTill, Reason: "identity is resting after GitHub rate limit"}
	}
	rate := float64(c.cfg.HourlyLimit) / 3600.0
	if now > c.lastTick {
		c.tokens = min(c.tokens+float64(now-c.lastTick)*rate, float64(c.cfg.Burst))
		c.lastTick = now
	}
	if c.tokens < 1.0 {
		c.restTill = now + int64((1.0-c.tokens)/rate) + 1
		return &FaultError{Kind: FaultRateLimit, Identity: c.cfg.Identity.Name, RetryAfter: c.restTill, Reason: "token bucket exhausted for identity"}
	}
	c.tokens -= 1.0
	return nil
}
func (c *Client) runGH_internal(ctx context.Context, argv ...string) (string, string, error) {
	if err := c.checkCredentials_internal(ctx); err != nil {
		return "", "", err
	}
	if err := c.takeToken_internal(); err != nil {
		return "", "", err
	}
	cmd := edge.Cmd{Argv: append([]string{c.cfg.Binary}, argv...), Env: c.env_internal(), CleanEnv: true, Timeout: c.cfg.Timeout}
	res, err := c.cfg.Exec.Run(ctx, cmd)
	if err == nil && res.OK() {
		return res.Stdout, res.Stderr, nil
	}
	// The environment is the whole process's (with GH_TOKEN): the failure
	// names none of it.
	failed := edge.Failure(cmd.Argv, nil, res.ExitCode, res.Stdout, res.Stderr, err)
	if retrySec, ok := detectRateLimit_internal(res.Stderr); ok {
		c.mu.Lock()
		c.restTill = c.cfg.Now() + retrySec
		c.mu.Unlock()
		return res.Stdout, res.Stderr, &FaultError{Kind: FaultRateLimit, Identity: c.cfg.Identity.Name, RetryAfter: c.RestedUntil(), Reason: failed.Error()}
	}
	return res.Stdout, res.Stderr, failed
}

func detectRateLimit_internal(s string) (int64, bool) {
	lower := strings.ToLower(s)
	if !strings.Contains(lower, "http 429") && !strings.Contains(lower, "status 429") &&
		!strings.Contains(lower, "rate limit") && !strings.Contains(lower, "secondary rate limit") &&
		!(strings.Contains(lower, "http 403") && strings.Contains(lower, "limit")) {
		return 0, false
	}
	if m := retryAfterRe.FindStringSubmatch(s); len(m) == 2 {
		if sec, err := strconv.ParseInt(m[1], 10, 64); err == nil && sec > 0 {
			return sec, true
		}
	}
	return DefaultRateLimitRestSec, true
}
func (c *Client) PR(ctx context.Context, repo, head, base string) (PRInfo, bool, error) {
	if repo == "" || head == "" || base == "" {
		return PRInfo{}, false, fmt.Errorf("forge refused: PR lookup requires repo, head, and base")
	}
	stdout, _, err := c.runGH_internal(ctx, "pr", "list", "--repo", repo, "--head", head, "--base", base, "--state", "open", "--json", "number,url,headRefName,headRefOid,baseRefName,state")
	if err != nil {
		return PRInfo{}, false, err
	}
	var prs []PRInfo
	if err := json.Unmarshal([]byte(stdout), &prs); err != nil {
		return PRInfo{}, false, fmt.Errorf("forge: decode gh pr list --json: %s", edge.ParseFailure(err, []byte(stdout)))
	}
	if len(prs) == 0 {
		return PRInfo{}, false, nil
	}
	return prs[0], true, nil
}
func (c *Client) OpenPR(ctx context.Context, repo, head, base, title, body string) (PRInfo, error) {
	if repo == "" || head == "" || base == "" || title == "" {
		return PRInfo{}, fmt.Errorf("forge refused: OpenPR requires repo, head, base, and title")
	}
	if existing, ok, err := c.PR(ctx, repo, head, base); err == nil && ok {
		return existing, nil
	}
	if _, _, err := c.runGH_internal(ctx, "pr", "create", "--repo", repo, "--head", head, "--base", base, "--title", title, "--body", body); err != nil {
		return PRInfo{}, err
	}
	pr, ok, err := c.PR(ctx, repo, head, base)
	if err != nil {
		return PRInfo{}, fmt.Errorf("forge: opened PR for %s -> %s on %s but could not find it: %w", head, base, repo, err)
	}
	if !ok {
		return PRInfo{}, fmt.Errorf("forge: opened PR for %s -> %s on %s but gh pr list shows no open PR for it", head, base, repo)
	}
	return pr, nil
}
func (c *Client) Checks(ctx context.Context, repo string, shas []string) (map[string]CheckRollup, error) {
	if len(shas) > MaxChecksBatch {
		return nil, fmt.Errorf("forge refused: Checks batch size %d exceeds %d SHAs: split into batches of at most %d", len(shas), MaxChecksBatch, MaxChecksBatch)
	}
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		return nil, fmt.Errorf("forge refused: invalid repository %q: expected org/repo", repo)
	}
	for _, sha := range shas {
		if !isFullSHA_internal(sha) {
			return nil, fmt.Errorf("forge refused: invalid SHA %q: pass a 40-character hex commit SHA", sha)
		}
	}
	if len(shas) == 0 {
		return map[string]CheckRollup{}, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "query { repository(owner: %q, name: %q) {", owner, name)
	for i, sha := range shas {
		fmt.Fprintf(&b, ` c%d: object(oid: %q) { ... on Commit { oid statusCheckRollup { state contexts(first: 100) { pageInfo { hasNextPage endCursor } nodes { __typename ... on CheckRun { name status conclusion detailsUrl } ... on StatusContext { context state targetUrl } } } } } }`, i, sha)
	}
	b.WriteString(" } }")
	stdout, _, err := c.runGH_internal(ctx, "api", "graphql", "-f", "query="+b.String())
	if err != nil {
		return nil, err
	}
	return c.parseChecksGraphQL_internal(ctx, owner, name, stdout, shas)
}

type gqlNode struct {
	Typename                                             string `json:"__typename"`
	Name, Status, Conclusion, DetailsURL, Context, State string
	TargetURL                                            string
}
type gqlPageInfo struct {
	HasNextPage bool
	EndCursor   string
}
type gqlChecksEnvelope struct {
	Data struct {
		Repository map[string]*struct {
			OID               string
			StatusCheckRollup *struct {
				State    string
				Contexts struct {
					PageInfo gqlPageInfo
					Nodes    []gqlNode
				}
			}
		}
	}
}

func (c *Client) parseChecksGraphQL_internal(ctx context.Context, owner, name, raw string, shas []string) (map[string]CheckRollup, error) {
	var env gqlChecksEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return nil, fmt.Errorf("forge: decode gh api graphql checks: %s", edge.ParseFailure(err, []byte(raw)))
	}
	out := make(map[string]CheckRollup, len(shas))
	for i, sha := range shas {
		commit := env.Data.Repository[fmt.Sprintf("c%d", i)]
		if commit == nil || commit.StatusCheckRollup == nil {
			out[sha] = CheckRollup{SHA: sha, State: CheckAbsent, Reason: "no check has been reported for this commit"}
			continue
		}
		if commit.OID != "" && !strings.EqualFold(commit.OID, sha) {
			out[sha] = CheckRollup{SHA: sha, State: CheckFault, Reason: fmt.Sprintf("graphql returned oid %s for requested sha %s", commit.OID, sha)}
			continue
		}
		nodes, pi := commit.StatusCheckRollup.Contexts.Nodes, commit.StatusCheckRollup.Contexts.PageInfo
		for pi.HasNextPage && pi.EndCursor != "" {
			more, nextPI, err := c.fetchMoreContexts_internal(ctx, owner, name, sha, pi.EndCursor)
			if err != nil {
				return nil, err
			}
			nodes, pi = append(nodes, more...), nextPI
		}
		out[sha] = classifyRollup_internal(sha, commit.StatusCheckRollup.State, nodes)
	}
	return out, nil
}
func (c *Client) fetchMoreContexts_internal(ctx context.Context, owner, name, sha, cursor string) ([]gqlNode, gqlPageInfo, error) {
	q := fmt.Sprintf(`query { repository(owner: %q, name: %q) { c0: object(oid: %q) { ... on Commit { oid statusCheckRollup { state contexts(first: 100, after: %q) { pageInfo { hasNextPage endCursor } nodes { __typename ... on CheckRun { name status conclusion detailsUrl } ... on StatusContext { context state targetUrl } } } } } } } }`, owner, name, sha, cursor)
	stdout, _, err := c.runGH_internal(ctx, "api", "graphql", "-f", "query="+q)
	if err != nil {
		return nil, gqlPageInfo{}, err
	}
	var env gqlChecksEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		return nil, gqlPageInfo{}, fmt.Errorf("forge: decode gh api graphql check contexts after %s: %s", cursor, edge.ParseFailure(err, []byte(stdout)))
	}
	if commit := env.Data.Repository["c0"]; commit != nil && commit.StatusCheckRollup != nil {
		return commit.StatusCheckRollup.Contexts.Nodes, commit.StatusCheckRollup.Contexts.PageInfo, nil
	}
	return nil, gqlPageInfo{}, nil
}
func classifyNode_internal(n gqlNode) (CheckState, string, string) {
	if n.Typename == "StatusContext" || (n.Context != "" && n.Name == "") {
		switch strings.ToUpper(n.State) {
		case "SUCCESS":
			return CheckGreen, n.Context, n.TargetURL
		case "PENDING", "EXPECTED":
			return CheckPending, n.Context, n.TargetURL
		case "ERROR":
			return CheckFault, n.Context, n.TargetURL
		default:
			return CheckRed, n.Context, n.TargetURL
		}
	}
	if !strings.EqualFold(n.Status, "COMPLETED") {
		return CheckPending, n.Name, n.DetailsURL
	}
	switch strings.ToUpper(n.Conclusion) {
	case "SUCCESS", "NEUTRAL", "SKIPPED":
		return CheckGreen, n.Name, n.DetailsURL
	case "FAILURE":
		return CheckRed, n.Name, n.DetailsURL
	case "CANCELLED", "TIMED_OUT", "STARTUP_FAILURE", "ACTION_REQUIRED", "STALE":
		return CheckFault, n.Name, n.DetailsURL
	default:
		return CheckPending, n.Name, n.DetailsURL
	}
}
func classifyRollup_internal(sha, rollupState string, nodes []gqlNode) CheckRollup {
	var failing, faulted []string
	var firstRedURL, firstFaultURL string
	pending := false
	for _, n := range nodes {
		st, name, url := classifyNode_internal(n)
		switch st {
		case CheckPending:
			pending = true
		case CheckRed:
			failing, firstRedURL = append(failing, name), cmp.Or(firstRedURL, url)
		case CheckFault:
			faulted, firstFaultURL = append(faulted, name), cmp.Or(firstFaultURL, url)
		}
	}
	switch {
	case len(faulted) > 0 && len(failing) == 0:
		return CheckRollup{SHA: sha, State: CheckFault, Failing: faulted, URL: firstFaultURL, Reason: "gate did not run: " + strings.Join(faulted, ", ")}
	case len(failing) > 0:
		return CheckRollup{SHA: sha, State: CheckRed, Failing: failing, URL: firstRedURL, Reason: "CI red: " + strings.Join(failing, ", ")}
	case strings.EqualFold(rollupState, "FAILURE") || strings.EqualFold(rollupState, "ERROR"):
		return CheckRollup{SHA: sha, State: CheckRed, Failing: []string{"statusCheckRollup"}, Reason: "statusCheckRollup state " + rollupState}
	case pending || strings.EqualFold(rollupState, "PENDING") || strings.EqualFold(rollupState, "EXPECTED") || len(nodes) == 0:
		if strings.EqualFold(rollupState, "SUCCESS") && len(nodes) == 0 {
			return CheckRollup{SHA: sha, State: CheckGreen}
		}
		return CheckRollup{SHA: sha, State: CheckPending, Reason: "checks in progress"}
	default:
		return CheckRollup{SHA: sha, State: CheckGreen}
	}
}
func (c *Client) Merge(ctx context.Context, repo string, pr int, sha, base string) error {
	if pr <= 0 || !isFullSHA_internal(sha) || base == "" {
		return fmt.Errorf("forge refused: Merge requires pr > 0, 40-hex sha, and base")
	}
	stdout, _, err := c.runGH_internal(ctx, "pr", "view", strconv.Itoa(pr), "--repo", repo, "--json", "number,url,headRefName,headRefOid,baseRefName,state")
	if err != nil {
		return err
	}
	var info PRInfo
	if err := json.Unmarshal([]byte(stdout), &info); err != nil {
		return fmt.Errorf("forge: decode gh pr view --json: %s", edge.ParseFailure(err, []byte(stdout)))
	}
	switch {
	case info.BaseRef != base:
		return fmt.Errorf("%w: PR #%d base is %s, expected %s: re-target PR to %s", ErrBaseMoved, pr, info.BaseRef, base, base)
	case info.HeadSHA != "" && !strings.EqualFold(info.HeadSHA, sha):
		return fmt.Errorf("%w: PR #%d head is %s, expected gated sha %s: re-gate before merge", ErrStaleHead, pr, info.HeadSHA, sha)
	}
	_, stderr, err := c.runGH_internal(ctx, "api", fmt.Sprintf("repos/%s/git/refs/heads/%s", repo, base), "-X", "PATCH", "-f", "sha="+sha)
	if err != nil {
		low := strings.ToLower(stderr + " " + err.Error())
		switch {
		case strings.Contains(low, "head branch was modified"):
			return fmt.Errorf("%w: %v", ErrStaleHead, err)
		case strings.Contains(low, "base branch was modified") || strings.Contains(low, "out-of-date") || strings.Contains(low, "not up to date") || strings.Contains(low, "fast-forward") || strings.Contains(low, "fast forward"):
			return fmt.Errorf("%w: %v", ErrBaseMoved, err)
		}
		return err
	}
	return nil
}
func isFullSHA_internal(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := 0; i < 40; i++ {
		if c := s[i]; !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
