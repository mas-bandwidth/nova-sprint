package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/bench"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-sprint/pkg/secrets"
	"github.com/mas-bandwidth/nova-tools/pkg/subproc"
)

// The doctor's reaches past the store (doctor.go): nova-config's machine and loop rows, the
// server's start, the seat's own nova-sprint build, and a probe of each member's host. Each is a
// field of outside (machinery.go), so a test gives its own and no test reaches a host; a field
// left nil is "not measured", said as such by its check.

// doctorProbeBudget bounds every probe of one run: the hosts are probed at once, and a host that
// does not answer inside it is down. The whole doctor stays under 15 s.
const doctorProbeBudget = 10 * time.Second

// doctorRunBudget bounds every reach of the doctor's checks past the store (doctorChecks), and
// doctorToolBudget each local tool it runs (lsof, ps, nova-sprint version): under the run's
// deadline, never the 60 s of a Tool, so the whole doctor stays under 15 s.
const (
	doctorRunBudget  = 12 * time.Second
	doctorToolBudget = 3 * time.Second
)

// probeEnd is the probe's last line: a probe that does not print it was cut by the budget.
const probeEnd = "end"

// doctorPushURL is the repository the push credential probe authenticates against.
const doctorPushURL = "https://github.com/mas-bandwidth/nova-sprint"

// doctorConfig is what the doctor reads of nova-config: its machine rows, and each loop's argv by name.
type doctorConfig struct {
	Machines []sprint.DoctorMachine
	Loops    map[string][]string
}

// readDoctorConfig reads nova-config's machine and loop rows (withConfig: the config tool's own
// address rules, bounded).
func (a *app) readDoctorConfig(ctx context.Context) (doctorConfig, error) {
	var out doctorConfig
	err := a.withConfig(ctx, "", func(ctx context.Context, st config.Store) error {
		rows, err := st.List(ctx, config.KindMachine)
		if err != nil {
			return err
		}
		out.Machines = []sprint.DoctorMachine{}
		for _, r := range rows {
			out.Machines = append(out.Machines, sprint.DoctorMachine{Name: r.Name, User: r.Fields["user"], Seat: r.Fields["seat"], Width: r.Int("width"), Default: r.Fields["width"] == ""})
		}
		loops, err := st.List(ctx, config.KindLoop)
		if err != nil {
			return err
		}
		out.Loops = map[string][]string{}
		for _, r := range loops {
			out.Loops[r.Name] = config.Argv(r.Fields["argv"])
		}
		return nil
	})
	return out, err
}

// serverStartedReal is when the process listening on the server's port started: its listener
// (lsof) and that process's elapsed time (ps). Only a server on this host is measured.
func (a *app) serverStartedReal(ctx context.Context, addr string) (time.Time, string) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return time.Time{}, "the server address " + addr + " has no port"
	}
	lsof, cancelLsof := subproc.CommandFor(ctx, doctorToolBudget, "lsof", "-nP", "-iTCP:"+port, "-sTCP:LISTEN", "-t")
	defer cancelLsof()
	lb, _ := lsof.Output() // ignored: no listener read is pid 0, said below
	pid, _ := strconv.Atoi(strings.TrimSpace(strings.SplitN(string(lb), "\n", 2)[0]))
	if pid == 0 {
		return time.Time{}, "nothing on this host listens on " + addr
	}
	cmd, cancel := subproc.CommandFor(ctx, doctorToolBudget, "ps", "-o", "etime=", "-p", strconv.Itoa(pid))
	defer cancel()
	b, err := cmd.Output()
	if err != nil {
		return time.Time{}, fmt.Sprintf("ps of the server's pid %d: %v", pid, err)
	}
	d, ok := parseEtime(strings.TrimSpace(string(b)))
	if !ok {
		return time.Time{}, "ps gave no elapsed time for pid " + strconv.Itoa(pid)
	}
	return a.now().Add(-d), fmt.Sprintf("pid %d on %s", pid, addr)
}

