package worker

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/usage"
)

type Harness interface {
	Name() string
	Command(j Job) (argv, env []string)
	Probe(j Job) Job
	Classify(exit int, tail []byte) Outcome
	Usage(j Job) (Cost, bool)
	Needs() Needs
}

// Needs is what a harness's wall needs, declared by its adapter (intent rule
// 31): BuildWallArgv honours it, and nothing else widens a wall.
type Needs struct {
	// Unix leaves AF_UNIX sockets open (nova-sandbox --unix), which reopens
	// the sandbox's same-uid escape (any local socket reachable by path). An
	// adapter that sets it says why in its own comment.
	Unix bool
	// Proxy: the harness reaches its provider only through the worker's key
	// proxy: --net-deny with one --net-allow for the proxy's loopback port.
	// NetOpen: it reaches its own provider with its own login (no
	// --net-deny). Neither: the network is denied outright.
	Proxy, NetOpen bool
	// Writes are directories outside the job directory the harness writes
	// (its config and session state).
	Writes []string
	// ListModels is the argv that prints the models the harness serves, one
	// provider/model first on each line; nil: it cannot list them, and the
	// probe's run is the only proof a model serves.
	ListModels []string
	// LogDir is where, under the job directory, the harness writes its own
	// log ("" none): empty after a death in the first second means it died
	// inside the wall before it started.
	LogDir string
	// Provider is the one provider a harness without the proxy serves; a
	// model is passed to it without the "<provider>/" prefix.
	Provider string
	// Model: every run of it is given a model (a probe too).
	Model bool
	// Limits are its known failures inside the wall, said with every failed
	// probe so a refusal names them.
	Limits string
}

func NewHarness(name string) (Harness, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "claude":
		return &ClaudeHarness{}, nil
	case "codex":
		return &CodexHarness{}, nil
	case "opencode":
		return &OpenCodeHarness{}, nil
	default:
		return nil, fmt.Errorf("worker refused: unknown harness %q: use claude, codex or opencode", name)
	}
}

type (
	baseHarness_internal struct{}
	ClaudeHarness        struct {
		baseHarness_internal
		ConfigDir string
	}
	CodexHarness struct {
		baseHarness_internal
		ConfigDir string
	}
	OpenCodeHarness struct {
		baseHarness_internal
		messages func(db string) ([]string, error) // nil: opencodeMessages; a unit test's rows
	}
)

func (baseHarness_internal) Usage(j Job) (Cost, bool) { return readUsageFile_internal(j.JobDir) }
func (baseHarness_internal) Classify(exit int, tail []byte) Outcome {
	return classifyExitAndTail_internal(exit, tail)
}
func (baseHarness_internal) Probe(j Job) Job { return probeJob_internal(j, j.HarnessName) }
func (h *ClaudeHarness) Name() string        { return "claude" }
func (h *CodexHarness) Name() string         { return "codex" }
func (h *OpenCodeHarness) Name() string      { return "opencode" }

// opencodeMessages reads the run's assistant messages from opencode's own
// session database, each row's data JSON (a unit test feeds rows through
// OpenCodeHarness.messages).
// The wall's XDG_DATA_HOME is <job>/data, so the database is the run's alone.
// A run that never opened opencode has no database, and no sqlite3 is started.
func opencodeMessages(db string) ([]string, error) {
	if _, err := os.Stat(db); err != nil {
		return nil, err
	}
	out, err := exec.Command("sqlite3", "-readonly", "-json", db, "select data from message where json_extract(data,'$.role')='assistant'").Output()
	if err != nil {
		return nil, err
	}
	var rows []struct{ Data string }
	if s := strings.TrimSpace(string(out)); s != "" {
		if err := json.Unmarshal([]byte(s), &rows); err != nil {
			return nil, err
		}
	}
	data := make([]string, len(rows))
	for i, r := range rows {
		data[i] = r.Data
	}
	return data, nil
}

// opencodeMessage is what one assistant message's data carries: its tokens
// by class, its model and its cost (opencode's own price, a float).
type opencodeMessage struct {
	ModelID    string  `json:"modelID"`
	ProviderID string  `json:"providerID"`
	Cost       float64 `json:"cost"`
	Tokens     struct {
		Input, Output, Reasoning int64
		Cache                    struct{ Read, Write int64 }
	} `json:"tokens"`
}

// Usage is usage.json when the run wrote one, else what opencode recorded for
// the run: its exact tokens by class summed over every assistant message
// (the words of package usage, which the engine prices), the model with the
// most tokens (models=<n> when the run used more than one), and opencode's
// own cost summed as the charged evidence. A run that recorded no token and
// no cost reports nothing: unpriced, never a priced zero.
func (h *OpenCodeHarness) Usage(j Job) (Cost, bool) {
	if c, ok := readUsageFile_internal(j.JobDir); ok {
		return c, true
	}
	if j.JobDir == "" {
		return Cost{}, false
	}
	messages := h.messages
	if messages == nil {
		messages = opencodeMessages
	}
	rows, err := messages(filepath.Join(j.JobDir, "data", "opencode", "opencode.db"))
	if err != nil {
		return Cost{}, false
	}
	return opencodeUsage_internal(rows)
}

