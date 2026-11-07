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
)

type Harness interface {
	Name() string
	Command(j Job) (argv, env []string)
	Probe(j Job) Job
	Classify(exit int, tail []byte) Outcome
	Usage(j Job) (Cost, bool)
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

// modelFlag_internal passes the model the take receipt named. The adapters
// hold no model of their own: a job with none (the probe) runs the harness's
// own configured default, since the probe tests the path, not the model.
func modelFlag_internal(j Job) []string {
	if j.Model == "" {
		return nil
	}
	return []string{"--model", j.Model}
}
func (h *ClaudeHarness) Command(j Job) ([]string, []string) {
	argv := append(append([]string{"claude", "--print", "--dangerously-skip-permissions", "--disallowedTools", "Task"}, modelFlag_internal(j)...), "--", buildPrompt_internal(j))
	env := baseWallEnv_internal(j)
	if h.ConfigDir != "" {
		env = append(env, "CLAUDE_CONFIG_DIR="+h.ConfigDir)
	}
	return argv, SanitizeWallEnv(env, j.JobDir)
}
func (h *CodexHarness) Command(j Job) ([]string, []string) {
	wt := cmp.Or(j.WorktreeDir, filepath.Join(j.JobDir, "repo"))
	argv := append(append([]string{"codex", "exec", "--full-auto"}, modelFlag_internal(j)...), "--cd", wt, buildPrompt_internal(j))
	env := baseWallEnv_internal(j)
	if h.ConfigDir != "" {
		env = append(env, "CODEX_HOME="+h.ConfigDir)
	}
	return argv, SanitizeWallEnv(env, j.JobDir)
}
func (h *OpenCodeHarness) Command(j Job) ([]string, []string) {
	if j.JobDir != "" { // LOCAL DOGFOOD STOPGAP: opencode's config lives in the wall home; without it every write outside the worktree asks for permission and the run dies
		dir := filepath.Join(j.JobDir, "home", ".config", "opencode")
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(`{"$schema":"https://opencode.ai/config.json","permission":{"edit":"allow","bash":"allow","webfetch":"allow","external_directory":"allow","doom_loop":"allow"}}`+"\n"), 0o644)
	}
	model := modelFlag_internal(j)
	if len(model) == 0 { // LOCAL DOGFOOD STOPGAP: a probe carries no model
		model = []string{"--model", "deepseek/deepseek-chat"}
	}
	argv := append(append([]string{"opencode", "run"}, model...), buildPrompt_internal(j))
	env := baseWallEnv_internal(j)
	for _, name := range []string{"DEEPSEEK_API_KEY", "INCEPTION_API_KEY"} { // LOCAL DOGFOOD STOPGAP
		if v := os.Getenv(name); v != "" {
			env = append(env, name+"="+v)
		}
	}
	if j.ProxyAddr != "" && os.Getenv("NOVA_WORKER_NET_OPEN") != "1" { // LOCAL DOGFOOD STOPGAP: no proxy env when the provider is reached directly
		baseURL := j.ProxyAddr
		if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
			baseURL = "http://" + baseURL
		}
		env = append(env, "OPENAI_BASE_URL="+strings.TrimSuffix(baseURL, "/")+"/v1", "OPENAI_API_KEY="+ProxyPlaceholderKey)
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
	fmt.Fprintf(&b, "\nWrite final verdict JSON to %s/result.json.\n", j.JobDir)
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
	if exit != 0 {
		return Outcome{Result: "fail", Reason: fmt.Sprintf("harness exited %d without result.json: %s", exit, text)}
	}
	return Outcome{Result: "fail", Reason: "no result.json written by harness"}
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
	if j.JobDir != "" {
		if data, err := os.ReadFile(filepath.Join(j.JobDir, "result.json")); err == nil {
			var f Outcome
			if json.Unmarshal(data, &f) == nil && f.Result != "" {
				return normalizeFileOutcome_internal(f.Result, f.Reason, f.Finding, exit, text)
			}
		}
	}
	if out, ok := classifyHTTPStatus_internal(j.ProxyStatus, elapsed, launchGrace); ok {
		return out
	}
	out := h.Classify(exit, tail)
	if out.Result == "fault" && out.Retryable5xx && launchGrace > 0 && elapsed > launchGrace {
		out.Retryable5xx = false
	}
	return out
}
func normalizeFileOutcome_internal(res, reason, finding string, exit int, tail string) Outcome {
	switch r := strings.ToLower(strings.TrimSpace(res)); r {
	case "ok", "yes":
		if exit != 0 {
			return Outcome{Result: "fail", Reason: fmt.Sprintf("result.json says %s but exit was %d: %s", r, exit, tail)}
		}
		return Outcome{Result: "ok", Reason: reason}
	case "fix":
		return Outcome{Result: "fix", Reason: cmp.Or(reason, finding), Finding: cmp.Or(finding, reason)}
	case "broken", "fail":
		return Outcome{Result: "fail", Reason: cmp.Or(reason, tail, "harness reported broken")}
	case "fault":
		return Outcome{Result: "fault", Reason: cmp.Or(reason, tail, "harness reported fault")}
	default:
		return Outcome{Result: "fail", Reason: fmt.Sprintf("unknown verdict %q in result.json", res)}
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

func BuildWallArgv(cfg WallConfig, j Job, harnessArgv []string) []string {
	argv := []string{cmp.Or(cfg.ReserveBin, "nova-reserve")}
	if cfg.ReserveDir != "" {
		argv = append(argv, "--dir", cfg.ReserveDir)
	}
	argv = append(argv, "run")
	if j.SlotWeight > 0 {
		argv = append(argv, "--slots", strconv.Itoa(j.SlotWeight))
	}
	argv = append(argv, "--wait", cmp.Or(cfg.ReserveWait, 5*time.Second).String(), "--why", cmp.Or(j.Consumer, "probe"), "--", cmp.Or(cfg.SandboxBin, "nova-sandbox"), "--write", j.JobDir)
	argv = append(argv, "--unix") // LOCAL DOGFOOD STOPGAP: opencode's own server needs AF_UNIX; without it the wall kills it at startup (defect 32)
	for _, name := range []string{"DEEPSEEK_API_KEY", "INCEPTION_API_KEY"} { // LOCAL DOGFOOD STOPGAP (Glenn 2026-11-07: move on): the key rides into the wall
		if os.Getenv(name) != "" {
			argv = append(argv, "--env", name)
		}
	}
	for _, wp := range cfg.WritePaths {
		if wp != "" {
			argv = append(argv, "--write", wp)
		}
	}
	for _, rp := range cfg.ReadPaths {
		if rp != "" {
			argv = append(argv, "--read", rp)
		}
	}
	if cfg.NetDeny && os.Getenv("NOVA_WORKER_NET_OPEN") != "1" { // LOCAL DOGFOOD STOPGAP: NOVA_WORKER_NET_OPEN=1 lets the harness reach its provider directly (no proxy route tonight)
		argv = append(argv, "--net-deny")
		if j.ProxyAddr != "" {
			argv = append(argv, "--net-allow", j.ProxyAddr)
		}
	}
	return append(append(argv, "--"), harnessArgv...)
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
	allowed := []string{"LANG", "LC_ALL", "GIT_TERMINAL_PROMPT", "NOVA_WORKER_CONSUMER", "NOVA_WORKER_ROW", "NOVA_WORKER_NONCE", "CLAUDE_CONFIG_DIR", "CODEX_HOME", "OPENAI_BASE_URL", "DEEPSEEK_API_KEY", "INCEPTION_API_KEY"} // LOCAL DOGFOOD STOPGAP: the last two
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
