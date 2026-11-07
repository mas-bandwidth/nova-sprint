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
	CheckFault              CheckState = "fault"
	FaultRateLimit          FaultKind  = "rate_limit"
	FaultGate               FaultKind  = "gate_fault"
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
	Runs             int // the checks reported on the commit; 0 while no CI has started (or none ever will)
}

// Slug is a repository as gh and the API name it, org/repo, from the forms a
// stream stores (https URL, ssh, with or without .git).
func Slug(repo string) string {
	r := strings.TrimSpace(repo)
	for _, p := range []string{"https://github.com/", "http://github.com/", "ssh://git@github.com/", "git@github.com:", "github.com/"} {
		r = strings.TrimPrefix(r, p)
	}
	return strings.TrimSuffix(strings.TrimSuffix(r, "/"), ".git")
}

// jobRe is a GitHub Actions job URL: .../actions/runs/<run>/job/<job>.
var jobRe = regexp.MustCompile(`/actions/runs/(\d+)/job/(\d+)`)

// JobOf is the run and job ids of a check's details URL, or false.
func JobOf(url string) (run, job string, ok bool) {
	m := jobRe.FindStringSubmatch(url)
	if len(m) != 3 {
		return "", "", false
	}
	return m[1], m[2], true
}

// FailingLines is the first failing lines of the Actions job a check's
// details URL names (gh run view --job <id> --log-failed): the go test FAIL
// and error lines first, else the log's last lines; at most n. Nothing
// when the URL is not an Actions job or gh cannot read it.
func (c *Client) FailingLines(ctx context.Context, repo, url string, n int) []string {
	_, job, ok := JobOf(url)
	if !ok || n <= 0 {
		return nil
	}
	stdout, _, err := c.runGH_internal(ctx, "run", "view", "--job", job, "--repo", Slug(repo), "--log-failed")
	if err != nil {
		return nil
	}
	return PickFailing(stdout, n)
}

