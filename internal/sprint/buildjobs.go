package sprint

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
)

// A BRIEF'S BUILD COMMANDS RUN AT -j4 OR LESS.
//
// The benches are shared: benchslot (one -j4 job per four cores) holds a slot for each
// child, so a child that builds at -j16 or -j$(nproc) takes four slots' cores while holding
// one, and every other child on the machine slows. On 2026-10-24 a batch of 36 working
// cards was re-briefed from -j16 to -j4 for exactly this. The card lint refuses a brief
// whose text names a build parallelism above BuildJobsMax, or one computed from the
// machine ($(nproc), `nproc`, getconf, sysctl), so the fix is made before the card is
// dealt and never by an edit in flight. The same reading (BuildJobsRE) is what the brief
// edit treats as operational text (internal/sprint, stripOperationalLines): a -jN change
// alone keeps an in-flight head.

// BuildJobsCheck is the check's token.
const BuildJobsCheck = "build-jobs"

// BuildJobsMax is the most build parallelism a brief may name on a shared bench.
const BuildJobsMax = 4

// BuildJobsRemedy is what the check wants.
const BuildJobsRemedy = "name -j4 or less on every build command (make -j4, cmake --build build -j4, ctest -j4): the benches are shared, one -j4 job per four cores, and a -jN above 4 or one computed from the machine ($(nproc)) takes other children's cores"

// BuildJobsRE is one build parallelism a brief names: -jN, -j N, --jobs N or --jobs=N, N a
// count or a value computed from the machine ($(...) or `...`). Group 1 is what precedes the
// flag, group 2 the value. A flag inside a word (java -jar) is no match: the value must be a
// digit, $( or a backquote.
var BuildJobsRE = regexp.MustCompile("(^|[\\s\"'`(=])(?:-j|--jobs)[ =]?(\\d+|\\$\\([^)]*\\)|`[^`]*`)")

// NormalizeBuildJobs is line with every build parallelism it names read as one: two lines
// that differ only in their -jN values normalize to the same line.
func NormalizeBuildJobs(line string) string {
	return BuildJobsRE.ReplaceAllString(line, "${1}-j<N>")
}

// LintBuildJobs holds a brief to the build-jobs rule (above): one finding per line naming a
// build parallelism above BuildJobsMax or one computed from the machine; none else.
func LintBuildJobs(raw []byte) []swarm.CardHeaderFinding {
	var out []swarm.CardHeaderFinding
	for i, line := range strings.Split(string(raw), "\n") {
		for _, m := range BuildJobsRE.FindAllStringSubmatch(line, -1) {
			v := m[2]
			n, err := strconv.Atoi(v)
			if err == nil && n <= BuildJobsMax {
				continue
			}
			why := fmt.Sprintf("names build parallelism %s, above %d", strings.TrimSpace(strings.TrimPrefix(m[0], m[1])), BuildJobsMax)
			if err != nil {
				why = fmt.Sprintf("names build parallelism %s, computed from the machine", strings.TrimSpace(strings.TrimPrefix(m[0], m[1])))
			}
			out = append(out, swarm.CardHeaderFinding{Check: BuildJobsCheck, Line: i + 1, Excerpt: why + "; the benches are shared, so a build runs at -j4 or less"})
			break
		}
	}
	return out
}

// LintRemedy is what a brief lint check of this package wants (pkg/swarm is a frozen copy of
// nova-tools', so its remedy table cannot name a check made here); "" for any other check.
func LintRemedy(check string) string {
	if check == BuildJobsCheck {
		return BuildJobsRemedy
	}
	return ""
}