// opencodeUsage_internal sums opencode's assistant messages (data JSON each).
func opencodeUsage_internal(rows []string) (Cost, bool) {
	t := usage.None()
	byModel := map[string]int64{}
	usd := 0.0
	for _, row := range rows {
		var m opencodeMessage
		if json.Unmarshal([]byte(row), &m) != nil {
			continue
		}
		mt := usage.Tokens{Input: m.Tokens.Input, CacheRead: m.Tokens.Cache.Read, CacheWrite: m.Tokens.Cache.Write, Output: m.Tokens.Output, Reasoning: m.Tokens.Reasoning}
		t = t.Add(mt)
		if model := strings.Trim(m.ProviderID+"/"+m.ModelID, "/"); model != "" {
			byModel[model] += mt.Total()
		}
		usd += max(m.Cost, 0)
	}
	r := usage.Record{Tokens: t}
	for _, model := range slices.Sorted(maps.Keys(byModel)) {
		if r.Model == "" || byModel[model] > byModel[r.Model] {
			r.Model = model
		}
	}
	if len(byModel) > 1 {
		r.Models = len(byModel)
	}
	c := Cost{MicroUSD: int64(math.Round(usd * 1e6))}
	c.Priced = c.MicroUSD > 0
	if t.Reported() {
		c.Usage = r.String()
	}
	return c, c.Priced || c.Usage != ""
}

// Needs: claude reaches its provider itself with the login in its config
// directory, which it writes (session state), so its network stays open
// and the directory is a --write. It has no command that lists its models.
// Whether it needs AF_UNIX inside the wall is not measured: no --unix.
func (h *ClaudeHarness) Needs() Needs {
	return Needs{NetOpen: true, Writes: []string{h.ConfigDir}, Provider: "anthropic", Model: true}
}

// Needs: codex as claude, with its home (CODEX_HOME) the --write; not
// measured inside the wall.
func (h *CodexHarness) Needs() Needs {
	return Needs{NetOpen: true, Writes: []string{h.ConfigDir}, Provider: "openai", Model: true}
}

// Needs: opencode's own in-process server listens on an AF_UNIX socket;
// without --unix the wall kills it at startup ("Unexpected server error")
// before it writes its log (measured on a bench, 2026-11-07). --unix reopens
// nova-sandbox's same-uid escape (any local socket reachable by path): that
// trade is accepted for this adapter only. It reaches providers through the
// key proxy (one loopback --net-allow, OpenCodeConfig), logs under its data
// home, and lists its models with `opencode models`.
func (h *OpenCodeHarness) Needs() Needs {
	return Needs{Unix: true, Proxy: true, ListModels: []string{"opencode", "models"}, LogDir: filepath.Join("data", "opencode", "log"), Model: true,
		Limits: "opencode 2.x starts a background service on a fixed port unless given --standalone, which 1.x refuses and this adapter does not pass: " +
			"run opencode 1.x (1.18.20 measured); its write tool's death inside the wall (defect 33) was seen before --unix and not since on macOS (2026-11-08), unmeasured on Linux"}
}

// modelFlag_internal passes the model the take receipt (or the probe) named,
// without the provider prefix for a harness that serves one provider. The
// adapters hold no model of their own: a job with none runs the harness's
// own configured default.
func modelFlag_internal(j Job, provider string) []string {
	if j.Model == "" {
		return nil
	}
	if p, m, ok := strings.Cut(j.Model, "/"); ok && provider != "" && p == provider {
		return []string{"--model", m}
	}
	return []string{"--model", j.Model}
}
func (h *ClaudeHarness) Command(j Job) ([]string, []string) {
	argv := append(append([]string{"claude", "--print", "--dangerously-skip-permissions", "--disallowedTools", "Task"}, modelFlag_internal(j, h.Needs().Provider)...), "--", buildPrompt_internal(j))
	env := baseWallEnv_internal(j)
	if h.ConfigDir != "" {
		env = append(env, "CLAUDE_CONFIG_DIR="+h.ConfigDir)
	}
	return argv, SanitizeWallEnv(env, j.JobDir)
}
func (h *CodexHarness) Command(j Job) ([]string, []string) {
	wt := cmp.Or(j.WorktreeDir, filepath.Join(j.JobDir, "repo"))
	argv := append(append([]string{"codex", "exec", "--full-auto"}, modelFlag_internal(j, h.Needs().Provider)...), "--cd", wt, buildPrompt_internal(j))
	env := baseWallEnv_internal(j)
	if h.ConfigDir != "" {
		env = append(env, "CODEX_HOME="+h.ConfigDir)
	}
	return argv, SanitizeWallEnv(env, j.JobDir)
}

