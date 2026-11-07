// Package forge is the GitHub edge of layer 4 (docs/LAYER-4-EDGES.md).
package forge

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"os"
	"regexp"
	"slices"
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
	jobRe        = regexp.MustCompile(`/job/(\d+)`)
	failLineRe   = regexp.MustCompile(`(?i)(error|fail|panic)`)
	retryAfterRe = regexp.MustCompile(`(?i)retry-after[:\s]+(\d+)`)
)

type ExecFunc func(ctx context.Context, cmd edge.Cmd) (edge.Result, error)

func (f ExecFunc) Run(ctx context.Context, cmd edge.Cmd) (edge.Result, error) { return f(ctx, cmd) }

// CheckRollup is a commit's checks: State, the failing checks with the
// first one's URL, and Waiting, some check still queued or running (a red
// rollup can still be waiting: directive 76).
type CheckRollup struct {
	SHA, URL, Reason string
	State            CheckState
	Failing          []string
	Waiting          bool
}

// PRInfo is an open PR as gh pr list prints it. Mergeable is GitHub's
// MERGEABLE, CONFLICTING or UNKNOWN (still computing).
type PRInfo struct {
	Number    int
	URL       string `json:"url"`
	State     string `json:"state"`
	HeadRef   string `json:"headRefName"`
	HeadSHA   string `json:"headRefOid"`
	BaseRef   string `json:"baseRefName"`
	Mergeable string `json:"mergeable"`
}
type FaultError struct {
	Kind             FaultKind
	Identity, Reason string
	RetryAfter       int64
}

func (e *FaultError) Error() string {
	return fmt.Sprintf("forge fault (%s) for %s until %d: %s", e.Kind, e.Identity, e.RetryAfter, e.Reason)
}

// Forge is the landing's GitHub (LANDING.md): a PR and its checks, the
// merge by the repository's allowed method at the exact head CI passed, and
// the first failing lines of a red check for the coordinator's judgement.
type Forge interface {
	PR(ctx context.Context, repo, head, base string) (PRInfo, bool, error)
	OpenPR(ctx context.Context, repo, head, base, title, body string) (PRInfo, error)
	Checks(ctx context.Context, repo string, shas []string) (map[string]CheckRollup, error)
	// Merge merges PR pr only while its head is sha, and returns the merge
	// commit base now has.
	Merge(ctx context.Context, repo string, pr int, sha string) (string, error)
	FailedLines(ctx context.Context, repo, url string) ([]string, error)
	// Inheritance splits head's failing workflows into those red on the
	// base too and those that are not (actions.go).
	Inheritance(ctx context.Context, repo, base, baseTip, head string) (RedSplit, error)
	// Authors is PR pr's distinct commit author identities.
	Authors(ctx context.Context, repo string, pr int) ([]string, error)
}

// Identity is who the forge acts as: the host's own gh login (the run on
// the coordinator's host, deliberately: the one identity that can act on
// every stream repository, intent rule 32), unless Token is set, an
// optional override that gh gets as GH_TOKEN. Name is for the refusals: it
// names the choice (HostLogin or TokenEnv), never a login it expects.
type Identity struct{ Name, Token string }

// HostLogin names the host identity in messages.
const HostLogin = "this host's gh login"

// credentialEnv lists the environment that would make gh act as someone
// other than the identity chosen: the host's login is gh's own config
// under the user's home, so none of these may reach it (directive 59: a
// GH_CONFIG_DIR in the environment defeated the host identity).
var credentialEnv = []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GH_CONFIG_DIR"}

// HostEnv is the process's environment without any GitHub credential of
// its own, so gh run in it acts as the host's own login.
func HostEnv() []string {
	return slices.DeleteFunc(os.Environ(), func(kv string) bool {
		k, _, _ := strings.Cut(kv, "=")
		return slices.Contains(credentialEnv, k)
	})
}

// Unset is credentialEnv as env(1)'s -u arguments: a command run through
// env with them sees no GitHub credential of the process.
func Unset() []string {
	var out []string
	for _, k := range credentialEnv {
		out = append(out, "-u", k)
	}
	return out
}

// TokenEnv is the optional override's environment variable: when it is set
// in nova-sprint's process, the forge and git act as that token instead of
// the host's gh login.
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
	tokens             float64
	lastTick, restTill int64
	methods            map[string]string // repo -> the merge method it allows
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
	return &Client{cfg: cfg, tokens: float64(cfg.Burst), lastTick: cfg.Now(), methods: map[string]string{}}
}
func (c *Client) RestedUntil() int64 { c.mu.Lock(); defer c.mu.Unlock(); return c.restTill }

// env_internal is gh's environment: the process's without any GitHub
// credential of its own (HostEnv: the host's gh login), with GH_TOKEN set
// to the override when there is one.
func (c *Client) env_internal() []string {
	env := append(HostEnv(), "GH_PROMPT_DISABLED=1", "NO_COLOR=1")
	if c.cfg.Identity.Token != "" {
		env = append(env, "GH_TOKEN="+c.cfg.Identity.Token)
	}
	return env
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
	return c.runGHBytes_internal(ctx, 0, argv...)
}

