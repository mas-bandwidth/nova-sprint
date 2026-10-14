package main

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/bench"
	"github.com/mas-bandwidth/nova-sprint/pkg/subproc"
)

// The member-env beat-form probe (docs/SPEC-SPRINT.md, "The doctor's refused-verb and
// beat-form checks"): the doctor runs each up member's installed `nova-sprint fleet beat <m>
// --load 0 --dry-run` through the same ssh probe the member-env checks use and faults a pair
// whose beat forms disagree. `fleet beat --dry-run` plans the beat and writes nothing: this
// file adds the flag and handles it here, before cmdFleetBeat's own flags, so an installed
// build whose beat form the running server would refuse answers with a non-zero exit and the
// doctor names it. A version pair whose beat forms disagree is a doctor FAIL at adopt time.

// init adds fleet beat --dry-run (stepDryRun, stepdry.go): the installed build accepts the
// probe's word and forwards it to the server, which plans the beat and writes nothing
// (serve.go). An installed build without this flag refuses the word, and the doctor names
// the pair whose beat forms disagree.
func init() {
	stepDryRun["fleet beat"] = true
	verbEffect["fleet beat"] = "store write: records the member's beat in the sprint's store; --dry-run plans the beat and prints what it would write and hands it to the doctor's member-env beat-form probe; --dry-run writes nothing (docs/SPEC-SPRINT.md, \"The doctor's refused-verb and beat-form checks\")"
}

// BeatForm is what the member-env probe read of one member's installed beat form: Known says
// the probe ran the installed `fleet beat --load 0 --dry-run`; OK says it exited 0 (the form
// is one the running server accepts); Form is its own last line, the form that disagreed.
type BeatForm struct {
	Known bool
	OK    bool
	Form  string
}

// beatFormScript is the read-only shell a member's host runs for the beat-form probe: the
// loop's PATH (as probeScript finds it) and then the installed `nova-sprint fleet beat <m>
// --load 0 --dry-run`, printing `beat <exit> <its last line>`. It reuses probeScript so the
// PATH discovery is the member-env probe's own, never a second one.
func beatFormScript(m sprint.DoctorMachine) string {
	s := probeScript(m.Name, m.Seat, nil)
	line := fmt.Sprintf("BO=$(PATH=\"$P\" nova-sprint fleet beat %s --load 0 --dry-run 2>&1); BRC=$?; echo \"beat $BRC $(printf '%%s' \"$BO\" | tail -1)\"\n", bench.Quote(m.Name))
	return strings.Replace(s, "echo "+probeEnd+"\n", line+"echo "+probeEnd+"\n", 1)
}

// parseBeatForm reads the beat-form probe's `beat <exit> <line>` into what it found, Known
// false when the probe printed no such line (it was cut, or the installed build named none).
func parseBeatForm(out []byte) BeatForm {
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		key, val, _ := strings.Cut(sc.Text(), " ")
		if key != "beat" {
			continue
		}
		code, line, _ := strings.Cut(val, " ")
		return BeatForm{Known: true, OK: code == "0", Form: probeClip(line, 200)}
	}
	return BeatForm{}
}

// probeBeatForms probes each member's host at once, over ssh as the fleet reaches it (its
// machine row's login), or the local shell on this host, inside doctorProbeBudget. A host
// that does not answer has Known false: a down member is the member-down stop's, never a
// beat-form FAIL.
func (a *app) probeBeatForms(ctx context.Context, machines []sprint.DoctorMachine, self string) map[string]BeatForm {
	ctx, cancel := context.WithTimeout(ctx, doctorProbeBudget)
	defer cancel()
	var mu sync.Mutex
	var wg sync.WaitGroup
	out := map[string]BeatForm{}
	for _, m := range machines {
		wg.Add(1)
		go func(m sprint.DoctorMachine) {
			defer wg.Done()
			host := m.Name
			if m.User != "" {
				host = m.User + "@" + m.Name
			}
			script := beatFormScript(m)
			var stdout, stderr bytes.Buffer
			var err error
			code := 0
			if m.Name == self {
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
			bf := BeatForm{}
			if err == nil || stdout.Len() > 0 {
				bf = parseBeatForm(stdout.Bytes())
			}
			mu.Lock()
			out[m.Name] = bf
			mu.Unlock()
		}(m)
	}
	wg.Wait()
	return out
}

// beatFormChecks is the member-env beat-form FAIL for each member up whose installed beat
// form the running server refuses: the pair (the installed build's version, the server's
// declared build) and the refusal's own line. A member whose forms agree, or was not probed,
// has no line (docs/SPEC-SPRINT.md, "The doctor's refused-verb and beat-form checks").
func beatFormChecks(machines []sprint.DoctorMachine, probes map[string]sprint.MemberProbe, forms map[string]BeatForm, declared string) []sprint.DoctorCheck {
	var out []sprint.DoctorCheck
	for _, m := range machines {
		p, ok := probes[m.Name]
		if !ok || !p.Reached {
			continue
		}
		bf, ok := forms[m.Name]
		if !ok || !bf.Known || bf.OK {
			continue
		}
		out = append(out, sprint.DoctorCheck{
			Area: sprint.AreaFleet, Check: sprint.CheckMemberEnv, Subject: m.Name, Status: sprint.DoctorFail,
			Evidence: "beat form: the installed nova-sprint " + cmp.Or(p.Version, "?") + " sends a beat the server at " + cmp.Or(declared, "?") + " refuses: " + cmp.Or(bf.Form, "the dry-run beat printed no line"),
			Remedy:   "adopt a nova-sprint whose fleet beat the server accepts (nova-sprint adopt)",
		})
	}
	return out
}
