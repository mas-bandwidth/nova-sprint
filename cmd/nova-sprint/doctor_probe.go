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
	"github.com/mas-bandwidth/nova-sprint/pkg/subproc"
)

// The doctor's reaches past the store (doctor.go): nova-config's machine and loop rows, the
// server's start, the seat's own nova-sprint build, and a probe of each member's host. Each is a
// field of outside (machinery.go), so a test gives its own and no test reaches a host; a field
// left nil is "not measured", said as such by its check.

// doctorProbeBudget bounds every probe of one run: the hosts are probed at once, and a host that
// does not answer inside it is down. The whole doctor stays under 15 s.
const doctorProbeBudget = 10 * time.Second

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
	o := a.realOutside()
	pid := o.listenerPID(port)
	if pid == 0 {
		return time.Time{}, "nothing on this host listens on " + addr
	}
	cmd, cancel := subproc.Command(ctx, subproc.Tool, "ps", "-o", "etime=", "-p", strconv.Itoa(pid))
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
	cmd, cancel := subproc.Command(ctx, subproc.Tool, "nova-sprint", "version")
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
	fmt.Fprintf(&b, "L=%s; P=\n", loop)
	b.WriteString(`if command -v systemctl >/dev/null 2>&1; then P=$(systemctl --user show "nova-loop-$L.service" -p Environment --value 2>/dev/null | tr ' ' '\n' | sed -n 's/^PATH=//p' | head -1); fi` + "\n")
	b.WriteString(`if [ -z "$P" ] && [ -f "$HOME/Library/LaunchAgents/com.nova.loop.$L.plist" ]; then P=$(/usr/bin/plutil -extract EnvironmentVariables.PATH raw -o - "$HOME/Library/LaunchAgents/com.nova.loop.$L.plist" 2>/dev/null); fi` + "\n")
	b.WriteString(`F=loop; [ -n "$P" ] || { P=$PATH; F=login; }` + "\n")
	// a card's PATH as native builds it (pkg/swarm BenchPath): ~/sdk/bin, then the Go's real
	// directory (~/sdk/bin/go, ~/go/bin/go, then the loop's PATH, through its links), then the loop's
	b.WriteString(`G=; for d in "$HOME/sdk/bin" "$HOME/go/bin" $(printf '%s' "$P" | tr ':' ' '); do if [ -x "$d/go" ]; then G=$(readlink -f "$d/go" 2>/dev/null || echo "$d/go"); break; fi; done` + "\n")
	b.WriteString(`[ -n "$G" ] && P="$(dirname "$G"):$P"; [ -d "$HOME/sdk/bin" ] && P="$HOME/sdk/bin:$P"` + "\n")
	b.WriteString(`echo "os $(uname -s)"; echo "pathfrom $F"` + "\n")
	fmt.Fprintf(&b, "for t in %s; do w=$(PATH=\"$P\" command -v \"$t\" 2>/dev/null); echo \"tool $t ${w:--}\"; done\n", strings.Join(tools, " "))
	// the gates' toolchain and the pinned harness live beside PATH: found there, they are said where
	b.WriteString(`if ! PATH="$P" command -v go >/dev/null 2>&1; then w=$(ls -d "$HOME"/sdk/go*/bin/go /usr/local/go/bin/go 2>/dev/null | tail -1); [ -n "$w" ] && echo "near go $w"; fi` + "\n")
	b.WriteString(`if ! PATH="$P" command -v opencode >/dev/null 2>&1; then w=$(ls -d "$HOME"/nova-bench/harness-*/opencode 2>/dev/null | tail -1); [ -n "$w" ] && echo "tool opencode $w"; fi` + "\n")
	b.WriteString(`echo "free $(df -Pk "$HOME" 2>/dev/null | awk 'NR==2{print $4}')"` + "\n")
	fmt.Fprintf(&b, "if [ -x \"$HOME/.local/bin/nova-push-credential\" ]; then echo \"push $(\"$HOME/.local/bin/nova-push-credential\" probe %s 2>&1 | tail -1)\"; else echo \"push FAIL no $HOME/.local/bin/nova-push-credential\"; fi\n", doctorPushURL)
	fmt.Fprintf(&b, "S=%s; NS=$(PATH=\"$P\" command -v nova-secrets); SO=$(PATH=\"$P\" command -v sops)\n", bench.Quote(seat))
	b.WriteString(`if [ -n "$NS" ] && [ -n "$SO" ]; then o=$("$NS" check --store "$HOME/nova-bench/secrets" --as "$S" --key "$HOME/.config/nova-secrets/$S.key" --sops "$SO" 2>&1); echo "secrets $? $(printf '%s' "$o" | tail -1)"; else echo "secrets 1 nova-secrets or sops is on no PATH entry of its loop"; fi` + "\n")
	b.WriteString(`echo "version $(PATH="$P" nova-sprint version 2>/dev/null | head -1)"` + "\n")
	return b.String()
}

// parseProbe reads a probe's lines into what it found.
func parseProbe(p *sprint.MemberProbe, out []byte) {
	p.Tools, p.Near, p.FreeKB = map[string]string{}, map[string]string{}, -1
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		key, val, _ := strings.Cut(sc.Text(), " ")
		switch key {
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
			p := sprint.MemberProbe{Member: m.Name, Host: host, FreeKB: -1}
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
