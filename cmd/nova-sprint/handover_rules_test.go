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

// handover_rules_test.go holds docs/SPRINT-COORDINATOR.md section 10, the
// coordinator's numbered rules, to the tools it names (restored from
// nova-tools internal/docs/coordrules_test.go, its TestEveryCoordinatorRuleNamesVerbsThatExist).
// A rule whose verb, flag or setting is not in the tool is a rule that cannot
// be followed, and `nova-sprint handover` prints each rule's number and
// sentence, so the two are the same list.

// coordinatorRunbookPath is the runbook, relative to this package.
const coordinatorRunbookPath = "../../docs/SPRINT-COORDINATOR.md"

// rulesSection is the heading of the runbook's numbered rules.
const rulesSection = "## 10. The rules"

// ruleHeadRe reads a rule's first line: its number and the start of its
// sentence, which runs on over the unindented lines after it to the closing **.
var ruleHeadRe = regexp.MustCompile(`^R([0-9]+)\. \*\*(.+)$`)

// judgmentDoneBy is the Done by of a rule no verb carries out: the seat's own
// call. It is said, never left blank.
const judgmentDoneBy = "judgment"

// ruleLikeRe is a line that starts as a rule does; one ruleHeadRe refuses is
// a rule written in another shape, and so unread.
var ruleLikeRe = regexp.MustCompile(`^R[0-9]`)

// coordRule is one numbered rule of the runbook.
type coordRule struct {
	line, n                 int
	rule, why, doneBy, card string
}

// coordRules reads the rules of the section. A field runs on over the indented
// lines after it until the next field, a blank line or the next rule.
func coordRules(text string) (rules []coordRule, problems []string) {
	in := false
	var cur *coordRule
	var last *string
	open := false // the sentence of cur has not reached its closing **
	for i, line := range strings.Split(text, "\n") {
		at := "line " + strconv.Itoa(i+1) + ": "
		if open && (strings.TrimSpace(line) == "" || strings.HasPrefix(line, " ")) {
			problems = append(problems, at+"R"+strconv.Itoa(cur.n)+"'s sentence is not closed by **")
			open = false
		}
		if strings.HasPrefix(line, "## ") {
			in, cur, last = line == rulesSection, nil, nil
			continue
		}
		if !in {
			continue
		}
		if open {
			cur.rule += " " + strings.TrimSpace(line)
			if strings.HasSuffix(cur.rule, "**") {
				cur.rule, open = strings.TrimSuffix(cur.rule, "**"), false
			}
			continue
		}
		if m := ruleHeadRe.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			rules = append(rules, coordRule{line: i + 1, n: n, rule: m[2]})
			cur, last = &rules[len(rules)-1], nil
			if strings.HasSuffix(cur.rule, "**") {
				cur.rule = strings.TrimSuffix(cur.rule, "**")
			} else {
				open = true
			}
			continue
		}
		body, indented := strings.CutPrefix(line, "    ")
		switch {
		case strings.TrimSpace(line) == "":
			cur, last = nil, nil
		case cur != nil && indented:
			field := false
			for _, f := range []struct {
				key string
				to  *string
			}{{"Why: ", &cur.why}, {"Done by: ", &cur.doneBy}, {"Card: ", &cur.card}} {
				if v, ok := strings.CutPrefix(body, f.key); ok {
					if *f.to != "" {
						problems = append(problems, at+"R"+strconv.Itoa(cur.n)+" has a second "+strings.TrimSpace(f.key))
					}
					*f.to, last, field = strings.TrimSpace(v), f.to, true
					break
				}
			}
			if !field && last != nil {
				*last += " " + strings.TrimSpace(body)
			}
		case ruleLikeRe.MatchString(line):
			problems = append(problems, at+"a rule's first line is R<n>. **<the rule>**")
		}
	}
	if open {
		problems = append(problems, "R"+strconv.Itoa(cur.n)+"'s sentence is not closed by **")
	}
	return rules, problems
}

// codeSpanRe reads the inline code spans of a line.
var codeSpanRe = regexp.MustCompile("`([^`]+)`")

// synopsisFlagRe reads a flag a verb's synopsis names: `--name`, `--name=value`.
var synopsisFlagRe = regexp.MustCompile(`--([a-z][a-z0-9-]*)`)

// commonFlags are the flags common.register and common.registerStore add to
// every verb that opens the store, so a verb line may name them on any verb.
var commonFlags = map[string]bool{"op": true, "max": true, "epoch": true, "redis": true, "actor": true, "json": true}

// sprintVerbs is nova-sprint's verb table read from verbs.go: the verb's name
// (one or two words) to the flags its synopsis names plus the common flags, so
// a line that names a verb or a flag the tool does not carry fails. The table
// is read from the package, never copied here.
func sprintVerbs() map[string]map[string]bool {
	table := map[string]map[string]bool{}
	for _, v := range verbs {
		flags := map[string]bool{}
		for _, m := range synopsisFlagRe.FindAllStringSubmatch(v.syntax, -1) {
			flags[m[1]] = true
		}
		for f := range commonFlags {
			flags[f] = true
		}
		table[v.name] = flags
	}
	return table
}