// Command: opencode is given OpenCodeConfig for the job's model (and the
// probe listing's Models) on the proxy's Routes in OPENCODE_CONFIG_CONTENT,
// so one proxy serves every provider.
func (h *OpenCodeHarness) Command(j Job) ([]string, []string) {
	argv := append(append([]string{"opencode", "run"}, modelFlag_internal(j, "")...), buildPrompt_internal(j))
	env := baseWallEnv_internal(j)
	if cfg := OpenCodeConfig(j.ProxyAddr, j.Consumer, j.Routes, append(slices.Clone(j.Models), j.Model)); j.ProxyAddr != "" && cfg != "" {
		env = append(env, "OPENCODE_CONFIG_CONTENT="+cfg)
	}
	return argv, SanitizeWallEnv(env, j.JobDir)
}

type (
	opencodeOptions_internal struct {
		BaseURL string `json:"baseURL"`
		APIKey  string `json:"apiKey"`
	}
	opencodeProvider_internal struct {
		NPM     string                   `json:"npm"`
		Name    string                   `json:"name"`
		Options opencodeOptions_internal `json:"options"`
		Models  map[string]struct{}      `json:"models"`
	}
	opencodePermission_internal struct {
		ExternalDirectory string `json:"external_directory"`
	}
	opencodeConfig_internal struct {
		Schema     string                               `json:"$schema"`
		Permission opencodePermission_internal          `json:"permission"`
		Provider   map[string]opencodeProvider_internal `json:"provider"`
	}
)

const (
	opencodeSchema_internal = "https://opencode.ai/config.json"
	opencodeNPM_internal    = "@ai-sdk/openai-compatible"
)

// OpenCodeConfig is opencode's config (JSON, its documented custom-provider
// form; opencode 1.18 and 2.2 read it from OPENCODE_CONFIG_CONTENT) declaring
// each provider/model of models whose provider is a provider name among
// routes, under an OpenAI-compatible provider whose base URL is the key
// proxy's route for it (ProxyBaseURL) and whose key is the placeholder the
// proxy swaps for the real one; "" when none is. It allows
// external_directory: opencode runs in <job>/repo and writes result.json to
// <job>, which `opencode run` otherwise auto-rejects; the wall is the
// filesystem boundary.
func OpenCodeConfig(proxyAddr, consumer string, routes, models []string) string {
	cfg := opencodeConfig_internal{Schema: opencodeSchema_internal, Permission: opencodePermission_internal{ExternalDirectory: "allow"}, Provider: map[string]opencodeProvider_internal{}}
	for _, m := range models {
		provider, id, ok := strings.Cut(m, "/")
		if !ok || id == "" || !providerName_internal.MatchString(provider) || !slices.Contains(routes, provider) {
			continue
		}
		p, seen := cfg.Provider[provider]
		if !seen {
			p = opencodeProvider_internal{NPM: opencodeNPM_internal, Name: provider,
				Options: opencodeOptions_internal{BaseURL: ProxyBaseURL(proxyAddr, m), APIKey: ProxyKey(consumer)}, Models: map[string]struct{}{}}
		}
		p.Models[id] = struct{}{}
		cfg.Provider[provider] = p
	}
	if len(cfg.Provider) == 0 {
		return ""
	}
	b, _ := json.Marshal(cfg) // strings and empty structs: never fails
	return string(b)
}

// openCodeConfigRefusal_internal is why the wall refuses v, nil when v is
// exactly an OpenCodeConfig, the only config it admits: re-encoded it is v
// (no field beside its own, each model {}), its one permission is
// external_directory "allow", and each provider is a provider
// name with the openai-compatible package, its own name, the placeholder
// key, and the base URL of its route on one loopback proxy. The reason
// never quotes v.
func openCodeConfigRefusal_internal(v string) error {
	var cfg opencodeConfig_internal
	if json.Unmarshal([]byte(v), &cfg) != nil || cfg.Schema != opencodeSchema_internal || len(cfg.Provider) == 0 {
		return errors.New("it is not an OpenCodeConfig with a provider")
	}
	if cfg.Permission.ExternalDirectory != "allow" {
		return errors.New(`its permission is not external_directory "allow"`)
	}
	proxy := ""
	for _, name := range slices.Sorted(maps.Keys(cfg.Provider)) {
		p := cfg.Provider[name]
		if !providerName_internal.MatchString(name) {
			return errors.New("a provider is not a provider name")
		}
		u, _ := url.Parse(p.Options.BaseURL) // unparsable is nil, refused next; its error would quote the config
		if u == nil || u.Scheme != "http" || CheckLoopbackAddr(u.Host) != nil || (proxy != "" && u.Host != proxy) {
			return fmt.Errorf("provider %s: its baseURL is not on a loopback http proxy (the worker's --proxy-addr)", name)
		}
		proxy = u.Host
		if p.NPM != opencodeNPM_internal || p.Name != name || !proxyKey_internal.MatchString(p.Options.APIKey) || p.Options.BaseURL != ProxyBaseURL(proxy, name+"/") {
			return fmt.Errorf("provider %s: its package, name, key or baseURL is not the proxy route's", name)
		}
	}
	if b, _ := json.Marshal(cfg); string(b) != v { // strings and empty structs: never fails
		return errors.New("it holds fields beside an OpenCodeConfig's own")
	}
	return nil
}

