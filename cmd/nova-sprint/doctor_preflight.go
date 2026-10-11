package main

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
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
