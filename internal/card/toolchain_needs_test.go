package card

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
)

// The TOOLCHAIN lines of three sprint-21 briefs whose attempts ran on a member that
// lacked the need (the replay: spike-wide-joint-solve #4 dealt to an avx2-only member,
// spike-batch-ops-neon #3..#9 on x86_64, reliable-cs-port-block-2 #1 and #6 where the
// .NET SDK could not start), as each was admitted.
const (
	toolchainWideJointSolve = "TOOLCHAIN: an avx512f Linux bench reached over ssh: hetzner2 (Ryzen 7950X3D) or space (EPYC 9124); b1, b2, b3 and hetzner1 also have avx512f. Never vision (avx2 only), never a Mac. Report the CPU model and `grep -o -w -E \"avx2|avx512f|avx512dq|avx512vl\" /proc/cpuinfo | sort -u`. tools/perf (ab.sh, cpu_info.sh, oracle_check.sh, scalar-oracle.txt) is on BASE now."
	toolchainBatchOpsNeon   = "TOOLCHAIN: a Linux bench with clang and gcc, cmake and make for x86-64 (check avx2 and avx512f in /proc/cpuinfo and report the CPU model), or an Apple silicon bench for arm64; both where the card says so; never the working Mac."
	toolchainReliableCSPort = "TOOLCHAIN: dotnet sdk 8 or newer; a C compiler (cc) and a checkout of mas-bandwidth/reliable (sprint/reliable-audit-fixes, and tag v1.4.5 for the interop pin) for the C reference leg only; git and network access only to fetch that checkout"
)

// toolchainBrief is a card brief carrying extra lines (a TOOLCHAIN line, a GATE-NEEDS
// line, a CARRY note) between its header and its task.
func toolchainBrief(extra ...string) string {
	return strings.Join(append([]string{
		"RESULT: x sha=<sha12> tier: pro",
		"REPO: mas-bandwidth/nova-sprint",
		"PATHS: internal/x/x.go",
		"TEST: internal/x TestX",
	}, append(extra,
		"THE TASK. Make the kernel faster and measure it.",
		"STEP 1. Run the bench and report the numbers.",
	)...), "\n")
}

func toolchainFindings(fs []cardgen.LintFinding) []cardgen.LintFinding {
	var out []cardgen.LintFinding
	for _, f := range fs {
		if f.Check == ToolchainGateCheck {
			out = append(out, f)
		}
	}
	return out
}

// A TOOLCHAIN line (or a THE TASK or STEP sentence saying the work must run on) that
// names an OS, an ISA or a toolchain wants a GATE-NEEDS line carrying the matching fact,
// so the deal matches a member that has it rather than dealing a wrong-host attempt;
// a brief that only mentions such a word in prose about the past passes.
func TestAToolchainNeedIsAGateNeedsLine(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		brief string
		want  []string // the facts the finding names; nil: passes
	}{
		{"wide-joint-solve as admitted", toolchainBrief(toolchainWideJointSolve), []string{"avx512f", "linux"}},
		{"batch-ops-neon as admitted", toolchainBrief(toolchainBatchOpsNeon), []string{"linux", "avx512f", "arm64"}},
		{"reliable-cs-port as admitted", toolchainBrief(toolchainReliableCSPort), []string{"dotnet"}},
		{"wide-joint-solve with GATE-NEEDS", toolchainBrief(toolchainWideJointSolve, "GATE-NEEDS: linux, avx512f"), nil},
		{"batch-ops-neon with GATE-NEEDS", toolchainBrief(toolchainBatchOpsNeon, "GATE-NEEDS: linux, avx512f"), nil},
		{"batch-ops-neon with the arm64 leg", toolchainBrief(toolchainBatchOpsNeon, "GATE-NEEDS: arm64"), nil},
		{"reliable-cs-port with GATE-NEEDS", toolchainBrief(toolchainReliableCSPort, "GATE-NEEDS: dotnet"), nil},
		{"a GATE-NEEDS short of the need", toolchainBrief(toolchainWideJointSolve, "GATE-NEEDS: linux"), []string{"avx512f"}},
		{"only a CARRY note about the past", toolchainBrief("CARRY: attempt 2 ran on a Mac and its numbers are kept"), nil},
		{"a past-tense task sentence", strings.Replace(toolchainBrief(), "THE TASK. Make", "THE TASK. Attempt 3 ran on an arm64 bench and an avx512f Linux one. Make", 1), nil},
		{"a STEP that must run on Linux", strings.Replace(toolchainBrief(), "STEP 1. Run", "STEP 1. The bench must run on an avx512f Linux machine. Run", 1), []string{"avx512f", "linux"}},
		{"a TASK that needs .NET", strings.Replace(toolchainBrief(), "THE TASK. Make", "THE TASK. The port builds and runs on .NET 8. Make", 1), []string{"dotnet"}},
		{"a TOOLCHAIN. section naming a Linux bench", toolchainBrief("TOOLCHAIN. A C++17 compiler, make, Go, git. Run ci.sh on a Linux bench, never on the coordinator's machine."), []string{"linux"}},
		{"no toolchain at all", toolchainBrief("TOOLCHAIN: go 1.25 and git"), nil},
	} {
		got := toolchainFindings(Checks("x", tc.brief, Options{}))
		if tc.want == nil {
			if len(got) != 0 {
				t.Errorf("%s: refused, want passed: %+v", tc.name, got)
			}
			continue
		}
		if len(got) == 0 {
			t.Errorf("%s: passed, want refused naming %v", tc.name, tc.want)
			continue
		}
		for _, w := range tc.want {
			found := false
			for _, f := range got {
				if hasWord(f.Excerpt, w) {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: no finding names %s: %+v", tc.name, w, got)
			}
		}
		for _, f := range got {
			if !strings.Contains(f.Excerpt, GateHostRemedy) {
				t.Errorf("%s: the finding carries no GATE-NEEDS remedy: %q", tc.name, f.Excerpt)
			}
		}
	}
	// The card's own id is no need: spike-batch-ops-neon names neon only as itself.
	own := toolchainBrief("TOOLCHAIN: go and git, as spike-batch-ops-neon's sibling cards had")
	if got := toolchainFindings(Checks("spike-batch-ops-neon", own, Options{})); len(got) != 0 {
		t.Errorf("the card's own id: refused, want passed: %+v", got)
	}
	if got := toolchainFindings(Checks("other-card", own, Options{})); len(got) != 0 {
		t.Errorf("another card's brief naming spike-batch-ops-neon: refused, want passed (a hyphenated name is no need): %+v", got)
	}
	if Remedy(ToolchainGateCheck) != GateHostRemedy {
		t.Errorf("Remedy(%s) = %q, want the gate-host remedy", ToolchainGateCheck, Remedy(ToolchainGateCheck))
	}
}