// WallRefusal is why the wall drops this job's opencode config (nil: none
// to give, or admitted), said by the runner so a disagreement between
// OpenCodeConfig and the wall's check is never a bare "not in the
// harness's model list".
func (h *OpenCodeHarness) WallRefusal(j Job) error {
	cfg := OpenCodeConfig(j.ProxyAddr, j.Consumer, j.Routes, append(slices.Clone(j.Models), j.Model))
	if j.ProxyAddr == "" || cfg == "" {
		return nil
	}
	if err := openCodeConfigRefusal_internal(cfg); err != nil {
		return fmt.Errorf("the opencode config was refused at the wall: %w: OpenCodeConfig and SanitizeWallEnv disagree; fix them together", err)
	}
	return nil
}

// wallRefusal_internal is the harness's WallRefusal for j, nil for a
// harness without one.
func wallRefusal_internal(h Harness, j Job) error {
	if w, ok := h.(interface{ WallRefusal(Job) error }); ok {
		return w.WallRefusal(j)
	}
	return nil
}

func probeJob_internal(j Job, harness string) Job {
	p := j
	p.Consumer, p.Kind, p.HarnessName, p.DoneWhen = j.Row+".probe", "work", harness, nil
	p.Prose = `Reply READY and write {"result":"ok"} to result.json.`
	if j.JobDir != "" {
		p.JobDir = filepath.Join(filepath.Dir(j.JobDir), p.Consumer)
		p.WorktreeDir = filepath.Join(p.JobDir, "repo")
	}
	return p
}
func buildPrompt_internal(j Job) string {
	var b strings.Builder
	fmt.Fprintf(&b, "consumer=%s kind=%s stream=%s tier=%s\n", j.Consumer, j.Kind, j.Stream, j.Tier)
	if j.Kind == "read" {
		fmt.Fprintf(&b, "mode=read head=%s (detached checkout; do not edit tracked files)\n", j.Head)
	} else {
		fmt.Fprintf(&b, "branch=%s base_sha=%s prior_head=%s\n", j.Branch, j.BaseSHA, j.PriorHead)
	}
	for _, kv := range [][2]string{{"prior_finding=", j.Finding}, {"paths=", strings.Join(j.Paths, ",")}, {"done_when=", strings.Join(j.DoneWhen, " && ")}} {
		if kv[1] != "" {
			b.WriteString(kv[0] + kv[1] + "\n")
		}
	}
	if body := cmp.Or(j.Prose, j.Brief); body != "" {
		b.WriteString("\n" + body + "\n")
	}
	if j.Repo == "" { // the probe: no card, no JOB.md
		fmt.Fprintf(&b, "\nWrite final verdict JSON to %s/result.json.\n", j.JobDir)
		return b.String()
	}
	fmt.Fprintf(&b, "\nRead %s first: it says what this card is and how it finishes.\n\n%s", filepath.Join(j.JobDir, JobMDName), finishRules_internal(j))
	return b.String()
}
func classifyExitAndTail_internal(exit int, tail []byte) Outcome {
	text := TailBytes(tail, MaxReasonBytes)
	if exit == 134 || exit == 137 {
		return Outcome{Result: "fault", Reason: fmt.Sprintf("harness killed with exit %d: %s", exit, text)}
	}
	var ev struct {
		Result, Reason, Finding string
		HTTPStatus              int `json:"http_status"`
	}
	if json.Unmarshal(tail, &ev) == nil {
		if ev.Result != "" {
			return normalizeFileOutcome_internal(ev.Result, ev.Reason, ev.Finding, exit, text)
		}
		if out, ok := classifyHTTPStatus_internal(ev.HTTPStatus, 0, 0); ok {
			return out
		}
	}
	return noVerdict_internal(fmt.Sprintf("the harness exited %d without writing result.json", exit), text)
}
func classifyHTTPStatus_internal(status int, elapsed, launchGrace time.Duration) (Outcome, bool) {
	if status == 402 || status == 429 || status >= 500 {
		return Outcome{Result: "fault", Reason: fmt.Sprintf("provider HTTP %d", status), Retryable5xx: status >= 500 && (launchGrace <= 0 || elapsed <= launchGrace)}, true
	}
	return Outcome{}, false
}
func ClassifyJob(h Harness, j Job, exit int, elapsed, launchGrace time.Duration, tail []byte) Outcome {
	text := TailBytes(tail, MaxReasonBytes)
	if exit == 134 || exit == 137 {
		return Outcome{Result: "fault", Reason: fmt.Sprintf("harness killed with exit %d: %s", exit, text)}
	}
	// A result.json that does not parse is said in the reason: the job is
	// classified as if there were none, but never as "no result.json".
	badFile := ""
	if j.JobDir != "" {
		if data, err := os.ReadFile(filepath.Join(j.JobDir, "result.json")); err == nil {
			var f Outcome
			if err := json.Unmarshal(data, &f); err != nil {
				badFile = "; result.json: " + edge.ParseFailure(err, data)
			} else if f.Result != "" {
				return normalizeFileOutcome_internal(f.Result, f.Reason, f.Finding, exit, text)
			}
		}
	}
	if out, ok := classifyHTTPStatus_internal(j.ProxyStatus, elapsed, launchGrace); ok {
		out.Reason += badFile
		return out
	}
	out := h.Classify(exit, tail)
	out.Reason += badFile
	if out.Result == "fault" && out.Retryable5xx && launchGrace > 0 && elapsed > launchGrace {
		out.Retryable5xx = false
	}
	return out
}
func normalizeFileOutcome_internal(res, reason, finding string, exit int, tail string) Outcome {
	switch r := strings.ToLower(strings.TrimSpace(res)); r {
	case "ok", "yes":
		if exit != 0 {
			return Outcome{Result: "fail", Reason: fmt.Sprintf("result.json says %s but exit was %d: %s", r, exit, tail), NoVerdict: true}
		}
		return Outcome{Result: "ok", Reason: reason}
	case "fix":
		return Outcome{Result: "fix", Reason: cmp.Or(reason, finding), Finding: cmp.Or(finding, reason)}
	case "broken", "fail":
		r := cmp.Or(reason, tail, "harness reported broken")
		if isBlockedWorkReason_internal(r) {
			return Outcome{Result: "fault", Reason: r}
		}
		return Outcome{Result: "fail", Reason: r}
	case "fault":
		return Outcome{Result: "fault", Reason: cmp.Or(reason, tail, "harness reported fault")}
	default:
		return Outcome{Result: "fail", Reason: fmt.Sprintf("unknown verdict %q in result.json", res), NoVerdict: true}
	}
}

