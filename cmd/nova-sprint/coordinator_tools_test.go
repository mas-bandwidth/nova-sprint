package main

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// coordinator_tools_test.go holds docs/COORDINATOR-TOOLS.md to the tools it
// names (restored from nova-tools internal/docs/coordinator_tools_test.go and
// coordtools_class_test.go). The page maps each seat-only wrapper, script and
// loop the coordinator runs to the nova verb that does the same, or to a
// proposed card; a stranger setting the sprint up pastes the verb lines, so a
// verb or a flag the tool does not carry is a line that fails the first time it
// is run. The verbs and flags are read from nova-sprint's own verb table
// (cmd/nova-sprint/verbs.go), and the verbs of the other tools the page names
// (nova-swarm, nova-friend, nova-config, nova-bus, nova-secrets) from
// foreignVerbs, read once from the page and the runbook.

// coordinatorToolsPath is the page, relative to this package.
const coordinatorToolsPath = "../../docs/COORDINATOR-TOOLS.md"

// cardCellRe reads a mapping cell that proposes a card: its id, the verb it
// needs (the one span not checked: it does not exist yet), and its PATHS.
var cardCellRe = regexp.MustCompile("^card: `?([a-z0-9][a-z0-9.-]*)`?; needs `([^`]+)`.*?PATHS: (\\S.*?)(?:;|$)")

// coordinatorToolRow is one row of the page's table.
type coordinatorToolRow struct {
	line              int
	tool, what, verbs string
}

// coordinatorToolRows reads the rows of every table whose header is
// `| Tool | What it does | Replaced by |`.
func coordinatorToolRows(text string) []coordinatorToolRow {
	var rows []coordinatorToolRow
	in := false
	for i, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "|") {
			in = false
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		for j := range cells {
			cells[j] = strings.TrimSpace(cells[j])
		}
		if len(cells) == 3 && cells[0] == "Tool" && cells[1] == "What it does" && cells[2] == "Replaced by" {
			in = true
			continue
		}
		if !in || strings.HasPrefix(cells[0], "---") {
			continue
		}
		row := coordinatorToolRow{line: i + 1, tool: cells[0]}
		if len(cells) == 3 {
			row.what, row.verbs = cells[1], cells[2]
		}
		rows = append(rows, row)
	}
	return rows
}

// checkCoordinatorTools is every problem of the page's rows.
func checkCoordinatorTools(verbs map[string]map[string]bool, text string) []string {
	var problems []string
	say := func(r coordinatorToolRow, s string) {
		problems = append(problems, "line "+strconv.Itoa(r.line)+" ("+r.tool+"): "+s)
	}
	rows := coordinatorToolRows(text)
	if len(rows) == 0 {
		return []string{"no row under a | Tool | What it does | Replaced by | header"}
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if !codeSpanRe.MatchString(r.tool) {
			say(r, "the tool cell names no path or unit in a code span")
		}
		if seen[r.tool] {
			say(r, "a second row for the same tool")
		}
		seen[r.tool] = true
		if r.what == "" || r.verbs == "" {
			say(r, "a row wants three cells: the tool, what it does in one line, and the verb or the card")
			continue
		}
		spans := codeSpanRe.FindAllStringSubmatch(r.verbs, -1)
		if strings.HasPrefix(r.verbs, "card:") {
			m := cardCellRe.FindStringSubmatch(r.verbs)
			if m == nil {
				say(r, "a card cell is `card: <id>; needs `<verb line>`; PATHS: <path>,...`")
				continue
			}
			proposed := false
			for _, s := range spans {
				if !proposed && s[1] == m[2] {
					proposed = true
					continue // the proposed verb: the card is what makes it exist
				}
				_, ps := checkCommand(verbs, strings.Fields(s[1]))
				for _, p := range ps {
					say(r, p)
				}
			}
			continue
		}
		checked := false
		for _, s := range spans {
			ok, ps := checkCommand(verbs, strings.Fields(s[1]))
			checked = checked || ok
			for _, p := range ps {
				say(r, p)
			}
		}
		if !checked {
			say(r, "the replacement names no nova verb line and no card")
		}
	}
	return problems
}

