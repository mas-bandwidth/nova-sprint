// Package forge is the GitHub edge of layer 4 (docs/LAYER-4-EDGES.md).
package forge

import (
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

const (
	MaxChecksBatch          = 100
	DefaultTimeout          = 30 * time.Second
	DefaultHourlyLimit      = 5000
	DefaultBucketBurst      = 100
	DefaultRateLimitRestSec = 60
)

var (
	ErrStaleHead = errors.New("forge: stale head commit")
	ErrBaseMoved = errors.New("forge: base branch moved")
)

type ExecFunc func(ctx context.Context, cmd edge.Cmd) (edge.Result, error)

func (f ExecFunc) Run(ctx context.Context, cmd edge.Cmd) (edge.Result, error) { return f(ctx, cmd) }

type CheckState string

const (
	CheckGreen   CheckState = "green"
	CheckRed     CheckState = "red"
	CheckPending CheckState = "pending"
	CheckFault   CheckState = "fault"
)

type CheckRollup struct {
	SHA         string
	State       CheckState
	Failing     []string
	URL, Reason string
}
type PRInfo struct {
	Number  int    `json:"number"`
	URL     string `json:"url"`
	HeadRef string `json:"headRefName"`
	HeadSHA string `json:"headRefOid"`
	BaseRef string `json:"baseRefName"`
	State   string `json:"state"`
}
type FaultKind string

const (
	FaultRateLimit FaultKind = "rate_limit"
	FaultGate      FaultKind = "gate_fault"
)

type FaultError struct {
	Kind       FaultKind
	Identity   string
	RetryAfter int64
	Reason     string
}

func (e *FaultError) Error() string {
	return fmt.Sprintf("forge fault (%s) for %s until %d: %s", e.Kind, e.Identity, e.RetryAfter, e.Reason)
}

type Forge interface {
	PR(ctx context.Context, repo, head, base string) (PRInfo, bool, error)
	OpenPR(ctx context.Context, repo, head, base, title, body string) (PRInfo, error)
	Checks(ctx context.Context, repo string, shas []string) (map[string]CheckRollup, error)
	Merge(ctx context.Context, repo string, pr int, sha, base string) error
	CheckBaseProtection(ctx context.Context, repo, base string) error
}
type Identity struct {
	Name, ExpectedLogin, Token, ConfigDir string
}
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
	dedup              *edge.Deduplicator
}