func isBlockedWorkReason_internal(reason string) bool {
	low := strings.ToLower(strings.TrimSpace(reason))
	if strings.HasPrefix(low, "missing-route:") {
		return true
	}
	for _, needle := range []string{
		"does not resolve on this bench",
		"toolchain is not installed",
		"outside allowed paths",
		"outside card paths",
	} {
		if strings.Contains(low, needle) {
			return true
		}
	}
	return false
}

// usageFile is usage.json, a harness's own report of a run: its dollars
// (micro_usd, priced) and, when it counted them, its tokens by class and
// model, or the words of package usage whole (usage).
type usageFile struct {
	Cost
	Tokens *struct {
		Input      *int64 `json:"input"`
		CacheRead  *int64 `json:"cache_read"`
		CacheWrite *int64 `json:"cache_write"`
		Output     *int64 `json:"output"`
		Reasoning  *int64 `json:"reasoning"`
	} `json:"tokens"`
	Model string `json:"model"`
}

func readUsageFile_internal(jobDir string) (Cost, bool) {
	data, err := os.ReadFile(filepath.Join(jobDir, "usage.json"))
	var f usageFile
	if jobDir == "" || err != nil || json.Unmarshal(data, &f) != nil {
		return Cost{}, false
	}
	c := f.Cost
	if r, err := usage.Parse(c.Usage); err != nil {
		c.Usage = "" // words that do not read are no usage, never a refused report
	} else if c.Usage == "" && f.Tokens != nil {
		n := func(p *int64) int64 {
			if p == nil || *p < 0 {
				return usage.Unreported
			}
			return *p
		}
		r.Tokens = usage.Tokens{Input: n(f.Tokens.Input), CacheRead: n(f.Tokens.CacheRead), CacheWrite: n(f.Tokens.CacheWrite), Output: n(f.Tokens.Output), Reasoning: n(f.Tokens.Reasoning)}
		r.Model = strings.Join(strings.Fields(f.Model), "_")
		if r.Tokens.Reported() {
			c.Usage = r.String()
		}
	}
	return c, c.Priced || c.MicroUSD > 0 || c.Usage != ""
}

type WallConfig struct {
	ReserveBin, ReserveDir, SandboxBin string
	ReserveWait                        time.Duration
	ReadPaths, WritePaths              []string
	NetDeny                            bool
	// WorkRoot and MirrorRoot are the worker's own clones a job's tree
	// borrows objects from (clone --shared): the wall reads the job repo's
	// <WorkRoot>/<name>/.git and <MirrorRoot>/<name>.git, worked out from
	// the job's repo, never from what the job's tree says.
	WorkRoot, MirrorRoot string
	// GoRoot and GoModCache are what `go env` said outside the wall at
	// start (DetectGo); GoWallEnv sets them inside it only under a
	// ReadPaths grant.
	GoRoot, GoModCache string
	// CargoRegistry is the bench's ~/.cargo/registry, granted at start;
	// CargoWallEnv links it into the job's own cargo home, offline.
	CargoRegistry string
	// GOOS is the OS the wall is built for; "" is runtime.GOOS.
	GOOS string
}

// wallKeeps are the names nova-sandbox passes into the wall without --env;
// every other name the harness's environment holds is named with --env, or
// the wall drops it (nova-sandbox help).
var wallKeeps_internal = []string{"PATH", "HOME", "LANG", "TERM"}

