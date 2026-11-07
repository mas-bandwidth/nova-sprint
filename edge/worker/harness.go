package worker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Harness interface {
	Name() string                           // claude, codex, opencode, script
	Command(j Job) (argv, env []string)     // pure, from the job and nova-config
	Probe(j Job) Job                        // a one-turn job on the same path
	Classify(exit int, tail []byte) Outcome // pure classification from exit + tail
	Usage(j Job) (Cost, bool)               // the harness's own accounting
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

type ClaudeHarness struct {
	ConfigDir string
}

func (h *ClaudeHarness) Name() string { return "claude" }
func (h *ClaudeHarness) Command(j Job) ([]string, []string) {
	model := j.Model
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	argv := []string{"claude", "--print", "--dangerously-skip-permissions", "--disallowedTools", "Task", "--model", model, "--", buildPrompt_internal(j)}
	env := baseWallEnv_internal(j)
	if h.ConfigDir != "" {
		env = append(env, "CLAUDE_CONFIG_DIR="+h.ConfigDir)
	}
	return argv, SanitizeWallEnv(env, j.JobDir)
}
func (h *ClaudeHarness) Probe(j Job) Job { return probeJob_internal(j, "claude") }
func (h *ClaudeHarness) Classify(exit int, tail []byte) Outcome {
	return classifyExitAndTail_internal(exit, tail)
}
func (h *ClaudeHarness) Usage(j Job) (Cost, bool) { return readUsageFile_internal(j.JobDir) }

type CodexHarness struct {
	ConfigDir string
}

func (h *CodexHarness) Name() string { return "codex" }
func (h *CodexHarness) Command(j Job) ([]string, []string) {
	model := j.Model
	if model == "" {
		model = "gpt-5-codex"
	}
	wt := j.WorktreeDir
	if wt == "" && j.JobDir != "" {
		wt = filepath.Join(j.JobDir, "repo")
	}
	argv := []string{"codex", "exec", "--full-auto", "--model", model, "--cd", wt, buildPrompt_internal(j)}
	env := baseWallEnv_internal(j)
	if h.ConfigDir != "" {
		env = append(env, "CODEX_HOME="+h.ConfigDir)
	}
	return argv, SanitizeWallEnv(env, j.JobDir)
}
func (h *CodexHarness) Probe(j Job) Job { return probeJob_internal(j, "codex") }
func (h *CodexHarness) Classify(exit int, tail []byte) Outcome {
	return classifyExitAndTail_internal(exit, tail)
}
func (h *CodexHarness) Usage(j Job) (Cost, bool) { return readUsageFile_internal(j.JobDir) }

type OpenCodeHarness struct{}

func (h *OpenCodeHarness) Name() string { return "opencode" }
func (h *OpenCodeHarness) Command(j Job) ([]string, []string) {
	model := j.Model
	if model == "" {
		model = "deepseek/deepseek-reasoner"
	}
	argv := []string{"opencode", "run", "--model", model, buildPrompt_internal(j)}
	env := baseWallEnv_internal(j)
	if j.ProxyAddr != "" {
		baseURL := j.ProxyAddr
		if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
			baseURL = "http://" + baseURL
		}
		env = append(env,
			"OPENAI_BASE_URL="+strings.TrimSuffix(baseURL, "/")+"/v1",
			"OPENAI_API_KEY="+ProxyPlaceholderKey,
		)
	}
	return argv, SanitizeWallEnv(env, j.JobDir)
}
func (h *OpenCodeHarness) Probe(j Job) Job { return probeJob_internal(j, "opencode") }
func (h *OpenCodeHarness) Classify(exit int, tail []byte) Outcome {
	return classifyExitAndTail_internal(exit, tail)
}
func (h *OpenCodeHarness) Usage(j Job) (Cost, bool) { return readUsageFile_internal(j.JobDir) }

type ScriptHarness struct{}