// foreignVerbs is the verbs of the tools the runbook and docs/COORDINATOR-TOOLS.md
// name that nova-sprint does not build, each read once from those docs. A line
// that names one of these tools with a verb not here is refused; the tools'
// flags are not checked because their FlagSets live in nova-tools.
var foreignVerbs = map[string]map[string]bool{
	"nova-swarm":   {"lint": true, "template": true, "disk-guard": true, "member": true, "mirror": true, "install": true},
	"nova-friend":  {"check": true, "ping": true, "ping-install": true, "wait-pong": true, "status": true, "install": true, "run": true},
	"nova-config":  {"machine": true, "friend": true, "loop": true, "apply": true, "route": true, "migrate": true, "sprint": true, "tier": true},
	"nova-bus":     {"log": true, "peek": true, "send": true, "recv": true},
	"nova-secrets": {"exec": true},
}

// checkCommand checks one command line, the words of a code span: leading
// environment words are skipped, a word that is not a nova tool ends the check
// (a placeholder, a rule name, a program the line runs), and after a bare `--`
// the rest is checked as the command it runs.
func checkCommand(verbs map[string]map[string]bool, words []string) (checked bool, problems []string) {
	for len(words) > 0 && (words[0] == "env" || words[0] == "/usr/bin/env" || (strings.Contains(words[0], "=") && !strings.HasPrefix(words[0], "-"))) {
		words = words[1:]
	}
	if len(words) == 0 {
		return false, nil
	}
	name := words[0]
	if !strings.HasPrefix(name, "nova-") {
		return false, nil
	}
	if foreign, ok := foreignVerbs[name]; ok {
		return checkForeign(foreign, name, words[1:])
	}
	if name != "nova-sprint" {
		return true, []string{name + " is a nova tool this test reads no verb table of"}
	}
	words = words[1:]
	var verbName string
	for n := min(2, len(words)); n >= 1; n-- {
		if _, ok := verbs[strings.Join(words[:n], " ")]; ok {
			verbName = strings.Join(words[:n], " ")
			words = words[n:]
			break
		}
	}
	if verbName == "" {
		first := "(nothing)"
		if len(words) > 0 {
			first = words[0]
		}
		return true, []string{"nova-sprint has no verb " + strconv.Quote(first) + " in its verb table"}
	}
	flags := verbs[verbName]
	for i, w := range words {
		if w == "--" {
			_, sub := checkCommand(verbs, words[i+1:])
			return true, append(problems, sub...)
		}
		flag, ok := strings.CutPrefix(w, "--")
		if !ok {
			continue
		}
		flag, _, _ = strings.Cut(flag, "=")
		if !flags[flag] {
			problems = append(problems, "nova-sprint "+verbName+" has no flag --"+flag+" in its synopsis")
		}
	}
	return true, problems
}

// checkForeign checks a verb line of a tool nova-sprint does not build: the
// verb's name is held to the runbook's own list, its flags not (their FlagSets
// are in nova-tools).
func checkForeign(foreign map[string]bool, tool string, words []string) (bool, []string) {
	if len(words) == 0 {
		return true, []string{tool + " names no verb"}
	}
	if !foreign[words[0]] {
		return true, []string{tool + " has no verb " + strconv.Quote(words[0]) + " in the runbook's list"}
	}
	return true, nil
}

// checkCoordRules is every problem of the runbook's rules.
func checkCoordRules(verbs map[string]map[string]bool, text string) []string {
	rules, problems := coordRules(text)
	if len(rules) == 0 {
		return append(problems, "no rule under "+strconv.Quote(rulesSection))
	}
	say := func(r coordRule, s string) {
		problems = append(problems, "line "+strconv.Itoa(r.line)+" (R"+strconv.Itoa(r.n)+"): "+s)
	}
	seen := map[string]int{}
	for i, r := range rules {
		if r.n != i+1 {
			say(r, "rules are numbered from 1 with no gap: want R"+strconv.Itoa(i+1))
		}
		if prev, ok := seen[r.rule]; ok {
			say(r, "the same rule as R"+strconv.Itoa(prev))
		}
		seen[r.rule] = r.n
		if r.why == "" {
			say(r, "a rule wants a Why: line, the failure it prevents")
		}
		if r.doneBy == "" {
			say(r, "a rule wants a Done by: line, the verb or setting that does it, or "+judgmentDoneBy)
			continue
		}
		if strings.HasPrefix(r.doneBy, judgmentDoneBy) {
			continue
		}
		checked := false
		for _, s := range codeSpanRe.FindAllStringSubmatch(r.doneBy, -1) {
			ok, ps := checkCommand(verbs, strings.Fields(s[1]))
			checked = checked || ok
			for _, p := range ps {
				say(r, p)
			}
		}
		if !checked {
			say(r, "Done by names no nova verb line or setting; a rule no verb carries out says "+judgmentDoneBy)
		}
	}
	return problems
}