func New(cfg Config) *Client {
	if cfg.Binary == "" {
		cfg.Binary = "gh"
	}
	if cfg.Exec == nil {
		cfg.Exec = edge.NewOSExec()
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.Now == nil {
		cfg.Now = func() int64 { return time.Now().Unix() }
	}
	if cfg.HourlyLimit <= 0 {
		cfg.HourlyLimit = DefaultHourlyLimit
	}
	if cfg.Burst <= 0 {
		cfg.Burst = DefaultBucketBurst
	}
	return &Client{cfg: cfg, tokens: float64(cfg.Burst), lastTick: cfg.Now(), dedup: edge.NewDeduplicator()}
}
func (c *Client) RestedUntil() int64 { c.mu.Lock(); defer c.mu.Unlock(); return c.restTill }
func (c *Client) checkCredentials_internal(ctx context.Context) error {
	id := c.cfg.Identity
	name := id.Name
	if name == "" {
		name = "unknown"
	}
	switch {
	case strings.TrimSpace(id.Token) == "" && strings.TrimSpace(id.ConfigDir) == "":
		return fmt.Errorf("forge refused: neither GH_TOKEN nor GH_CONFIG_DIR is set for identity %s: run nova-secrets exec or set GH_CONFIG_DIR", name)
	case isForbiddenLogin_internal(id.ExpectedLogin):
		return fmt.Errorf("forge refused: gh auth login is %s, expected a non-Glenn identity (Glenn's credentials are never used): set GH_CONFIG_DIR for %s", id.ExpectedLogin, name)
	}
	c.mu.Lock()
	already := c.verified
	c.mu.Unlock()
	if already {
		return nil
	}
	res, err := c.cfg.Exec.Run(ctx, edge.Cmd{Argv: []string{c.cfg.Binary, "auth", "status"}, Env: c.env_internal(), CleanEnv: true, Timeout: c.cfg.Timeout})
	combined := res.Stdout + "\n" + res.Stderr
	login := parseAuthLogin_internal(combined)
	if (err != nil || !res.OK()) && login == "" {
		return fmt.Errorf("forge refused: gh auth status failed for %s: %s: set GH_CONFIG_DIR for %s", name, strings.TrimSpace(combined), name)
	}
	want := id.ExpectedLogin
	if want == "" {
		want = name
	}
	if isForbiddenLogin_internal(login) || (id.ExpectedLogin != "" && !strings.EqualFold(login, id.ExpectedLogin)) {
		if login == "" {
			login = "unknown"
		}
		return fmt.Errorf("forge refused: gh auth login is %s, expected %s (Glenn's credentials are never used): set GH_CONFIG_DIR for %s", login, want, want)
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

var accountRe = regexp.MustCompile(`(?i)account\s+([A-Za-z0-9_.-]+)`)

func parseAuthLogin_internal(s string) string {
	if m := accountRe.FindStringSubmatch(s); len(m) == 2 {
		return m[1]
	}
	return ""
}
func (c *Client) env_internal() []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GH_TOKEN=") || strings.HasPrefix(kv, "GITHUB_TOKEN=") || strings.HasPrefix(kv, "GH_CONFIG_DIR=") {
			continue
		}
		env = append(env, kv)
	}
	if c.cfg.Identity.Token != "" {
		env = append(env, "GH_TOKEN="+c.cfg.Identity.Token)
	}
	if c.cfg.Identity.ConfigDir != "" {
		env = append(env, "GH_CONFIG_DIR="+c.cfg.Identity.ConfigDir)
	}
	return append(env, "GH_PROMPT_DISABLED=1", "NO_COLOR=1")
}
func (c *Client) takeToken_internal() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.cfg.Now()
	if now < c.restTill {
		return &FaultError{Kind: FaultRateLimit, Identity: c.cfg.Identity.Name, RetryAfter: c.restTill, Reason: "identity is resting after GitHub rate limit"}
	}
	if now > c.lastTick {
		rate := float64(c.cfg.HourlyLimit) / 3600.0
		c.tokens += float64(now-c.lastTick) * rate
		if c.tokens > float64(c.cfg.Burst) {
			c.tokens = float64(c.cfg.Burst)
		}
		c.lastTick = now
	}
	if c.tokens < 1.0 {
		rate := float64(c.cfg.HourlyLimit) / 3600.0
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
	res, err := c.cfg.Exec.Run(ctx, edge.Cmd{Argv: append([]string{c.cfg.Binary}, argv...), Env: c.env_internal(), CleanEnv: true, Timeout: c.cfg.Timeout})
	combined := res.Stdout + "\n" + res.Stderr
	if retrySec, ok := detectRateLimit_internal(combined); ok {
		c.mu.Lock()
		c.restTill = c.cfg.Now() + retrySec
		until := c.restTill
		c.mu.Unlock()
		return res.Stdout, res.Stderr, &FaultError{Kind: FaultRateLimit, Identity: c.cfg.Identity.Name, RetryAfter: until, Reason: strings.TrimSpace(res.Stderr)}
	}
	switch {
	case err != nil:
		return res.Stdout, res.Stderr, err
	case !res.OK():
		return res.Stdout, res.Stderr, fmt.Errorf("gh %s exited %d: %s", strings.Join(argv, " "), res.ExitCode, strings.TrimSpace(res.Stderr))
	}
	return res.Stdout, res.Stderr, nil
}