// parseEtime reads ps's elapsed time, [[dd-]hh:]mm:ss.
func parseEtime(s string) (time.Duration, bool) {
	days := 0
	if d, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.Atoi(d)
		if err != nil {
			return 0, false
		}
		days, s = n, rest
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	total := 0
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0, false
		}
		total = total*60 + n
	}
	return time.Duration(days)*24*time.Hour + time.Duration(total)*time.Second, true
}

// declaredReal is the nova-sprint build installed on the seat's PATH: the build the seat runs
// and every member is declared to run (its version word).
func (a *app) declaredReal(ctx context.Context) string {
	cmd, cancel := subproc.CommandFor(ctx, doctorToolBudget, "nova-sprint", "version")
	defer cancel()
	b, err := cmd.Output()
	if err != nil {
		return ""
	}
	return versionWord(string(b))
}

// versionWord is the version of a `nova-sprint version` line: its second word.
func versionWord(line string) string {
	f := strings.Fields(strings.SplitN(line, "\n", 2)[0])
	if len(f) < 2 {
		return ""
	}
	return f[1]
}

// probeScript is the read-only shell a member's host runs for its probe: its loop's PATH (the
// loop unit's, systemd or launchd, else the login shell's, said) with the bench's toolchain
// ahead of it as native puts it on a card's PATH (pkg/swarm BenchPath), each tool found on it, its
// home's free space, its push credential probe, its secrets seat checked, and its nova-sprint
// version. It prints one "key value" line each and no secret.
func probeScript(member, seat string, tools []string) string {
	loop := "member-" + member
	var b strings.Builder
	fmt.Fprintf(&b, "L=%s; P=\n", bench.Quote(loop))
	b.WriteString(`if command -v systemctl >/dev/null 2>&1; then P=$(systemctl --user show "nova-loop-$L.service" -p Environment --value 2>/dev/null | tr ' ' '\n' | sed -n 's/^PATH=//p' | head -1); fi` + "\n")
	b.WriteString(`if [ -z "$P" ] && [ -f "$HOME/Library/LaunchAgents/com.nova.loop.$L.plist" ]; then P=$(/usr/bin/plutil -extract EnvironmentVariables.PATH raw -o - "$HOME/Library/LaunchAgents/com.nova.loop.$L.plist" 2>/dev/null); fi` + "\n")
	b.WriteString(`F=loop; [ -n "$P" ] || { P=$PATH; F=login; }` + "\n")
	// a card's PATH as native builds it (pkg/swarm BenchPath): ~/sdk/bin, then the Go's real
	// directory (~/sdk/bin/go, ~/go/bin/go, then the loop's PATH, through its links), then the loop's
	b.WriteString(`G=; for d in "$HOME/sdk/bin" "$HOME/go/bin" $(printf '%s' "$P" | tr ':' ' '); do if [ -x "$d/go" ]; then G=$(readlink -f "$d/go" 2>/dev/null || echo "$d/go"); break; fi; done` + "\n")
	b.WriteString(`[ -n "$G" ] && P="$(dirname "$G"):$P"; [ -d "$HOME/sdk/bin" ] && P="$HOME/sdk/bin:$P"` + "\n")
	b.WriteString(`echo "os $(uname -s)"; echo "pathfrom $F"` + "\n")
	fmt.Fprintf(&b, "for t in %s; do w=$(PATH=\"$P\" command -v \"$t\" 2>/dev/null); echo \"tool $t ${w:--}\"; done\n", quotedWords(tools))
	// the gates' toolchain and the pinned harness live beside PATH: found there, they are said where
	b.WriteString(`if ! PATH="$P" command -v go >/dev/null 2>&1; then w=$(ls -d "$HOME"/sdk/go*/bin/go /usr/local/go/bin/go 2>/dev/null | tail -1); [ -n "$w" ] && echo "near go $w"; fi` + "\n")
	b.WriteString(`if ! PATH="$P" command -v opencode >/dev/null 2>&1; then w=$(ls -d "$HOME"/nova-bench/harness-*/opencode 2>/dev/null | tail -1); [ -n "$w" ] && echo "tool opencode $w"; fi` + "\n")
	b.WriteString(`echo "free $(df -Pk "$HOME" 2>/dev/null | awk 'NR==2{print $4}')"` + "\n")
	fmt.Fprintf(&b, "if [ -x \"$HOME/.local/bin/nova-push-credential\" ]; then echo \"push $(\"$HOME/.local/bin/nova-push-credential\" probe %s 2>&1 | tail -1)\"; else echo \"push FAIL no $HOME/.local/bin/nova-push-credential\"; fi\n", doctorPushURL)
	fmt.Fprintf(&b, "S=%s; NS=$(PATH=\"$P\" command -v nova-secrets); SO=$(PATH=\"$P\" command -v sops)\n", bench.Quote(seat))
	b.WriteString(`if [ -n "$NS" ] && [ -n "$SO" ]; then o=$("$NS" check --store "$HOME/nova-bench/secrets" --as "$S" --key "$HOME/.config/nova-secrets/$S.key" --sops "$SO" 2>&1); echo "secrets $? $(printf '%s' "$o" | tail -1)"; else echo "secrets 1 nova-secrets or sops is on no PATH entry of its loop"; fi` + "\n")
	// java on the loop's PATH and the TLA+ tools' jar: only some benches run TLC (the dealer
	// keeps TLA+ cards off the others), so a missing one is a WARN, never a FAIL.
	b.WriteString(`echo "tla $(PATH="$P" command -v java 2>/dev/null || echo -) $( [ -f /opt/tla/tla2tools.jar ] && echo present || echo absent )"` + "\n")
	// the key names the member's secrets seat holds, by name only and never a value
	// (nova-secrets names reads the file without decrypting): its typed lines are passed
	// through with a `name ` prefix and parseProbe reads the NAME rows with pkg/secrets,
	// so neither drifts from the line names prints. The probe asks for the whole seat
	// (--max 0), since names bounds what it prints by default and a route key past the
	// cut would be read as absent; cmd/nova-sprint/backup_out.go reads a seat the same way.
	// "names -" is not measured and marks a call that failed or a nova-secrets without the
	// verb; "names measured" marks a seat that answered, even when it holds no key.
	b.WriteString(`if [ -n "$NS" ]; then o=$("$NS" names --store "$HOME/nova-bench/secrets" --as "$S" --max 0 2>/dev/null); rc=$?; if [ "$rc" -eq 0 ]; then printf '%s\n' "$o" | sed -n 's/^/name /p'; echo "names measured"; else echo "names -"; fi; else echo "names -"; fi` + "\n")
	b.WriteString(`echo "version $(PATH="$P" nova-sprint version 2>/dev/null | head -1)"` + "\n")
	// the kernel's link changes in the last hour (docs/SPEC-SPRINT.md, doctor: member-env; the
	// card doctor-names-a-member-whose-link-flapped): one extended grep over a short journal
	// window, never the whole journal, and nothing from nova-config quoted into it.
	b.WriteString(`O=$(uname -s); if [ "$O" = Darwin ]; then echo "link - not measured on darwin"; elif command -v journalctl >/dev/null 2>&1; then J=$(journalctl -k -q --no-pager --since "-1h" 2>&1); rc=$?; if [ "$rc" -ne 0 ] && [ -z "$J" ]; then echo "link - journalctl exit $rc"; elif printf '%s' "$J" | grep -qiE 'insufficient permissions|not seeing messages'; then echo "link - journalctl cannot read the kernel journal"; else n=$(printf '%s\n' "$J" | grep -icE 'link is up|link is down|link up|link down|carrier lost|carrier acquired'); if [ "$n" -eq 0 ]; then echo "link 0 "; else last=$(printf '%s\n' "$J" | grep -iE 'link is up|link is down|link up|link down|carrier lost|carrier acquired' | tail -1 | cut -c1-160); echo "link $n $last"; fi; fi; else echo "link - journalctl absent"; fi` + "\n")
	b.WriteString("echo " + probeEnd + "\n")
	return b.String()
}