// PickFailing is the lines of a failed job's log worth quoting: lines with a
// test failure or an error, in order, else the last non-empty lines. The
// job and step columns and the timestamp gh prints are cut.
func PickFailing(log string, n int) []string {
	var all, hits []string
	for _, line := range strings.Split(log, "\n") {
		f := strings.SplitN(line, "\t", 3)
		text := strings.TrimSpace(f[len(f)-1])
		if sp := strings.IndexByte(text, ' '); sp > 0 && strings.HasSuffix(text[:sp], "Z") && strings.Contains(text[:sp], "T") {
			text = strings.TrimSpace(text[sp+1:])
		}
		if text == "" {
			continue
		}
		all = append(all, text)
		low := strings.ToLower(text)
		if strings.HasPrefix(text, "--- FAIL") || strings.HasPrefix(text, "FAIL") || strings.Contains(low, "panic:") || strings.Contains(text, "_test.go:") || (strings.Contains(low, "error") && !strings.Contains(low, "process completed with exit code")) {
			hits = append(hits, text)
		}
	}
	if len(hits) == 0 {
		hits = all[max(0, len(all)-n):]
	}
	return hits[:min(len(hits), n)]
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
type Identity struct {
	Name, ExpectedLogin, Token, ConfigDir string
	// Host is the host's own logged-in gh, by the deployer's explicit choice
	// (NOVA_SPRINT_FORGE_HOST=1; Glenn 2026-11-07: the studio's gh is the one
	// identity that acts on every org the sprint lands in). It needs no
	// token or config dir and its login is not refused.
	Host bool
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
func (c *Client) checkCredentials_internal(ctx context.Context) error {
	id := c.cfg.Identity
	name := cmp.Or(id.Name, "unknown")
	switch {
	case id.Host:
	case strings.TrimSpace(id.Token) == "" && strings.TrimSpace(id.ConfigDir) == "":
		return fmt.Errorf("forge refused: neither GH_TOKEN nor GH_CONFIG_DIR is set for identity %s: run nova-secrets exec or set GH_CONFIG_DIR", name)
	case isForbiddenLogin_internal(id.ExpectedLogin):
		return fmt.Errorf("forge refused: gh auth login is %s, expected an identity other than the human's (the human's own credentials are never used): set GH_CONFIG_DIR for %s", id.ExpectedLogin, name)
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
	want := cmp.Or(id.ExpectedLogin, name)
	if (!id.Host && isForbiddenLogin_internal(login)) || (id.ExpectedLogin != "" && !strings.EqualFold(login, id.ExpectedLogin)) {
		return fmt.Errorf("forge refused: gh auth login is %s, expected %s (the human's own credentials are never used): set GH_CONFIG_DIR for %s", cmp.Or(login, "unknown"), want, want)
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
		if !strings.HasPrefix(kv, "GH_TOKEN=") && !strings.HasPrefix(kv, "GITHUB_TOKEN=") && !strings.HasPrefix(kv, "GH_CONFIG_DIR=") {
			env = append(env, kv)
		}
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
	res, err := c.cfg.Exec.Run(ctx, edge.Cmd{Argv: append([]string{c.cfg.Binary}, argv...), Env: c.env_internal(), CleanEnv: true, Timeout: c.cfg.Timeout})
	if err != nil || !res.OK() {
		if retrySec, ok := detectRateLimit_internal(res.Stderr); ok {
			c.mu.Lock()
			c.restTill = c.cfg.Now() + retrySec
			c.mu.Unlock()
			return res.Stdout, res.Stderr, &FaultError{Kind: FaultRateLimit, Identity: c.cfg.Identity.Name, RetryAfter: c.RestedUntil(), Reason: strings.TrimSpace(res.Stderr)}
		}
	}
	if err != nil {
		return res.Stdout, res.Stderr, err
	}
	if !res.OK() {
		return res.Stdout, res.Stderr, fmt.Errorf("gh %s exited %d: %s", strings.Join(argv, " "), res.ExitCode, strings.TrimSpace(res.Stderr))
	}
	return res.Stdout, res.Stderr, nil
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
	repo = Slug(repo)
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
	repo = Slug(repo)
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

// PRState is a PR of a head branch into a base as the landing reads it.
type PRState struct {
	Number         int
	URL, State     string // OPEN, MERGED, CLOSED
	HeadSHA, Merge string // the head it is at; the merge commit once merged
	Mergeable      string // MERGEABLE, CONFLICTING, UNKNOWN
}

// FindPR is the newest PR of head into base in any state.
func (c *Client) FindPR(ctx context.Context, repo, head, base string) (PRState, bool, error) {
	repo = Slug(repo)
	if repo == "" || head == "" || base == "" {
		return PRState{}, false, fmt.Errorf("forge refused: PR lookup requires repo, head, and base")
	}
	stdout, _, err := c.runGH_internal(ctx, "pr", "list", "--repo", repo, "--head", head, "--base", base, "--state", "all", "--limit", "1", "--json", "number,url,state,headRefOid,mergeCommit,mergeable")
	if err != nil {
		return PRState{}, false, err
	}
	return decodePRState_internal(stdout)
}

func decodePRState_internal(stdout string) (PRState, bool, error) {
	var prs []struct {
		Number      int
		URL, State  string
		HeadRefOid  string `json:"headRefOid"`
		Mergeable   string `json:"mergeable"`
		MergeCommit *struct {
			Oid string `json:"oid"`
		} `json:"mergeCommit"`
	}
	if err := json.Unmarshal([]byte(stdout), &prs); err != nil {
		return PRState{}, false, fmt.Errorf("forge: decode pr list: %w", err)
	}
	if len(prs) == 0 {
		return PRState{}, false, nil
	}
	p := prs[0]
	out := PRState{Number: p.Number, URL: p.URL, State: strings.ToUpper(p.State), HeadSHA: strings.ToLower(p.HeadRefOid), Mergeable: strings.ToUpper(p.Mergeable)}
	if p.MergeCommit != nil {
		out.Merge = strings.ToLower(p.MergeCommit.Oid)
	}
	return out, true, nil
}

// MergeMethods are tried in order for a card's PR that is behind its base
// (one that is not is fast-forwarded to the exact sha its checks passed): a
// merge commit first, so the head's commits land as they were read; a
// ruleset that allows only one method refuses the others by name.
var MergeMethods = []string{"--merge", "--squash", "--rebase"}

// Behind is how many commits base has that head does not (0: head contains
// base, so base can fast-forward to head).
func (c *Client) Behind(ctx context.Context, repo, base, head string) (int, error) {
	stdout, _, err := c.runGH_internal(ctx, "api", fmt.Sprintf("repos/%s/compare/%s...%s", Slug(repo), base, head), "--jq", ".behind_by")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(stdout))
}

// MergePR merges PR pr of repo at head (gh pr merge --match-head-commit) by
// the first of methods the repository allows, and returns the merge commit. A
// refusal other than "method not allowed" is returned verbatim.
func (c *Client) MergePR(ctx context.Context, repo string, pr int, head string, methods []string) (string, error) {
	repo = Slug(repo)
	if pr <= 0 || !isFullSHA_internal(head) {
		return "", fmt.Errorf("forge refused: MergePR requires pr > 0 and a 40-hex head")
	}
	var last error
	for _, m := range methods {
		_, stderr, err := c.runGH_internal(ctx, "pr", "merge", strconv.Itoa(pr), "--repo", repo, m, "--match-head-commit", head)
		if err == nil {
			last = nil
			break
		}
		last = err
		if !methodRefused_internal(stderr + " " + err.Error()) {
			return "", err
		}
	}
	if last != nil {
		return "", last
	}
	stdout, _, err := c.runGH_internal(ctx, "pr", "view", strconv.Itoa(pr), "--repo", repo, "--json", "number,url,state,headRefOid,mergeCommit,mergeable")
	if err != nil {
		return "", err
	}
	st, _, err := decodePRState_internal("[" + stdout + "]")
	if err != nil {
		return "", err
	}
	if st.State != "MERGED" {
		return "", fmt.Errorf("gh pr merge %d returned but the PR is %s (a merge queue or auto-merge holds it)", pr, st.State)
	}
	return st.Merge, nil
}

func methodRefused_internal(s string) bool {
	low := strings.ToLower(s)
	return strings.Contains(low, "not allowed") || strings.Contains(low, "merge method") || strings.Contains(low, "not enabled")
}

func (c *Client) Checks(ctx context.Context, repo string, shas []string) (map[string]CheckRollup, error) {
	repo = Slug(repo)
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
		return nil, fmt.Errorf("forge: decode graphql checks: %w", err)
	}
	out := make(map[string]CheckRollup, len(shas))
	for i, sha := range shas {
		commit := env.Data.Repository[fmt.Sprintf("c%d", i)]
		if commit == nil || commit.StatusCheckRollup == nil {
			out[sha] = CheckRollup{SHA: sha, State: CheckPending, Reason: "no check rollup reported yet"}
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
		return nil, gqlPageInfo{}, err
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
	r := classifyStates_internal(sha, rollupState, nodes, failing, faulted, firstRedURL, firstFaultURL, pending)
	r.Runs = len(nodes)
	return r
}
func classifyStates_internal(sha, rollupState string, nodes []gqlNode, failing, faulted []string, firstRedURL, firstFaultURL string, pending bool) CheckRollup {
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
	repo = Slug(repo)
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