var retryAfterRe = regexp.MustCompile(`(?i)retry-after[:\s]+(\d+)`)

func detectRateLimit_internal(s string) (int64, bool) {
	lower := strings.ToLower(s)
	isLimit := strings.Contains(lower, "http 429") || strings.Contains(lower, "status 429") ||
		strings.Contains(lower, "rate limit") || strings.Contains(lower, "secondary rate limit") ||
		(strings.Contains(lower, "http 403") && strings.Contains(lower, "limit"))
	if !isLimit {
		return 0, false
	}
	if m := retryAfterRe.FindStringSubmatch(s); len(m) == 2 {
		if sec, err := strconv.ParseInt(m[1], 10, 64); err == nil && sec > 0 {
			return sec, true
		}
	}
	return DefaultRateLimitRestSec, true
}
func (c *Client) Query(ctx context.Context, repo string, req edge.EdgeRequest) (edge.EdgeResult, error) {
	return c.dedup.Do(ctx, req, func(ctx context.Context, r edge.EdgeRequest) (edge.EdgeResult, error) {
		out := edge.EdgeResult{ID: r.ID, Kind: r.Kind, Card: r.Card, SHA: r.SHA}
		m, err := c.Checks(ctx, repo, []string{r.SHA})
		if err != nil {
			var fe *FaultError
			if errors.As(err, &fe) {
				out.Outcome = string(CheckFault)
				out.Failure = string(fe.Kind)
				out.Reason = fe.Error()
				return out, nil
			}
			return out, err
		}
		cr := m[r.SHA]
		out.Outcome = string(cr.State)
		out.Reason = cr.Reason
		if cr.URL != "" {
			out.Reason = cr.Reason + " (" + cr.URL + ")"
		}
		return out, nil
	})
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
		return PRInfo{}, false, fmt.Errorf("forge: decode pr list: %w", err)
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
	if err != nil || !ok {
		return PRInfo{}, fmt.Errorf("forge: opened PR for %s -> %s on %s but could not find it: %v", head, base, repo, err)
	}
	return pr, nil
}
func (c *Client) Checks(ctx context.Context, repo string, shas []string) (map[string]CheckRollup, error) {
	if len(shas) > MaxChecksBatch {
		return nil, fmt.Errorf("forge refused: Checks batch size %d exceeds %d SHAs: split into batches of at most %d", len(shas), MaxChecksBatch, MaxChecksBatch)
	}
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		return nil, fmt.Errorf("forge refused: invalid repository %q: expected owner/name", repo)
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
		fmt.Fprintf(&b, ` c%d: object(oid: %q) { ... on Commit { statusCheckRollup { state contexts(first: 100) { nodes { __typename ... on CheckRun { name status conclusion detailsUrl } ... on StatusContext { context state targetUrl } } } } } }`, i, sha)
	}
	b.WriteString(" } }")
	stdout, _, err := c.runGH_internal(ctx, "api", "graphql", "-f", "query="+b.String())
	if err != nil {
		return nil, err
	}
	return parseChecksGraphQL_internal(stdout, shas)
}