// BuildWallArgv is nova-reserve around nova-sandbox around the harness: the
// job directory and the adapter's Needs (j.Needs) written, cfg's paths
// read, --unix (Linux only: nova-sandbox refuses it on macOS, where a socket
// under a --write is reachable) and the network as the adapter declares
// (under cfg.NetDeny: open, denied with the proxy's port reopened, or
// denied), and every name of env passed with --env.
func BuildWallArgv(cfg WallConfig, j Job, env, harnessArgv []string) []string {
	// Card work runs at nice 15, below CI's 0, and the harness, its builds
	// and its gates inherit it, so a CI job on a shared bench keeps its cap.
	var argv []string
	if runtime.GOOS != "windows" {
		argv = append(argv, "nice", "-n", "15")
	}
	argv = append(argv, cmp.Or(cfg.ReserveBin, "nova-reserve"), "run")
	if cfg.ReserveDir != "" {
		argv = append(argv, "--dir", cfg.ReserveDir)
	}
	if j.SlotWeight > 0 {
		argv = append(argv, "--slots", strconv.Itoa(j.SlotWeight))
	}
	argv = append(argv, "--wait", cmp.Or(cfg.ReserveWait, 5*time.Second).String(), "--why", cmp.Or(j.Consumer, "probe"), "--", cmp.Or(cfg.SandboxBin, "nova-sandbox"))
	argv = append(argv, sandboxFlags_internal(cfg, j, env)...)
	return append(append(argv, "--"), harnessArgv...)
}

// sandboxFlags_internal are nova-sandbox's flags for job j (BuildWallArgv).
func sandboxFlags_internal(cfg WallConfig, j Job, env []string) []string {
	var flags []string
	add := func(flag string, paths []string) {
		for _, p := range paths {
			if p != "" && !slices.Contains(flags, p) {
				flags = append(flags, flag, p)
			}
		}
	}
	add("--write", append([]string{j.JobDir}, append(slices.Clone(cfg.WritePaths), j.Needs.Writes...)...))
	add("--read", cfg.ReadPaths)
	if j.Needs.Unix && cmp.Or(cfg.GOOS, runtime.GOOS) != "darwin" {
		flags = append(flags, "--unix")
	}
	if cfg.NetDeny && !j.Needs.NetOpen {
		flags = append(flags, "--net-deny")
		if j.Needs.Proxy && j.ProxyAddr != "" {
			flags = append(flags, "--net-allow", j.ProxyAddr)
		}
	}
	for _, kv := range env {
		if name, _, ok := strings.Cut(kv, "="); ok && name != "" && !slices.Contains(wallKeeps_internal, strings.ToUpper(name)) && !slices.Contains(flags, name) {
			flags = append(flags, "--env", name)
		}
	}
	return flags
}

// SandboxShow is the command that prints the wall argv (a BuildWallArgv)
// applies, without running anything: nova-sandbox show, its flags, and the
// harness binary alone (its prompt left out).
func SandboxShow(argv []string) string {
	i := slices.IndexFunc(argv, func(a string) bool { return filepath.Base(a) == "nova-sandbox" })
	if i < 0 {
		return "nova-sandbox show"
	}
	rest := argv[i+1:]
	cut := slices.Index(rest, "--")
	if cut < 0 {
		return "nova-sandbox show " + strings.Join(rest, " ")
	}
	show := append([]string{"nova-sandbox", "show"}, rest[:cut]...)
	if cut+1 < len(rest) {
		show = append(show, "--", rest[cut+1])
	}
	return strings.Join(show, " ")
}