// quotedWords is each word shell-quoted, space separated: a name from nova-config or a route
// never becomes shell on a member's host.
func quotedWords(ws []string) string {
	q := make([]string, len(ws))
	for i, w := range ws {
		q[i] = bench.Quote(w)
	}
	return strings.Join(q, " ")
}

// parseProbe reads a probe's lines into what it found.
func parseProbe(p *sprint.MemberProbe, out []byte) {
	p.Tools, p.Near, p.FreeKB = map[string]string{}, map[string]string{}, -1
	p.LinkChanges = -1
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		key, val, _ := strings.Cut(sc.Text(), " ")
		switch key {
		case probeEnd:
			p.Complete = true
		case "link":
			first, rest, _ := strings.Cut(strings.TrimSpace(val), " ")
			if first == "-" {
				p.LinkChanges, p.LinkWhy = -1, probeClip(rest, 160)
			} else if n, err := strconv.Atoi(first); err == nil {
				p.LinkChanges, p.LinkLast = n, probeClip(rest, 160)
			}
		case "os":
			p.OS = val
		case "pathfrom":
			p.PathFrom = val
		case "tool":
			name, where, _ := strings.Cut(val, " ")
			if where != "-" && where != "" {
				p.Tools[name] = where
			} else if _, seen := p.Tools[name]; !seen {
				p.Tools[name] = ""
			}
		case "near":
			name, where, _ := strings.Cut(val, " ")
			p.Near[name] = where
		case "free":
			if n, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64); err == nil {
				p.FreeKB = n
			}
		case "push":
			p.Push, p.PushOK = probeClip(val, 200), strings.Contains(val, "PUSH-CREDENTIAL OK")
		case "secrets":
			code, line, _ := strings.Cut(val, " ")
			p.Secrets, p.SecretOK = probeClip(line, 200), code == "0"
		case "tla":
			jp, jr, _ := strings.Cut(val, " ")
			if jp == "-" {
				jp = ""
			}
			p.Java, p.TLA = jp, strings.TrimSpace(jr)
		case "name":
			if r, ok := secrets.ParseNameLine(val); ok {
				p.SecretNames = append(p.SecretNames, r.Name)
			}
		case "names":
			if val == "measured" && p.SecretNames == nil {
				p.SecretNames = []string{}
			}
		case "version":
			p.Version = versionWord(val)
		}
	}
}

