package worker

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
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
	case "script":
		return &ScriptHarness{}, nil
	default:
		return nil, fmt.Errorf("worker refused: unknown harness %q: use claude, codex, opencode, or script", name)
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
	OpenCodeHarness struct{ baseHarness_internal }
	ScriptHarness   struct{ baseHarness_internal }
)

func (baseHarness_internal) Usage(j Job) (Cost, bool) { return readUsageFile_internal(j.JobDir) }
func (baseHarness_internal) Classify(exit int, tail []byte) Outcome {
	return classifyExitAndTail_internal(exit, tail)
}
func (baseHarness_internal) Probe(j Job) Job { return probeJob_internal(j, j.HarnessName) }
func (h *ClaudeHarness) Name() string        { return "claude" }
func (h *CodexHarness) Name() string         { return "codex" }
func (h *OpenCodeHarness) Name() string      { return "opencode" }
func (h *ScriptHarness) Name() string        { return "script" }
func (h *ScriptHarness) Probe(j Job) Job {
	p := probeJob_internal(j, "script")
	p.ScriptMode = "probe"
	return p
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
// key proxy (one loopback --net-allow), logs under its data home, and lists
// its models with `opencode models`.
func (h *OpenCodeHarness) Needs() Needs {
	return Needs{Unix: true, Proxy: true, ListModels: []string{"opencode", "models"}, LogDir: filepath.Join("data", "opencode", "log"), Model: true,
		Limits: "through the key proxy opencode needs a provider config in the wall home mapping the proxy's routes, which is not built: a model sent " +
			"through OPENAI_BASE_URL dies with \"Unexpected server error\"; and its own write tool dies inside the wall with the same words (cause unknown)"}
}

// Needs: the script harness is the test double: no network, no model.
func (h *ScriptHarness) Needs() Needs { return Needs{} }

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

// Command: the proxy's base URL names the route of the model's provider
// (http://<proxy>/<provider>/v1), so one proxy serves every provider.
func (h *OpenCodeHarness) Command(j Job) ([]string, []string) {
	argv := append(append([]string{"opencode", "run"}, modelFlag_internal(j, "")...), buildPrompt_internal(j))
	env := baseWallEnv_internal(j)
	if j.ProxyAddr != "" {
		env = append(env, "OPENAI_BASE_URL="+ProxyBaseURL(j.ProxyAddr, j.Model), "OPENAI_API_KEY="+ProxyPlaceholderKey)
	}
	return argv, SanitizeWallEnv(env, j.JobDir)
}

const scriptHarnessProg_internal = `set -e
op="$1"; wt="$2"; res="$3"; use="$4"; a1="$5"; a2="$6"; cons="$7"; row="$8"; msg="$9"
case "$op" in
  probe) printf '{"result":"ok"}\n' > "$res" ;;
  exit137) exit 137 ;;
  exit134) exit 134 ;;
  5xx) printf '{"http_status":503}\n'; exit 1 ;;
  ratelimit) printf '{"http_status":429}\n'; exit 1 ;;
  fail) printf '{"result":"fail","reason":"%s"}\n' "$a1" > "$res" ;;
  readfix) printf '{"result":"fix","finding":"%s","reason":"%s"}\n' "$a1" "$a1" > "$res" ;;
  readok) printf '{"result":"ok","reason":"read ok"}\n' > "$res" ;;
  dirty) cd "$wt" && printf 'dirty\n' > "$a1" && exit 1 ;;
  commit)
    cd "$wt" && printf '%s\n' "$a2" > "$a1" && printf 'card %s\n\nAuthor: %s\n' "$cons" "$row" > "$msg"
    git add -- "$a1" && git commit -F "$msg" && printf '{"result":"ok"}\n' > "$res" && printf '{"micro_usd":1200,"priced":true}\n' > "$use" ;;
esac`

func (h *ScriptHarness) Command(j Job) ([]string, []string) {
	mode := cmp.Or(j.ScriptMode, strings.TrimSpace(j.Prose), "ok")
	wt := cmp.Or(j.WorktreeDir, filepath.Join(j.JobDir, "repo"))
	op, a1, a2 := parseScriptMode_internal(j, mode)
	argv := []string{"sh", "-c", scriptHarnessProg_internal, "script", op, wt,
		filepath.Join(j.JobDir, "result.json"), filepath.Join(j.JobDir, "usage.json"),
		a1, a2, j.Consumer, j.Row, filepath.Join(j.JobDir, "commit-msg.txt")}
	return argv, SanitizeWallEnv(baseWallEnv_internal(j), j.JobDir)
}
func parseScriptMode_internal(j Job, mode string) (op, a1, a2 string) {
	switch {
	case mode == "probe", mode == "5xx", mode == "ratelimit":
		return mode, "", ""
	case mode == "exit:137", mode == "exit:134", mode == "read:ok":
		return strings.ReplaceAll(mode, ":", ""), "", ""
	case strings.HasPrefix(mode, "fail:"):
		return "fail", strings.TrimPrefix(mode, "fail:"), ""
	case strings.HasPrefix(mode, "read:fix:"):
		return "readfix", strings.TrimPrefix(mode, "read:fix:"), ""
	case strings.HasPrefix(mode, "dirty:"):
		return "dirty", strings.TrimPrefix(mode, "dirty:"), ""
	default:
		if f, c, ok := strings.Cut(strings.TrimPrefix(mode, "commit:"), ":"); ok && f != "" {
			return "commit", f, c
		}
		return "commit", "work.txt", j.Consumer
	}
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
		return Outcome{Result: "fail", Reason: cmp.Or(reason, tail, "harness reported broken")}
	case "fault":
		return Outcome{Result: "fault", Reason: cmp.Or(reason, tail, "harness reported fault")}
	default:
		return Outcome{Result: "fail", Reason: fmt.Sprintf("unknown verdict %q in result.json", res), NoVerdict: true}
	}
}
func readUsageFile_internal(jobDir string) (Cost, bool) {
	data, err := os.ReadFile(filepath.Join(jobDir, "usage.json"))
	var c Cost
	if jobDir == "" || err != nil || json.Unmarshal(data, &c) != nil {
		return Cost{}, false
	}
	return c, c.Priced || c.MicroUSD > 0
}