// HarnessDirs are the directories the wall must grant --read for the
// harness binary to start (defect 24): the directory lookPath finds it in
// and, when that is a symlink, the directory of the file it names. A name
// lookPath cannot find is nil: the wall then refuses it, naming the path.
func HarnessDirs(name string, lookPath func(string) (string, error)) []string {
	p, err := lookPath(name)
	if err != nil || p == "" {
		return nil
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	dirs := []string{filepath.Dir(p)}
	if real, err := filepath.EvalSymlinks(p); err == nil && filepath.Dir(real) != dirs[0] {
		dirs = append(dirs, filepath.Dir(real))
	}
	return dirs
}
func baseWallEnv_internal(j Job) []string {
	env := []string{
		"PATH=" + cmp.Or(os.Getenv("PATH"), "/usr/local/bin:/usr/bin:/bin"), "LANG=" + cmp.Or(os.Getenv("LANG"), "C.UTF-8"),
		"HOME=" + filepath.Join(j.JobDir, "home"), "TMPDIR=" + filepath.Join(j.JobDir, "tmp"), "XDG_DATA_HOME=" + filepath.Join(j.JobDir, "data"),
		"GIT_TERMINAL_PROMPT=0", "NOVA_WORKER_CONSUMER=" + j.Consumer, "NOVA_WORKER_ROW=" + j.Row,
	}
	if os.Getenv("DOTNET_CLI_TELEMETRY_OPTOUT") != "" {
		env = append(env, "DOTNET_CLI_TELEMETRY_OPTOUT=1", "DOTNET_NOLOGO=1")
	}
	return env
}
func SanitizeWallEnv(env []string, jobDir string) []string {
	var clean []string
	hasPath := false
	hasDotnetOptout := false
	allowed := []string{"LANG", "LC_ALL", "GIT_TERMINAL_PROMPT", "NOVA_WORKER_CONSUMER", "NOVA_WORKER_ROW", "NOVA_WORKER_NONCE", "CLAUDE_CONFIG_DIR", "CODEX_HOME", "DOTNET_CLI_TELEMETRY_OPTOUT", "DOTNET_NOLOGO"}
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		u := strings.ToUpper(k)
		switch {
		case !ok:
		case u == "PATH":
			hasPath, clean = true, append(clean, kv)
		case u == "OPENCODE_CONFIG_CONTENT" && openCodeConfigRefusal_internal(v) == nil:
			clean = append(clean, kv)
		case (u == "HOME" || u == "TMPDIR" || u == "XDG_DATA_HOME" || u == "DOTNET_CLI_HOME") && jobDir == "":
			clean = append(clean, kv)
		case slices.Contains(allowed, u):
			if u == "DOTNET_CLI_TELEMETRY_OPTOUT" {
				hasDotnetOptout = true
			}
			clean = append(clean, kv)
		}
	}
	if !hasPath {
		clean = append(clean, "PATH="+cmp.Or(os.Getenv("PATH"), "/usr/local/bin:/usr/bin:/bin"))
	}
	if jobDir != "" {
		clean = append(clean, "HOME="+filepath.Join(jobDir, "home"), "TMPDIR="+filepath.Join(jobDir, "tmp"), "XDG_DATA_HOME="+filepath.Join(jobDir, "data"))
		if hasDotnetOptout {
			clean = append(clean, "DOTNET_CLI_HOME="+filepath.Join(jobDir, "home"))
		}
	}
	return clean
}