// runGHBytes_internal is runGH_internal with stdout capped at maxBytes (0:
// edge.DefaultMaxBytes).
func (c *Client) runGHBytes_internal(ctx context.Context, maxBytes int, argv ...string) (string, string, error) {
	if err := c.takeToken_internal(); err != nil {
		return "", "", err
	}
	cmd := edge.Cmd{Argv: append([]string{c.cfg.Binary}, argv...), Env: c.env_internal(), CleanEnv: true, Timeout: c.cfg.Timeout, MaxBytes: maxBytes}
	res, err := c.cfg.Exec.Run(ctx, cmd)
	if err == nil && res.OK() {
		return res.Stdout, res.Stderr, nil
	}
	// The failure names none of the environment (it may hold a token).
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
	stdout, _, err := c.runGH_internal(ctx, "pr", "list", "--repo", repo, "--head", head, "--base", base, "--state", "open", "--json", "number,url,headRefName,headRefOid,baseRefName,state,mergeable")
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
		return CheckRollup{SHA: sha, State: CheckRed, Failing: failing, URL: firstRedURL, Waiting: pending, Reason: "CI red: " + strings.Join(failing, ", ")}
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

// Merge merges PR pr by the repository's allowed method (merge commit,
// then squash, then rebase, the first it allows), refused by GitHub unless
// the PR's head is still sha (--match-head-commit), and returns the merge
// commit GitHub recorded. A PR left open (a merge queue took it) is a
// refusal: the landing records only a merge it saw.
func (c *Client) Merge(ctx context.Context, repo string, pr int, sha string) (string, error) {
	if pr <= 0 || !isFullSHA_internal(sha) || repo == "" {
		return "", fmt.Errorf("forge refused: Merge requires a repo, pr > 0 and a 40-hex sha")
	}
	method, err := c.method_internal(ctx, repo)
	if err != nil {
		return "", err
	}
	n := strconv.Itoa(pr)
	if _, _, err := c.runGH_internal(ctx, "pr", "merge", n, "--repo", repo, "--"+method, "--match-head-commit", sha); err != nil {
		return "", err
	}
	stdout, _, err := c.runGH_internal(ctx, "pr", "view", n, "--repo", repo, "--json", "state,mergeCommit")
	if err != nil {
		return "", fmt.Errorf("forge: merged PR #%d on %s but could not read its merge commit: %w", pr, repo, err)
	}
	var v struct {
		State       string `json:"state"`
		MergeCommit *struct {
			OID string `json:"oid"`
		} `json:"mergeCommit"`
	}
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		return "", fmt.Errorf("forge: decode gh pr view --json state,mergeCommit: %s", edge.ParseFailure(err, []byte(stdout)))
	}
	if v.State != "MERGED" || v.MergeCommit == nil || !isFullSHA_internal(v.MergeCommit.OID) {
		return "", fmt.Errorf("forge: gh pr merge %d on %s returned, but the PR is %s with no merge commit (a merge queue or an auto-merge holds it)", pr, repo, cmp.Or(v.State, "in no state"))
	}
	return strings.ToLower(v.MergeCommit.OID), nil
}

// method_internal is the first merge method repo allows, read once per repo.
func (c *Client) method_internal(ctx context.Context, repo string) (string, error) {
	c.mu.Lock()
	m, ok := c.methods[repo]
	c.mu.Unlock()
	if ok {
		return m, nil
	}
	stdout, _, err := c.runGH_internal(ctx, "repo", "view", repo, "--json", "mergeCommitAllowed,squashMergeAllowed,rebaseMergeAllowed")
	if err != nil {
		return "", err
	}
	var v struct{ MergeCommitAllowed, SquashMergeAllowed, RebaseMergeAllowed bool }
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		return "", fmt.Errorf("forge: decode gh repo view --json merge methods: %s", edge.ParseFailure(err, []byte(stdout)))
	}
	switch {
	case v.MergeCommitAllowed:
		m = "merge"
	case v.SquashMergeAllowed:
		m = "squash"
	case v.RebaseMergeAllowed:
		m = "rebase"
	default:
		return "", fmt.Errorf("forge refused: %s allows no merge method (merge commit, squash and rebase are all off)", repo)
	}
	c.mu.Lock()
	c.methods[repo] = m
	c.mu.Unlock()
	return m, nil
}

// FailedLines is the first failing lines of the GitHub Actions job a red
// check's url names (gh run view --job <id> --log-failed): the first five
// lines that say error, fail or panic, else the last five. A url that names
// no job has no lines to read.
func (c *Client) FailedLines(ctx context.Context, repo, url string) ([]string, error) {
	m := jobRe.FindStringSubmatch(url)
	if m == nil {
		return nil, fmt.Errorf("forge: %s names no Actions job, so its log cannot be read", cmp.Or(url, "the check's url"))
	}
	stdout, _, err := c.runGH_internal(ctx, "run", "view", "--repo", repo, "--job", m[1], "--log-failed")
	if err != nil {
		return nil, err
	}
	return FirstFailing(stdout, 5), nil
}

// FirstFailing is the first n lines of log that say error, fail or panic,
// else its last n lines, each trimmed.
func FirstFailing(log string, n int) []string {
	var all, hits []string
	for _, l := range strings.Split(log, "\n") {
		if l = strings.TrimSpace(l); l == "" {
			continue
		}
		all = append(all, l)
		if len(hits) < n && failLineRe.MatchString(l) {
			hits = append(hits, l)
		}
	}
	if len(hits) > 0 {
		return hits
	}
	return all[max(0, len(all)-n):]
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