type gqlNode struct {
	Typename   string `json:"__typename"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	DetailsURL string `json:"detailsUrl"`
	Context    string `json:"context"`
	State      string `json:"state"`
	TargetURL  string `json:"targetUrl"`
}
type gqlChecksEnvelope struct {
	Data struct {
		Repository map[string]*struct {
			StatusCheckRollup *struct {
				State    string `json:"state"`
				Contexts struct {
					Nodes []gqlNode `json:"nodes"`
				} `json:"contexts"`
			} `json:"statusCheckRollup"`
		} `json:"repository"`
	} `json:"data"`
}

func parseChecksGraphQL_internal(raw string, shas []string) (map[string]CheckRollup, error) {
	var env gqlChecksEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return nil, fmt.Errorf("forge: decode graphql checks: %w", err)
	}
	out := make(map[string]CheckRollup, len(shas))
	for i, sha := range shas {
		commit := env.Data.Repository[fmt.Sprintf("c%d", i)]
		if commit == nil || commit.StatusCheckRollup == nil {
			out[sha] = CheckRollup{SHA: sha, State: CheckPending, Reason: "no check rollup reported yet"}
			continue
		}
		out[sha] = classifyRollup_internal(sha, commit.StatusCheckRollup.State, commit.StatusCheckRollup.Contexts.Nodes)
	}
	return out, nil
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
			failing = append(failing, name)
			if firstRedURL == "" {
				firstRedURL = url
			}
		case CheckFault:
			faulted = append(faulted, name)
			if firstFaultURL == "" {
				firstFaultURL = url
			}
		}
	}
	if len(faulted) > 0 && len(failing) == 0 {
		return CheckRollup{SHA: sha, State: CheckFault, Failing: faulted, URL: firstFaultURL, Reason: "gate did not run: " + strings.Join(faulted, ", ")}
	}
	if len(failing) > 0 {
		return CheckRollup{SHA: sha, State: CheckRed, Failing: failing, URL: firstRedURL, Reason: "CI red: " + strings.Join(failing, ", ")}
	}
	if pending || len(nodes) == 0 {
		if strings.EqualFold(rollupState, "SUCCESS") && len(nodes) == 0 {
			return CheckRollup{SHA: sha, State: CheckGreen}
		}
		return CheckRollup{SHA: sha, State: CheckPending, Reason: "checks in progress"}
	}
	return CheckRollup{SHA: sha, State: CheckGreen}
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
		return fmt.Errorf("forge: decode pr view: %w", err)
	}
	switch {
	case info.BaseRef != base:
		return fmt.Errorf("%w: PR #%d base is %s, expected %s: re-target PR to %s", ErrBaseMoved, pr, info.BaseRef, base, base)
	case info.HeadSHA != "" && !strings.EqualFold(info.HeadSHA, sha):
		return fmt.Errorf("%w: PR #%d head is %s, expected gated sha %s: re-gate before merge", ErrStaleHead, pr, info.HeadSHA, sha)
	}
	_, stderr, err := c.runGH_internal(ctx, "pr", "merge", strconv.Itoa(pr), "--repo", repo, "--merge", "--match-head-commit", sha)
	if err != nil {
		low := strings.ToLower(stderr + " " + err.Error())
		switch {
		case strings.Contains(low, "match-head-commit") || strings.Contains(low, "head branch was modified"):
			return fmt.Errorf("%w: %v", ErrStaleHead, err)
		case strings.Contains(low, "base branch was modified") || strings.Contains(low, "out-of-date") || strings.Contains(low, "not up to date"):
			return fmt.Errorf("%w: %v", ErrBaseMoved, err)
		}
		return err
	}
	return nil
}
func (c *Client) CheckBaseProtection(ctx context.Context, repo, base string) error {
	stdout, _, err := c.runGH_internal(ctx, "api", fmt.Sprintf("repos/%s/branches/%s/protection", repo, base))
	if err != nil {
		return fmt.Errorf("SPRINT REFUSED stream add: base branch %s on %s does not require branches to be up to date before merging: enable strict status checks on %s/%s", base, repo, repo, base)
	}
	var prot struct {
		RequiredStatusChecks *struct {
			Strict bool `json:"strict"`
		} `json:"required_status_checks"`
	}
	if err := json.Unmarshal([]byte(stdout), &prot); err != nil || prot.RequiredStatusChecks == nil || !prot.RequiredStatusChecks.Strict {
		return fmt.Errorf("SPRINT REFUSED stream add: base branch %s on %s does not require branches to be up to date before merging: enable strict status checks on %s/%s", base, repo, repo, base)
	}
	return nil
}
func isFullSHA_internal(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := 0; i < 40; i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