// TestEveryCoordinatorToolMapsToARealNovaVerb is the contract: every row of
// docs/COORDINATOR-TOOLS.md maps its tool to verb lines whose verbs and flags
// the tool carries, or to a card with the verb it needs and its PATHS. The
// witnesses run the same check on rows that must be refused, so a check that
// passes everything fails here.
func TestEveryCoordinatorToolMapsToARealNovaVerb(t *testing.T) {
	t.Parallel()
	verbs := sprintVerbs()

	t.Run("the page", func(t *testing.T) {
		raw, err := os.ReadFile(coordinatorToolsPath)
		require.NoError(t, err, "%s: %v", coordinatorToolsPath, err)
		require.GreaterOrEqual(t, len(coordinatorToolRows(string(raw))), 10, "%s maps fewer tools than the coordinator runs", coordinatorToolsPath)
		for _, p := range checkCoordinatorTools(verbs, string(raw)) {
			t.Errorf("%s %s", coordinatorToolsPath, p)
		}
	})

	t.Run("the witnesses", func(t *testing.T) {
		head := "| Tool | What it does | Replaced by |\n|---|---|---|\n"
		good := head + "| `a` | x | `NOVA_SPRINT_SERVER=127.0.0.1:6390 nova-sprint where --watch --every 1s` |\n" +
			"| `b` | x | `nova-sprint friend sync --root <d> --actor <c>` |\n" +
			"| `c` | x | `nova-friend ping --as <c> --to <f>`, `nova-swarm disk-guard --root <d>`, `nova-bus recv --as <me> --forever --exec <cmd>` |\n" +
			"| `d` | x | card: friend-ping-every; needs `nova-friend ping --every <d>`; PATHS: cmd/nova-friend/main.go |\n"
		assert.Empty(t, checkCoordinatorTools(verbs, good), "rows that name real verbs and flags are refused")

		for line, want := range map[string]string{
			"| `a` | x | `nova-sprint fly` |":                                                 `nova-sprint has no verb "fly"`,
			"| `a` | x | `nova-sprint where --fly` |":                                         "nova-sprint where has no flag --fly",
			"| `a` | x | `nova-friend fly` |":                                                 `nova-friend has no verb "fly"`,
			"| `a` | x | `nova-teleport go` |":                                                "nova-teleport is a nova tool this test reads no verb table of",
			"| `a` | x | `ns.sh where` |":                                                     "names no nova verb line and no card",
			"| `a` | x | card: x-1; needs `nova-sprint fly` |":                                "a card cell is",
			"| `a` | x | card: x-1; needs `nova-sprint fly`; PATHS: a.go; today `nova-sprint fly` |": `nova-sprint has no verb "fly"`,
			"| `a` | x |":                     "a row wants three cells",
			"| a | x | `nova-sprint where` |": "names no path or unit",
		} {
			ps := checkCoordinatorTools(verbs, head+line+"\n")
			assert.NotEmpty(t, ps, "the row is not refused: %s", line)
			assert.Contains(t, strings.Join(ps, "\n"), want, "the row is refused for another reason: %s", line)
		}
		assert.Contains(t, strings.Join(checkCoordinatorTools(verbs, head+"| `a` | x | `nova-sprint where` |\n| `a` | x | `nova-sprint where` |\n"), "\n"), "a second row")
		assert.NotEmpty(t, checkCoordinatorTools(verbs, "no table"))
	})
}

// verbOwedNoCard names the rows whose verb is written, not waited for: the
// dashboard server, disk-guard, mirror-refresh, the bash nova-loop wrapper and
// the live table. A card cell on one of them fails.
var verbOwedNoCard = []string{"server.py", "/disk-guard`", "mirror-refresh`", "/nova-loop`", "sprint-table-live"}

// isScript reads a tool cell: a script file, a wrapper in a bin directory, or
// a loop unit.
func isScript(tool string) bool {
	for _, m := range []string{".sh", ".py", "/bin/", "com.nova.loop.", "~/.local/bin/"} {
		if strings.Contains(tool, m) {
			return true
		}
	}
	return false
}