type WallConfig struct {
	ReserveBin, ReserveDir, SandboxBin string
	ReserveWait                        time.Duration
	ReadPaths, WritePaths              []string
	NetDeny                            bool
}

// wallKeeps are the names nova-sandbox passes into the wall without --env;
// every other name the harness's environment holds is named with --env, or
// the wall drops it (nova-sandbox help).
var wallKeeps_internal = []string{"PATH", "HOME", "LANG", "TERM"}

// BuildWallArgv is nova-reserve around nova-sandbox around the harness: the
// job directory and the adapter's Needs (j.Needs) written, cfg's paths
// read, --unix and the network as the adapter declares (under cfg.NetDeny:
// open, denied with the proxy's port reopened, or denied), and every name
// of env passed with --env.
func BuildWallArgv(cfg WallConfig, j Job, env, harnessArgv []string) []string {
	argv := []string{cmp.Or(cfg.ReserveBin, "nova-reserve")}
	if cfg.ReserveDir != "" {
		argv = append(argv, "--dir", cfg.ReserveDir)
	}
	argv = append(argv, "run")
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
	if j.Needs.Unix {
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
	return []string{
		"PATH=" + cmp.Or(os.Getenv("PATH"), "/usr/local/bin:/usr/bin:/bin"), "LANG=" + cmp.Or(os.Getenv("LANG"), "C.UTF-8"),
		"HOME=" + filepath.Join(j.JobDir, "home"), "TMPDIR=" + filepath.Join(j.JobDir, "tmp"), "XDG_DATA_HOME=" + filepath.Join(j.JobDir, "data"),
		"GIT_TERMINAL_PROMPT=0", "NOVA_WORKER_CONSUMER=" + j.Consumer, "NOVA_WORKER_ROW=" + j.Row,
	}
}
func SanitizeWallEnv(env []string, jobDir string) []string {
	var clean []string
	hasPath := false
	allowed := []string{"LANG", "LC_ALL", "GIT_TERMINAL_PROMPT", "NOVA_WORKER_CONSUMER", "NOVA_WORKER_ROW", "NOVA_WORKER_NONCE", "CLAUDE_CONFIG_DIR", "CODEX_HOME", "OPENAI_BASE_URL"}
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		u := strings.ToUpper(k)
		switch {
		case !ok:
		case u == "PATH":
			hasPath, clean = true, append(clean, kv)
		case u == "OPENAI_API_KEY" && v == ProxyPlaceholderKey:
			clean = append(clean, kv)
		case (u == "HOME" || u == "TMPDIR" || u == "XDG_DATA_HOME") && jobDir == "":
			clean = append(clean, kv)
		case slices.Contains(allowed, u):
			clean = append(clean, kv)
		}
	}
	if !hasPath {
		clean = append(clean, "PATH="+cmp.Or(os.Getenv("PATH"), "/usr/local/bin:/usr/bin:/bin"))
	}
	if jobDir != "" {
		clean = append(clean, "HOME="+filepath.Join(jobDir, "home"), "TMPDIR="+filepath.Join(jobDir, "tmp"), "XDG_DATA_HOME="+filepath.Join(jobDir, "data"))
	}
	return clean
}