// probeClip is s cut to n bytes.
func probeClip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// probeMembersReal probes each member's host at once, over ssh as the fleet reaches it (its
// machine row's login), or the local shell on this host, inside doctorProbeBudget.
func (a *app) probeMembersReal(ctx context.Context, machines []sprint.DoctorMachine, tools func(member string) []string, self string) map[string]sprint.MemberProbe {
	ctx, cancel := context.WithTimeout(ctx, doctorProbeBudget)
	defer cancel()
	var mu sync.Mutex
	var wg sync.WaitGroup
	out := map[string]sprint.MemberProbe{}
	for _, m := range machines {
		wg.Add(1)
		go func(m sprint.DoctorMachine) {
			defer wg.Done()
			host := m.Name
			if m.User != "" {
				host = m.User + "@" + m.Name
			}
			p := sprint.MemberProbe{Member: m.Name, Host: host, FreeKB: -1, LinkChanges: -1}
			script := probeScript(m.Name, m.Seat, tools(m.Name))
			var stdout, stderr bytes.Buffer
			var err error
			code := 0
			if m.Name == self {
				p.Host = "local"
				cmd, c := subproc.Command(ctx, subproc.Tool, "sh", "-c", script)
				defer c()
				cmd.Stdout, cmd.Stderr = &stdout, &stderr
				err = cmd.Run()
			} else {
				code, err = bench.Exec{}.Shell(ctx, host, "sh -c "+bench.Quote(script), &stdout, &stderr)
				if err == nil && code == 255 { // ssh's own failure: the host was not reached
					err = fmt.Errorf("ssh exit 255: %s", probeClip(stderr.String(), 160))
				}
			}
			switch {
			case err != nil && stdout.Len() == 0:
				p.Err = probeClip(err.Error()+" "+stderr.String(), 200)
			case ctx.Err() != nil && stdout.Len() == 0:
				p.Err = "no answer within " + doctorProbeBudget.String()
			default:
				p.Reached = true
				parseProbe(&p, stdout.Bytes())
			}
			mu.Lock()
			out[m.Name] = p
			mu.Unlock()
		}(m)
	}
	wg.Wait()
	return out
}
