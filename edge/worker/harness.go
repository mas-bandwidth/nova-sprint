package worker

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

type ClaudeHarness struct{ ConfigDir string }

func (h *ClaudeHarness) Name() string             { return "claude" }
func (h *ClaudeHarness) Probe(j Job) Job          { return probeJob_internal(j, "claude") }
func (h *ClaudeHarness) Usage(j Job) (Cost, bool) { return readUsageFile_internal(j.JobDir) }
func (h *ClaudeHarness) Classify(exit int, tail []byte) Outcome {
	return classifyExitAndTail_internal(exit, tail)
}
func (h *ClaudeHarness) Command(j Job) ([]string, []string) {
	argv := []string{"claude", "--print", "--dangerously-skip-permissions", "--disallowedTools", "Task", "--model", cmp.Or(j.Model, "claude-sonnet-4-6"), "--", buildPrompt_internal(j)}
	env := baseWallEnv_internal(j)
	if h.ConfigDir != "" {
		env = append(env, "CLAUDE_CONFIG_DIR="+h.ConfigDir)
	}
	return argv, SanitizeWallEnv(env, j.JobDir)
}

type CodexHarness struct{ ConfigDir string }

func (h *CodexHarness) Name() string             { return "codex" }
func (h *CodexHarness) Probe(j Job) Job          { return probeJob_internal(j, "codex") }
func (h *CodexHarness) Usage(j Job) (Cost, bool) { return readUsageFile_internal(j.JobDir) }
func (h *CodexHarness) Classify(exit int, tail []byte) Outcome {
	return classifyExitAndTail_internal(exit, tail)
}
func (h *CodexHarness) Command(j Job) ([]string, []string) {
	wt := cmp.Or(j.WorktreeDir, filepath.Join(j.JobDir, "repo"))
	argv := []string{"codex", "exec", "--full-auto", "--model", cmp.Or(j.Model, "gpt-5-codex"), "--cd", wt, buildPrompt_internal(j)}
	env := baseWallEnv_internal(j)
	if h.ConfigDir != "" {
		env = append(env, "CODEX_HOME="+h.ConfigDir)
	}
	return argv, SanitizeWallEnv(env, j.JobDir)
}

type OpenCodeHarness struct{}

func (h *OpenCodeHarness) Name() string             { return "opencode" }
func (h *OpenCodeHarness) Probe(j Job) Job          { return probeJob_internal(j, "opencode") }
func (h *OpenCodeHarness) Usage(j Job) (Cost, bool) { return readUsageFile_internal(j.JobDir) }
func (h *OpenCodeHarness) Classify(exit int, tail []byte) Outcome {
	return classifyExitAndTail_internal(exit, tail)
}
func (h *OpenCodeHarness) Command(j Job) ([]string, []string) {
	argv := []string{"opencode", "run", "--model", cmp.Or(j.Model, "deepseek/deepseek-reasoner"), buildPrompt_internal(j)}
	env := baseWallEnv_internal(j)
	if j.ProxyAddr != "" {
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
  5xx) echo 'provider error: HTTP 503 Service Unavailable' >&2; exit 1 ;;
  ratelimit) echo 'HTTP 429 rate_limit_exceeded' >&2; exit 1 ;;
  fail) printf '{"result":"fail","reason":"%s"}\n' "$a1" > "$res" ;;
  readfix) printf '{"result":"fix","finding":"%s","reason":"%s"}\n' "$a1" "$a1" > "$res" ;;
  readok) printf '{"result":"ok","reason":"read ok"}\n' > "$res" ;;
  dirty) cd "$wt" && printf 'dirty\n' > "$a1" && exit 1 ;;
  commit)
    cd "$wt" && printf '%s\n' "$a2" > "$a1"
    printf 'card %s\n\nAuthor: %s\n' "$cons" "$row" > "$msg"
    git add -- "$a1" && git commit -F "$msg"
    printf '{"result":"ok"}\n' > "$res"
    printf '{"micro_usd":1200,"priced":true}\n' > "$use" ;;