func (h *ScriptHarness) Name() string { return "script" }
func (h *ScriptHarness) Command(j Job) ([]string, []string) {
	mode := j.ScriptMode
	if mode == "" {
		mode = strings.TrimSpace(j.Prose)
	}
	if mode == "" {
		mode = "ok"
	}
	argv := []string{"sh", "-c", buildScriptBody_internal(j, mode)}
	return argv, SanitizeWallEnv(baseWallEnv_internal(j), j.JobDir)
}
func (h *ScriptHarness) Probe(j Job) Job {
	p := probeJob_internal(j, "script")
	p.ScriptMode = "probe"
	return p
}
func (h *ScriptHarness) Classify(exit int, tail []byte) Outcome {
	return classifyExitAndTail_internal(exit, tail)
}
func (h *ScriptHarness) Usage(j Job) (Cost, bool) { return readUsageFile_internal(j.JobDir) }
func buildScriptBody_internal(j Job, mode string) string {
	resPath := filepath.Join(j.JobDir, "result.json")
	usePath := filepath.Join(j.JobDir, "usage.json")
	wt := j.WorktreeDir
	if wt == "" && j.JobDir != "" {
		wt = filepath.Join(j.JobDir, "repo")
	}
	switch {
	case mode == "probe":
		return fmt.Sprintf("printf '{\"result\":\"ok\"}\\n' > %q", resPath)
	case mode == "exit:137":
		return "exit 137"
	case mode == "exit:134":
		return "exit 134"
	case mode == "5xx":
		return "echo 'provider error: HTTP 503 Service Unavailable' >&2; exit 1"
	case mode == "ratelimit":
		return "echo 'HTTP 429 rate_limit_exceeded' >&2; exit 1"
	case strings.HasPrefix(mode, "fail:"):
		why := strings.TrimPrefix(mode, "fail:")
		return fmt.Sprintf("printf '{\"result\":\"fail\",\"reason\":%q}\\n' > %q", why, resPath)
	case strings.HasPrefix(mode, "read:fix:"):
		f := strings.TrimPrefix(mode, "read:fix:")
		return fmt.Sprintf("printf '{\"result\":\"fix\",\"finding\":%q,\"reason\":%q}\\n' > %q", f, f, resPath)
	case mode == "read:ok":
		return fmt.Sprintf("printf '{\"result\":\"ok\",\"reason\":\"read ok\"}\\n' > %q", resPath)
	case strings.HasPrefix(mode, "dirty:"):
		f := strings.TrimPrefix(mode, "dirty:")
		return fmt.Sprintf("cd %q && printf 'dirty\\n' > %q && exit 1", wt, f)
	default:
		file := "work.txt"
		content := j.Consumer
		if strings.HasPrefix(mode, "commit:") {
			parts := strings.SplitN(strings.TrimPrefix(mode, "commit:"), ":", 2)
			if len(parts) == 2 && parts[0] != "" {
				file, content = parts[0], parts[1]
			}
		}
		msgFile := filepath.Join(j.JobDir, "commit-msg.txt")
		return fmt.Sprintf(
			"set -e; cd %q && printf '%%s\\n' %q > %q && printf 'card %s\\n\\nAuthor: %s\\n' > %q && git add %q && git commit -F %q && printf '{\"result\":\"ok\"}\\n' > %q && printf '{\"micro_usd\":1200,\"priced\":true}\\n' > %q",
			wt, content, file, j.Consumer, j.Row, msgFile, file, msgFile, resPath, usePath,
		)
	}
}
func probeJob_internal(j Job, harness string) Job {
	p := j
	p.Consumer = j.Row + ".probe"
	p.Kind = "work"
	p.HarnessName = harness
	p.DoneWhen = nil
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
	if j.Prose != "" {
		fmt.Fprintf(&b, "\n%s\n", j.Prose)
	} else if j.Brief != "" {
		fmt.Fprintf(&b, "\n%s\n", j.Brief)
	}
	fmt.Fprintf(&b, "\nWrite final verdict JSON to %s/result.json.\n", j.JobDir)
	return b.String()
}
func classifyExitAndTail_internal(exit int, tail []byte) Outcome {
	text := TailBytes(tail, MaxReasonBytes)
	lower := strings.ToLower(text)
	if exit == 134 || exit == 137 {
		return Outcome{Result: "fault", Reason: fmt.Sprintf("harness killed with exit %d: %s", exit, text)}
	}
	if isRateLimitOrCredit_internal(lower) {
		return Outcome{Result: "fault", Reason: "provider rate limit or credit refusal: " + text}
	}
	if isProvider5xx_internal(lower) {
		return Outcome{Result: "fault", Reason: "provider 5xx error: " + text, Retryable5xx: true}
	}
	var fileRes struct {
		Result  string `json:"result"`
		Reason  string `json:"reason"`
		Finding string `json:"finding"`
	}
	if err := json.Unmarshal(tail, &fileRes); err == nil && fileRes.Result != "" {
		return normalizeFileOutcome_internal(fileRes.Result, fileRes.Reason, fileRes.Finding, exit, text)
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
			var f struct {
				Result  string `json:"result"`
				Reason  string `json:"reason"`
				Finding string `json:"finding"`
			}
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
		return Outcome{Result: "fix", Reason: cmpOr_internal(reason, finding), Finding: cmpOr_internal(finding, reason)}
	case "broken", "fail":
		return Outcome{Result: "fail", Reason: cmpOr_internal(reason, cmpOr_internal(tail, "harness reported broken"))}
	case "fault":
		return Outcome{Result: "fault", Reason: cmpOr_internal(reason, cmpOr_internal(tail, "harness reported fault"))}
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
	ReserveBin, ReserveDir string
	ReserveWait            time.Duration
	SandboxBin             string
	ReadPaths              []string
	NetDeny                bool
}

func BuildWallArgv(cfg WallConfig, j Job, harnessArgv []string) []string {
	resBin := cmpOr_internal(cfg.ReserveBin, "nova-reserve")
	sbBin := cmpOr_internal(cfg.SandboxBin, "nova-sandbox")
	weight := j.SlotWeight
	if weight <= 0 {
		weight = 1
	}
	wait := cfg.ReserveWait
	if wait <= 0 {
		wait = 5 * time.Second
	}
	argv := []string{resBin}
	if cfg.ReserveDir != "" {
		argv = append(argv, "--dir", cfg.ReserveDir)
	}
	argv = append(argv,
		"run",
		"--slots", strconv.Itoa(weight),
		"--wait", wait.String(),
		"--why", cmpOr_internal(j.Consumer, "probe"),
		"--",
		sbBin,
		"--write", j.JobDir,
	)
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
	argv = append(argv, "--")
	argv = append(argv, harnessArgv...)
	return argv
}
func baseWallEnv_internal(j Job) []string {
	home := filepath.Join(j.JobDir, "home")
	tmp := filepath.Join(j.JobDir, "tmp")
	data := filepath.Join(j.JobDir, "data")
	return []string{"HOME=" + home, "TMPDIR=" + tmp, "XDG_DATA_HOME=" + data, "GIT_TERMINAL_PROMPT=0", "NOVA_WORKER_CONSUMER=" + j.Consumer, "NOVA_WORKER_ROW=" + j.Row}
}
func SanitizeWallEnv(env []string, jobDir string) []string {
	var clean []string
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		ku := strings.ToUpper(k)
		if ku == "OPENAI_API_KEY" && v == ProxyPlaceholderKey {
			clean = append(clean, kv)
			continue
		}
		if isForbiddenEnvKey_internal(ku) {
			continue
		}
		if ku == "HOME" || ku == "TMPDIR" || ku == "XDG_DATA_HOME" {
			continue
		}
		clean = append(clean, kv)
	}
	if jobDir != "" {
		clean = append(clean,
			"HOME="+filepath.Join(jobDir, "home"),
			"TMPDIR="+filepath.Join(jobDir, "tmp"),
			"XDG_DATA_HOME="+filepath.Join(jobDir, "data"),
		)
	}
	return clean
}
func isForbiddenEnvKey_internal(ku string) bool {
	for _, sub := range []string{"KEY", "TOKEN", "SECRET", "PASSWORD", "PASSWD", "DSN", "CREDENTIAL", "NOVA_PG", "NOVA_BUS"} {
		if strings.Contains(ku, sub) {
			return true
		}
	}
	return false
}
func cmpOr_internal(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