// checkStopgaps is every problem of the rows the class rule judges.
func checkStopgaps(verbs map[string]map[string]bool, text string) []string {
	var problems []string
	say := func(r coordinatorToolRow, s string) {
		problems = append(problems, "line "+strconv.Itoa(r.line)+" ("+r.tool+"): "+s)
	}
	for _, r := range coordinatorToolRows(text) {
		if r.what == "" || r.verbs == "" {
			continue // coordinator_tools_test.go refuses a row of fewer than three cells
		}
		isCard := strings.HasPrefix(r.verbs, "card:")
		retired := strings.HasPrefix(r.verbs, "retired:")
		hasVerb := false
		for _, s := range codeSpanRe.FindAllStringSubmatch(r.verbs, -1) {
			if ok, _ := checkCommand(verbs, strings.Fields(s[1])); ok && !isCard {
				hasVerb = true
			}
		}
		if isCard && !cardCellRe.MatchString(r.verbs) {
			say(r, "a card row names the card, the verb it needs and its PATHS")
		}
		if isScript(r.tool) && !isCard && !hasVerb {
			say(r, "a script with no verb that replaces it and no card")
		}
		if retired && !hasVerb {
			say(r, "a retired row keeps the verb line that replaced the tool")
		}
		for _, m := range verbOwedNoCard {
			if strings.Contains(r.tool, m) && isCard {
				say(r, "this tool has no card to wait for: its verb is written, name the verb line")
			}
		}
	}
	return problems
}

// TestEveryCoordinatorStopgapNamesTheVerbThatReplacesIt holds the register: the
// page's rows pass, and the witnesses run the same check on rows that must be
// refused.
func TestEveryCoordinatorStopgapNamesTheVerbThatReplacesIt(t *testing.T) {
	t.Parallel()
	verbs := sprintVerbs()

	t.Run("the register", func(t *testing.T) {
		raw, err := os.ReadFile(coordinatorToolsPath)
		require.NoError(t, err)
		for _, p := range checkStopgaps(verbs, string(raw)) {
			t.Errorf("%s %s", coordinatorToolsPath, p)
		}
		for _, want := range []string{"nova-swarm mirror", "--stop-floor", "nova-sprint dashboard", "nova-config loop run"} {
			assert.Contains(t, string(raw), want, "the register no longer names %s", want)
		}
	})

	t.Run("the witnesses", func(t *testing.T) {
		head := "| Tool | What it does | Replaced by |\n|---|---|---|\n"
		for line, want := range map[string]string{
			"| `~/bin/thing.sh` | x | see the wiki |":    "a script with no verb",
			"| `<seat-bin>/ns.sh` | x | `ns.sh where` |": "a script with no verb",
			"| `com.nova.loop.a` | x | retired: gone |":  "a script with no verb",
			"| `x` | x | retired: gone |":                "a retired row keeps the verb line",
			"| `~/nova-bench/dashboard/server.py` | x | card: dash; needs `nova-sprint dashboard serve`; PATHS: a.go |": "its verb is written",
			"| `~/.local/bin/nova-loop` | x | card: l; needs `nova-config loop run <name>`; PATHS: a.go |":              "its verb is written",
			"| `a.sh` | x | card: only a name |": "names the card, the verb it needs",
		} {
			ps := checkStopgaps(verbs, head+line+"\n")
			assert.Contains(t, strings.Join(ps, "\n"), want, "the row is not refused for this: %s", line)
		}
		for _, line := range []string{
			"| `a.sh` | x | `nova-sprint where --watch` |",
			"| `~/.local/bin/nova-loop` | x | `nova-config loop run <name> --run-dir <d> -- <cmd>` |",
			"| `a.sh` | x | retired: `nova-sprint where --watch` |",
			"| `a.sh` | x | card: c-1; needs `nova-sprint fly`; PATHS: a.go |",
			"| `x` | x | prose with no script |",
		} {
			assert.Empty(t, checkStopgaps(verbs, head+line+"\n"), "the row is refused: %s", line)
		}
	})
}
