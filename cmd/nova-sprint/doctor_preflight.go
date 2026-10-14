package main

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
)

// cmdDoctorPreflight is `doctor --preflight <candidate binary>`: the release gate's
// frame, run before any server switch or adopt (docs/SPEC-SPRINT.md section 14, "The
// server"; the owner, 2026-10-11: "Also consider sprint doctor in this process"). It
// runs the candidate's `version` and its `tick --shadow --json` against the store
// this doctor reads, read-only as the switch runs it (runShadow, never a copy), and
// prints the frame's rows: the candidate's version, the store's tables against the
// candidate's TABLES.lock, and the gate's later checks each WARN until its card
// lands. The rows are built by sprint.DoctorPreflight over the facts read. Exit 1 on
// any FAIL, as doctor does.
func (a *app) cmdDoctorPreflight(ctx context.Context, c common, binary string, stdout, stderr io.Writer) int {
	ctx, cancel := context.WithTimeout(ctx, doctorRunBudget)
	defer cancel()
	in := sprint.PreflightInput{Binary: binary}
	in.Version, in.VersionErr = candidateVersion(ctx, binary)
	if _, _, err := runShadow(ctx, binary, c.redis, TickDeadline); err != nil {
		in.ShadowCode = 2
		in.ShadowText = err.Error()
	}
	// the members' facts (docs/SPEC-SPRINT.md, doctor: member-env and the preflight): the
	// same probe doctor runs, so a missing tool, a push credential that does not
	// authenticate, a seat that does not open and a flapping link are FAIL rows that
	// refuse the switch, and a member held, draining, away, or short of java/the TLA+
	// tools is a WARN. The sprint's own facts beside each probe (harnesses, provider key
	// names, hold, drain) come from the store; a fleet the preflight cannot reach leaves
	// Probes and Members nil and their checks WARN. A test with no outside reads none.
	o := a.outside
	if o.doctorConfig == nil {
		o = a.realOutside()
	}
	if o.doctorConfig != nil && o.probeMembers != nil {
		if cfg, err := o.doctorConfig(ctx); err == nil && cfg.Machines != nil {
			self := ""
			if o.hostname != nil {
				self = o.hostname()
			}
			var snap *sprint.Snapshot
			if st, err := a.shadowStore(ctx, c); err == nil {
				if read, err := st.DoctorRead(ctx); err == nil {
					snap = read.Snap
				}
			}
			tools := func(member string) []string {
				if snap == nil {
					return slices.Clone(sprint.MemberTools)
				}
				return append(slices.Clone(sprint.MemberTools), sprint.MemberHarnesses(snap, member)...)
			}
			in.Probes = o.probeMembers(ctx, cfg.Machines, tools, self)
			in.Members = sprint.PreflightMembers(snap, in.Probes)
		}
	}
	red := false
	for _, ch := range sprint.DoctorPreflight(in) {
		fmt.Fprintln(stdout, oneline.Escape(ch.Line()))
		if ch.Status == sprint.DoctorFail {
			red = true
		}
	}
	if red {
		return 1
	}
	return 0
}

// candidateVersion runs `<binary> version` and returns its version word, and why it
// did not answer ("" when it did).
func candidateVersion(ctx context.Context, binary string) (string, string) {
	out, err := exec.CommandContext(ctx, binary, "version").Output()
	if err != nil {
		return "", err.Error()
	}
	v := versionWord(string(out))
	if v == "" {
		return "", "printed no version line: " + oneline.Escape(strings.TrimSpace(string(out)))
	}
	return v, ""
}