esac`

type ScriptHarness struct{}

func (h *ScriptHarness) Name() string             { return "script" }
func (h *ScriptHarness) Usage(j Job) (Cost, bool) { return readUsageFile_internal(j.JobDir) }
func (h *ScriptHarness) Classify(exit int, tail []byte) Outcome {
	return classifyExitAndTail_internal(exit, tail)
}
func (h *ScriptHarness) Probe(j Job) Job {
	p := probeJob_internal(j, "script")
	p.ScriptMode = "probe"
	return p
}
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
		file, content := "work.txt", j.Consumer
		if f, c, ok := strings.Cut(strings.TrimPrefix(mode, "commit:"), ":"); ok && f != "" {
			file, content = f, c
		}
		return "commit", file, content
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
	if j.Finding != "" {
		fmt.Fprintf(&b, "prior_finding=%s\n", j.Finding)
	}
	if len(j.Paths) > 0 {
		fmt.Fprintf(&b, "paths=%s\n", strings.Join(j.Paths, ","))
	}
	if len(j.DoneWhen) > 0 {
		fmt.Fprintf(&b, "done_when=%s\n", strings.Join(j.DoneWhen, " && "))
	}
	if body := cmp.Or(j.Prose, j.Brief); body != "" {
		fmt.Fprintf(&b, "\n%s\n", body)
	}
	fmt.Fprintf(&b, "\nWrite final verdict JSON to %s/result.json.\n", j.JobDir)
	return b.String()
}
func classifyExitAndTail_internal(exit int, tail []byte) Outcome {
	text := TailBytes(tail, MaxReasonBytes)
	lower := strings.ToLower(text)
	switch {
	case exit == 134 || exit == 137:
		return Outcome{Result: "fault", Reason: fmt.Sprintf("harness killed with exit %d: %s", exit, text)}
	case isRateLimitOrCredit_internal(lower):
		return Outcome{Result: "fault", Reason: "provider rate limit or credit refusal: " + text}
	case isProvider5xx_internal(lower):
		return Outcome{Result: "fault", Reason: "provider 5xx error: " + text, Retryable5xx: true}
	}
	var f Outcome
	if json.Unmarshal(tail, &f) == nil && f.Result != "" {
		return normalizeFileOutcome_internal(f.Result, f.Reason, f.Finding, exit, text)
	}
	if exit != 0 {
		return Outcome{Result: "fail", Reason: fmt.Sprintf("harness exited %d without result.json: %s", exit, text)}
	}
	return Outcome{Result: "fail", Reason: "no result.json written by harness"}
}
func ClassifyJob(h Harness, j Job, exit int, elapsed, launchGrace time.Duration, tail []byte) Outcome {
	out := h.Classify(exit, tail)
	if out.Result == "fault" {
		if out.Retryable5xx && launchGrace > 0 && elapsed > launchGrace {
			out.Retryable5xx = false
		}
		return out
	}
	if j.JobDir != "" {
		if data, err := os.ReadFile(filepath.Join(j.JobDir, "result.json")); err == nil {
			var f Outcome
			if json.Unmarshal(data, &f) == nil && f.Result != "" {
				return normalizeFileOutcome_internal(f.Result, f.Reason, f.Finding, exit, TailBytes(tail, MaxReasonBytes))
			}
		}
	}
	return out
}
func normalizeFileOutcome_internal(res, reason, finding string, exit int, tail string) Outcome {
	r := strings.ToLower(strings.TrimSpace(res))
	switch r {
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
func isRateLimitOrCredit_internal(lower string) bool {
	for _, pat := range []string{"429", "rate_limit", "rate limit", "insufficient_quota", "credit balance is too low", "402 payment required", "quota exceeded"} {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}
func isProvider5xx_internal(lower string) bool {
	for _, pat := range []string{"http 500", "http 502", "http 503", "http 504", "502 bad gateway", "503 service unavailable", "504 gateway timeout", "overloaded_error", "server_error", "bad gateway"} {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}
func readUsageFile_internal(jobDir string) (Cost, bool) {
	if jobDir == "" {
		return Cost{}, false
	}
	data, err := os.ReadFile(filepath.Join(jobDir, "usage.json"))
	if err != nil {
		return Cost{}, false
	}
	var c Cost
	if err := json.Unmarshal(data, &c); err != nil {
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
	wait := cmp.Or(cfg.ReserveWait, 5*time.Second)
	argv = append(argv, "--wait", wait.String(), "--why", cmp.Or(j.Consumer, "probe"), "--", cmp.Or(cfg.SandboxBin, "nova-sandbox"), "--write", j.JobDir)
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
	if cfg.NetDeny {
		argv = append(argv, "--net-deny")
		if j.ProxyAddr != "" {
			argv = append(argv, "--net-allow", j.ProxyAddr)
		}
	}
	return append(append(argv, "--"), harnessArgv...)
}
func baseWallEnv_internal(j Job) []string {
	return []string{
		"PATH=" + cmp.Or(os.Getenv("PATH"), "/usr/local/bin:/usr/bin:/bin"),
		"LANG=" + cmp.Or(os.Getenv("LANG"), "C.UTF-8"),
		"HOME=" + filepath.Join(j.JobDir, "home"),
		"TMPDIR=" + filepath.Join(j.JobDir, "tmp"),
		"XDG_DATA_HOME=" + filepath.Join(j.JobDir, "data"),
		"GIT_TERMINAL_PROMPT=0",
		"NOVA_WORKER_CONSUMER=" + j.Consumer,
		"NOVA_WORKER_ROW=" + j.Row,
	}
}
func SanitizeWallEnv(env []string, jobDir string) []string {
	var clean []string
	hasPath := false
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		ku := strings.ToUpper(k)
		switch ku {
		case "PATH":
			hasPath = true
			clean = append(clean, kv)
		case "OPENAI_API_KEY":
			if v == ProxyPlaceholderKey {
				clean = append(clean, kv)
			}
		case "HOME", "TMPDIR", "XDG_DATA_HOME":
			if jobDir == "" {
				clean = append(clean, kv)
			}
		case "LANG", "LC_ALL", "GIT_TERMINAL_PROMPT", "NOVA_WORKER_CONSUMER", "NOVA_WORKER_ROW", "CLAUDE_CONFIG_DIR", "CODEX_HOME", "OPENAI_BASE_URL":
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
func cmpOr_internal(a, b string) string { return cmp.Or(a, b) }