// TestEveryCoordinatorRuleNamesVerbsThatExist is the contract: every numbered
// rule of docs/SPRINT-COORDINATOR.md section 10 says why, and names verbs,
// flags and settings the tools carry, or says judgment.
func TestEveryCoordinatorRuleNamesVerbsThatExist(t *testing.T) {
	t.Parallel()
	verbs := sprintVerbs()
	require.Contains(t, verbs, "where", "cmd/nova-sprint: the verb table was not read")

	t.Run("the runbook", func(t *testing.T) {
		raw, err := os.ReadFile(coordinatorRunbookPath)
		require.NoError(t, err, "%s: %v", coordinatorRunbookPath, err)
		rules, _ := coordRules(string(raw))
		require.GreaterOrEqual(t, len(rules), 90, "%s carries fewer rules than the coordinator follows", coordinatorRunbookPath)
		for _, p := range checkCoordRules(verbs, string(raw)) {
			t.Errorf("%s %s", coordinatorRunbookPath, p)
		}
	})

	t.Run("the witnesses", func(t *testing.T) {
		rule := func(n int, done string) string {
			return "R" + strconv.Itoa(n) + ". **Rule " + strconv.Itoa(n) + ".**\n    Why: a failure.\n    Done by: " + done + "\n\n"
		}
		head := "# x\n\n" + rulesSection + "\n\n"
		good := head + rule(1, "`nova-sprint fleet level`, `nova-sprint set --friend-finish 30m`.") +
			rule(2, "`nova-sprint where --watch`; `nova-friend ping --as <c> --to <f>`.") +
			rule(3, "judgment: the seat decides.")
		assert.Empty(t, checkCoordRules(verbs, good), "rules that name real verbs and flags are refused")

		for done, want := range map[string]string{
			"`nova-sprint fly`":                          `nova-sprint has no verb "fly"`,
			"`nova-sprint set --fly 1`":                  "nova-sprint set has no flag --fly",
			"`nova-friend fly`":                          `nova-friend has no verb "fly"`,
			"`nova-teleport go`":                         "nova-teleport is a nova tool this test reads no verb table of",
			"`ns.sh where`":                              "names no nova verb line or setting",
			"":                                           "wants a Done by: line",
		} {
			text := head + "R1. **Rule.**\n    Why: a failure.\n"
			if done != "" {
				text += "    Done by: " + done + "\n"
			}
			ps := checkCoordRules(verbs, text)
			assert.Contains(t, strings.Join(ps, "\n"), want, "the rule is not refused, or refused for another reason: %q", done)
		}
		for text, want := range map[string]string{
			head + "R1. **Rule.**\n    Done by: judgment\n":  "wants a Why: line",
			head + rule(1, "judgment") + rule(3, "judgment"):  "no gap: want R2",
		} {
			ps := checkCoordRules(verbs, text)
			assert.Contains(t, strings.Join(ps, "\n"), want, "the runbook is not refused, or refused for another reason:\n%s", text)
		}
	})
}

// runbookRules reads the numbered rules of docs/SPRINT-COORDINATOR.md section
// 10, each sentence joined from the lines it wraps over.
func runbookRules(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(coordinatorRunbookPath)
	require.NoError(t, err)
	var rules []string
	in, open := false, false
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "## ") {
			in = line == rulesSection
			continue
		}
		switch {
		case in && open:
			rules[len(rules)-1] += " " + strings.TrimSpace(line)
		case in && ruleHeadRe.MatchString(line):
			rules = append(rules, ruleHeadRe.FindStringSubmatch(line)[2])
		default:
			continue
		}
		last := rules[len(rules)-1]
		open = !strings.HasSuffix(last, "**")
		rules[len(rules)-1] = strings.TrimSuffix(last, "**")
	}
	return rules
}

// The rules handover prints are the runbook's, in its order: a rule changed
// in one and not the other fails here.
func TestHandoverRulesAreTheRunbooks(t *testing.T) {
	t.Parallel()
	doc := runbookRules(t)
	require.NotEmpty(t, doc, "%s section 10 holds no rule", coordinatorRunbookPath)
	assert.Equal(t, doc, coordinatorRules, "cmd/nova-sprint/handover_rules.go and %s section 10 disagree", coordinatorRunbookPath)
}

// `nova-sprint handover` prints each rule of section 10, numbered as the
// runbook numbers it, through the real path (seat.go's handover sets its Rules
// from handoverRules).
func TestHandoverPrintsEachRuleByNumber(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	out := ta.ok("handover")
	assert.Contains(t, out, "RULE R1. "+coordinatorRules[0]+"\n")
	assert.Contains(t, out, "RULE R"+strconv.Itoa(len(coordinatorRules))+". "+coordinatorRules[len(coordinatorRules)-1]+"\n")
	assert.Equal(t, len(coordinatorRules), strings.Count(out, "\nRULE R"), out)
}