// CheckWallReads is the operator's --wall-read directories as the wall
// grants them, read-only, to every job and probe (WallConfig.ReadPaths),
// each resolved through its symlinks; or the refusal of the first that
// would let a job read what is not a toolchain's: a relative path, one that
// is not a directory, the home or a directory holding it, anything under a
// dot-directory of the home (credentials and configuration, a secrets
// store's default among them), or one that holds or lies under a directory
// of the worker's own (own: a flag and its directory; a job would read
// other jobs' trees or the worker's state).
func CheckWallReads(reads []string, home string, own [][2]string) ([]string, error) {
	var out []string
	homeR := resolvePath_internal(home)
	for _, raw := range reads {
		d := strings.TrimSpace(raw)
		if err := checkWallReadForm_internal(d); err != nil {
			return nil, err
		}
		r := resolvePath_internal(d)
		if homeR != "" && pathUnder_internal(homeR, r) {
			return nil, fmt.Errorf("worker refused: --wall-read %s is the worker's home or holds it, and the home holds its credentials and state: grant the toolchain's own directory under it", d)
		}
		if rel, err := filepath.Rel(homeR, r); homeR != "" && err == nil && pathUnder_internal(r, homeR) && strings.HasPrefix(rel, ".") {
			return nil, fmt.Errorf("worker refused: --wall-read %s is under %s, a dot-directory of the home, where credentials and configuration live: grant a directory outside it", d, filepath.Join(home, strings.Split(rel, string(filepath.Separator))[0]))
		}
		for _, o := range own {
			if oR := resolvePath_internal(o[1]); o[1] != "" && (pathUnder_internal(r, oR) || pathUnder_internal(oR, r)) {
				return nil, fmt.Errorf("worker refused: --wall-read %s overlaps the worker's own %s %s, and a job would read other jobs' trees or the worker's state: grant only a toolchain's directory", d, o[0], o[1])
			}
		}
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out, nil
}

// checkWallReadForm_internal refuses a --wall-read that is not an absolute
// path to an existing directory.
func checkWallReadForm_internal(d string) error {
	if !filepath.IsAbs(d) {
		return fmt.Errorf("worker refused: --wall-read %q is not an absolute path: pass the directory's absolute path", d)
	}
	st, err := os.Stat(d)
	if err != nil {
		return fmt.Errorf("worker refused: --wall-read %s is not a directory on this bench: pass a directory that exists: %w", d, err)
	}
	if !st.IsDir() {
		return fmt.Errorf("worker refused: --wall-read %s is not a directory on this bench: pass a directory, not a file", d)
	}
	return nil
}

// resolvePath_internal is p cleaned and, when it exists, through its
// symlinks; "" stays "".
func resolvePath_internal(p string) string {
	if p == "" {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// pathUnder_internal is true when p is dir or under it (both resolved).
func pathUnder_internal(p, dir string) bool {
	if p == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// goWallGrant_internal is the Go toolchain's names inside the wall, and
// the worker's start line about them. cfg.GoRoot and cfg.GoModCache are
// what `go env` said outside the wall at start (DetectGo), granted then
// (osWallConfig_internal); each is admitted only when a grant in
// cfg.ReadPaths covers it, so they widen nothing: GOROOT with
// GOTOOLCHAIN=local (no toolchain download), and GOMODCACHE with
// GOPROXY=off (the cache is read-only and the wall has no network: a
// module missing from it fails at once, named). GOCACHE is left to go's
// default under the job's own home.
func goWallGrant_internal(cfg WallConfig) (vars, notes []string) {
	root, cache := resolvePath_internal(cfg.GoRoot), resolvePath_internal(cfg.GoModCache)
	granted := func(p string) bool {
		return filepath.IsAbs(p) && slices.ContainsFunc(cfg.ReadPaths, func(d string) bool { return pathUnder_internal(p, resolvePath_internal(d)) })
	}
	if root == "" || !granted(root) {
		return nil, nil
	}
	vars = []string{"GOROOT=" + root, "GOTOOLCHAIN=local"}
	line := "worker: the wall reads go: GOROOT=" + root
	if cache != "" && granted(cache) {
		vars = append(vars, "GOMODCACHE="+cache, "GOPROXY=off")
		line += " GOMODCACHE=" + cache + " (GOPROXY=off)"
	}
	return vars, []string{line + " GOTOOLCHAIN=local"}
}

// GoWallEnv is env (a sanitized wall environment) with the Go toolchain's
// granted names (goWallGrant_internal) and GOROOT/bin first on its PATH;
// env as it is when go is not granted.
func GoWallEnv(env []string, cfg WallConfig) []string {
	vars, _ := goWallGrant_internal(cfg)
	if len(vars) == 0 {
		return env
	}
	bin := filepath.Join(strings.TrimPrefix(vars[0], "GOROOT="), "bin")
	out := make([]string, 0, len(env)+len(vars))
	for _, kv := range env {
		name, val, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(name) {
		case "GOROOT", "GOTOOLCHAIN", "GOMODCACHE", "GOPROXY":
		case "PATH":
			out = append(out, "PATH="+bin+string(os.PathListSeparator)+val)
		default:
			out = append(out, kv)
		}
	}
	return append(out, vars...)
}

// CargoWallEnv is env with CARGO_NET_OFFLINE=true, and the job's cargo home
// (<jobDir>/home/.cargo, from the wall's HOME) given a registry link to
// cfg.CargoRegistry, when a grant in cfg.ReadPaths covers it: the registry is
// read-only and the wall has no network, so a crate the bench holds builds
// and one it lacks fails at once, named, as GOMODCACHE does for go. A path
// already at the link is left as it is; env as it is when not granted. The
// link is made through an os.Root on jobDir: the job owns its tree between
// runs, and a symlink it plants there never leads the worker outside it.
func CargoWallEnv(env []string, cfg WallConfig, jobDir string) ([]string, error) {
	reg := resolvePath_internal(cfg.CargoRegistry)
	if reg == "" || jobDir == "" || !slices.ContainsFunc(cfg.ReadPaths, func(d string) bool { return pathUnder_internal(reg, resolvePath_internal(d)) }) {
		return env, nil
	}
	root, err := os.OpenRoot(jobDir)
	if err != nil {
		return env, fmt.Errorf("worker: opening the job directory to link cargo's registry: %w", err)
	}
	defer root.Close()
	link := filepath.Join("home", ".cargo", "registry")
	if _, err := root.Lstat(link); errors.Is(err, fs.ErrNotExist) {
		if err := root.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			return env, fmt.Errorf("worker: making the job's cargo home: %w", err)
		}
		if err := root.Symlink(reg, link); err != nil {
			return env, fmt.Errorf("worker: linking the bench's cargo registry into the job's cargo home: %w", err)
		}
	} else if err != nil {
		return env, fmt.Errorf("worker: the job's cargo home: %w", err)
	}
	return append(env, "CARGO_NET_OFFLINE=true"), nil
}

// DetectGo is `go env GOROOT GOMODCACHE` on the worker's PATH, outside the
// wall, once at start: both "" when go is not on it (a bench without Go).
func DetectGo(ctx context.Context, ex edge.Exec, timeout time.Duration) (goroot, modcache string, err error) {
	argv := []string{"go", "env", "GOROOT", "GOMODCACHE"}
	res, runErr := ex.Run(ctx, edge.Cmd{Argv: argv, Timeout: timeout})
	var ee *edge.ExecError
	if errors.As(runErr, &ee) && ee.Kind == edge.ErrMissingBinary {
		return "", "", nil
	}
	if runErr != nil || res.ExitCode != 0 {
		return "", "", edge.Failure(argv, nil, res.ExitCode, res.Stdout, res.Stderr, runErr)
	}
	lines := strings.Split(strings.TrimSpace(res.Stdout), "\n")
	if len(lines) != 2 {
		return "", "", fmt.Errorf("%s printed %d lines, want GOROOT and GOMODCACHE: %q", strings.Join(argv, " "), len(lines), TailBytes([]byte(res.Stdout), 200))
	}
	return strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1]), nil
}
